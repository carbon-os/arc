package runtime

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
)

// Config holds the parameters used when starting a renderer process.
type Config struct {
	Title        string
	Width        int
	Height       int
	Debug        bool
	Logging      bool
	RendererPath string
	Prebuilt     bool
	OnReady      func()
	OnClose      func() bool

	// ChannelID, when non-empty, disables renderer spawning. Arc will connect
	// to an already-running renderer on this channel instead of starting one.
	// Populated automatically from --channel when main_process.mm is the parent.
	ChannelID string
}

// MessageHandler is the internal callback type used by the ipc package.
type MessageHandler func(text string, data []byte, binary bool)

// BillingProductsHandler is called when the renderer returns live product metadata.
type BillingProductsHandler func([]BillingProduct)

// BillingPurchaseHandler is called on every purchase lifecycle transition.
type BillingPurchaseHandler func(BillingPurchaseEvent)

// Runtime manages a single renderer subprocess and its IPC connection.
// Each BrowserWindow owns exactly one Runtime.
type Runtime struct {
	cfg       Config
	log       *log.Logger
	transport *Transport
	conn      net.Conn
	cmd       *exec.Cmd

	writeMu sync.Mutex

	handlersMu sync.RWMutex
	handlers   map[string]MessageHandler

	billingMu              sync.RWMutex
	billingProductsHandler BillingProductsHandler
	billingPurchaseHandler BillingPurchaseHandler

	quit chan struct{}
	once sync.Once
}

// New creates a Runtime for the given config.
func New(cfg Config) (*Runtime, error) {
	if cfg.Width == 0 {
		cfg.Width = 1280
	}
	if cfg.Height == 0 {
		cfg.Height = 800
	}
	if cfg.Title == "" {
		cfg.Title = "Arc"
	}
	return &Runtime{
		cfg:      cfg,
		log:      NewLogger(cfg.Logging),
		handlers: make(map[string]MessageHandler),
		quit:     make(chan struct{}),
	}, nil
}

// Run connects to or spawns the renderer process, then blocks on the event
// loop until the window is closed or Quit is called.
//
// When cfg.ChannelID is set (external renderer mode), Run connects to an
// already-listening socket opened by main_process.mm — no subprocess is
// started. When cfg.ChannelID is empty (self-spawn mode), Run resolves the
// renderer binary, listens on a new socket, and spawns the renderer itself.
func (rt *Runtime) Run() error {
	wv2Path, err := EnsureWebView2()
	if err != nil {
		return fmt.Errorf("arc: webview2: %w", err)
	}

	var transport *Transport

	if rt.cfg.ChannelID != "" {
		rt.log.Printf("[go] Run: external renderer mode, listening on channel %s", rt.cfg.ChannelID)

		t, err := ListenTransport(rt.cfg.ChannelID)
		if err != nil {
			return fmt.Errorf("arc: listen for renderer: %w", err)
		}
		transport = t

		rt.log.Println("[go] Run: waiting for renderer to connect...")
		if err := transport.Accept(); err != nil {
			return fmt.Errorf("arc: renderer did not connect: %w", err)
		}

	} else {
		// ── Self-spawn mode (default) ─────────────────────────────────────────
		// Go is the parent. Resolve the binary, open the socket, spawn renderer.
		rendererPath, err := EnsureRenderer(rt.cfg.RendererPath, rt.cfg.Prebuilt)
		if err != nil {
			return err
		}

		id := uniqueID()
		t, err := ListenTransport(id)
		if err != nil {
			return err
		}
		transport = t

		args := []string{"--channel", transport.ID}
		if rt.cfg.Logging {
			args = append(args, "--logging")
		}

		rt.cmd = exec.Command(rendererPath, args...)
		rt.cmd.Stderr = prefixWriter("renderer: ")

		if wv2Path != "" {
			rt.cmd.Env = append(os.Environ(),
				"WEBVIEW2_BROWSER_EXECUTABLE_FOLDER="+wv2Path,
			)
		}

		rt.log.Printf("[go] Run: spawning renderer %s %v", rendererPath, args)
		if err := rt.cmd.Start(); err != nil {
			return fmt.Errorf("arc: start renderer: %w", err)
		}

		rt.log.Println("[go] Run: waiting for renderer to connect...")
		if err := transport.Accept(); err != nil {
			return fmt.Errorf("arc: renderer did not connect: %w", err)
		}
	}

	rt.transport = transport
	defer rt.transport.Close()
	rt.conn = transport.Conn()

	rt.log.Println("[go] Run: sending WindowCreate")
	if err := rt.sendWindowCreate(); err != nil {
		return fmt.Errorf("arc: WindowCreate: %w", err)
	}

	rt.log.Println("[go] Run: entering event loop")
	return rt.loop()
}

func (rt *Runtime) loop() error {
	rt.log.Println("[go] loop: starting")
	r := bufio.NewReader(rt.conn)
	for {
		evt, err := ReadEvent(r)
		if err != nil {
			select {
			case <-rt.quit:
				rt.log.Println("[go] loop: quit signalled, exiting cleanly")
				return nil
			default:
				if err == io.EOF {
					rt.log.Println("[go] loop: EOF — renderer closed connection")
					return nil
				}
				rt.log.Printf("[go] loop: read error: %v", err)
				return fmt.Errorf("arc: read event: %w", err)
			}
		}

		rt.log.Printf("[go] loop: received event type=0x%02X", evt.Type)

		switch evt.Type {
		case evtReady:
			rt.log.Println("[go] loop: evtReady — firing OnReady callback")
			if rt.cfg.OnReady != nil {
				go rt.cfg.OnReady()
			}

		case evtClosed:
			rt.log.Println("[go] loop: evtClosed — calling OnClose")
			allow := true
			if rt.cfg.OnClose != nil {
				allow = rt.cfg.OnClose()
			}
			rt.log.Printf("[go] loop: evtClosed allow=%v", allow)
			if allow {
				return nil
			}

		case evtIpcText:
			rt.log.Printf("[go] loop: evtIpcText channel=%q text=%q", evt.Channel, evt.Text)
			go rt.dispatch(evt.Channel, evt.Text, nil, false)

		case evtIpcBinary:
			rt.log.Printf("[go] loop: evtIpcBinary channel=%q bytes=%d", evt.Channel, len(evt.Data))
			go rt.dispatch(evt.Channel, "", evt.Data, true)

		case evtBillingProducts:
			rt.log.Printf("[go] loop: evtBillingProducts count=%d", len(evt.BillingProducts))
			rt.billingMu.RLock()
			h := rt.billingProductsHandler
			rt.billingMu.RUnlock()
			if h != nil {
				go h(evt.BillingProducts)
			}

		case evtBillingPurchase:
			rt.log.Printf("[go] loop: evtBillingPurchase product=%q status=%d",
				evt.BillingPurchase.ProductID, evt.BillingPurchase.Status)
			rt.billingMu.RLock()
			h := rt.billingPurchaseHandler
			rt.billingMu.RUnlock()
			if h != nil {
				go h(evt.BillingPurchase)
			}

		default:
			rt.log.Printf("[go] loop: unknown event type=0x%02X — ignoring", evt.Type)
		}
	}
}

func (rt *Runtime) dispatch(channel, text string, data []byte, binary bool) {
	rt.handlersMu.RLock()
	h, ok := rt.handlers[channel]
	rt.handlersMu.RUnlock()
	rt.log.Printf("[go] dispatch: channel=%q handlerFound=%v", channel, ok)
	if ok {
		h(text, data, binary)
	}
}

// Send writes a framed command to the renderer. Safe to call from any goroutine.
func (rt *Runtime) Send(typ CmdByte, payload []byte) {
	rt.writeMu.Lock()
	defer rt.writeMu.Unlock()
	if rt.conn == nil {
		rt.log.Printf("[go] Send: conn is nil, dropping cmd=0x%02X", typ)
		return
	}
	rt.log.Printf("[go] Send: cmd=0x%02X payload=%d bytes", typ, len(payload))
	if err := WriteFrame(rt.conn, typ, payload); err != nil {
		rt.log.Printf("[go] Send: WriteFrame error: %v", err)
	}
}

// Quit sends CmdQuit to the renderer and unblocks the event loop.
// Safe to call from any goroutine; idempotent.
func (rt *Runtime) Quit() {
	rt.once.Do(func() {
		rt.log.Println("[go] Quit: signalling quit and sending CmdQuit")
		close(rt.quit)
		rt.Send(CmdQuit, nil)
	})
}

// OnMessage registers an inbound IPC message handler for the named channel.
func (rt *Runtime) OnMessage(channel string, h MessageHandler) {
	rt.handlersMu.Lock()
	rt.handlers[channel] = h
	rt.handlersMu.Unlock()
	rt.log.Printf("[go] OnMessage: registered channel=%q", channel)
}

// OffMessage removes the handler for the named channel.
func (rt *Runtime) OffMessage(channel string) {
	rt.handlersMu.Lock()
	delete(rt.handlers, channel)
	rt.handlersMu.Unlock()
	rt.log.Printf("[go] OffMessage: removed channel=%q", channel)
}

// OnBillingProducts registers the handler for evtBillingProducts.
func (rt *Runtime) OnBillingProducts(h BillingProductsHandler) {
	rt.billingMu.Lock()
	rt.billingProductsHandler = h
	rt.billingMu.Unlock()
	rt.log.Println("[go] OnBillingProducts: registered")
}

// OnBillingPurchase registers the handler for evtBillingPurchase.
func (rt *Runtime) OnBillingPurchase(h BillingPurchaseHandler) {
	rt.billingMu.Lock()
	rt.billingPurchaseHandler = h
	rt.billingMu.Unlock()
	rt.log.Println("[go] OnBillingPurchase: registered")
}

func (rt *Runtime) sendWindowCreate() error {
	payload := EncodeWindowCreate(
		rt.cfg.Width, rt.cfg.Height,
		rt.cfg.Debug, rt.cfg.Title)
	rt.writeMu.Lock()
	defer rt.writeMu.Unlock()
	rt.log.Printf("[go] sendWindowCreate: %dx%d debug=%v title=%q",
		rt.cfg.Width, rt.cfg.Height, rt.cfg.Debug, rt.cfg.Title)
	return WriteFrame(rt.conn, CmdWindowCreate, payload)
}