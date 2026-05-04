package arc

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	arcIpc "github.com/carbon-os/arc/ipc"
	wcfg "github.com/carbon-os/arc/window"
)

// ─── Configuration ────────────────────────────────────────────────────────────

type RendererConfig struct {
	Path string
}

type AppConfig struct {
	Title    string
	Logging  bool
	Renderer RendererConfig
	Ipc      arcIpc.Config
}

// ─── App ──────────────────────────────────────────────────────────────────────

type App struct {
	cfg AppConfig

	conn   net.Conn
	sendMu sync.Mutex

	onReady func()
	onClose func() bool

	mu         sync.RWMutex
	windows    map[string]*Window
	webviewOwn map[string]*Window
	overlayOwn map[string]*WebView

	// prefix-based event routing for integration packages (e.g. billing).
	// Keyed by the prefix string, e.g. "apple.store." or "microsoft.store."
	prefixMu       sync.RWMutex
	prefixHandlers map[string]func(eventType string, payload []byte)

	hostCmd   *exec.Cmd
	channelID string
	done      chan struct{}
	idSeq     uint64
}

func NewApp(cfg AppConfig) *App {
	return &App{
		cfg:            cfg,
		windows:        make(map[string]*Window),
		webviewOwn:     make(map[string]*Window),
		overlayOwn:     make(map[string]*WebView),
		prefixHandlers: make(map[string]func(string, []byte)),
		done:           make(chan struct{}),
	}
}

func (a *App) OnReady(fn func()) { a.onReady = fn }
func (a *App) OnClose(fn func() bool) { a.onClose = fn }

// Send is the public wrapper around sendJSON.
// Integration packages (e.g. billing) use this to issue commands to arc-host
// without importing arc directly, via the App interface in their package.
func (a *App) Send(v any) { a.sendJSON(v) }

// OnEventPrefix registers fn to receive every inbound event whose type field
// starts with prefix. A second call with the same prefix replaces the first.
// Used by integration packages to intercept their own event namespaces.
func (a *App) OnEventPrefix(prefix string, fn func(eventType string, payload []byte)) {
	a.prefixMu.Lock()
	a.prefixHandlers[prefix] = fn
	a.prefixMu.Unlock()
}

func (a *App) Shutdown() {
	a.sendJSON(map[string]any{"type": "host.shutdown"})
}

func (a *App) Ping() {
	a.sendJSON(map[string]any{"type": "host.ping"})
}

func (a *App) Run() error {
	a.channelID = a.cfg.Ipc.Channel
	if a.channelID == "" {
		a.channelID = fmt.Sprintf("arc-%d-%d", os.Getpid(), time.Now().UnixNano())
	}

	if a.cfg.Renderer.Path != "" {
		cmd := exec.Command(a.cfg.Renderer.Path, "--ipc-channel", a.channelID)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("arc: start renderer: %w", err)
		}
		a.hostCmd = cmd
		if a.cfg.Logging {
			log.Printf("[arc] spawned arc-host pid=%d channel=%s", cmd.Process.Pid, a.channelID)
		}
	}

	var conn net.Conn
	var lastErr error
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, lastErr = dialHost(a.channelID)
		if lastErr == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("arc: connect to host (channel %q): %w", a.channelID, lastErr)
	}
	a.conn = conn
	if a.cfg.Logging {
		log.Printf("[arc] connected  channel=%s", a.channelID)
	}

	go a.readLoop()
	<-a.done

	if a.hostCmd != nil {
		_ = a.hostCmd.Wait()
	}
	return nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (a *App) genID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, atomic.AddUint64(&a.idSeq, 1))
}

func (a *App) sendJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("[arc] marshal: %v", err)
		return
	}
	if a.cfg.Logging {
		log.Printf("[arc] → %s", clip(string(data), 200))
	}
	if err := writeFrame(a.conn, &a.sendMu, msgTypeJSON, data); err != nil && a.cfg.Logging {
		log.Printf("[arc] send: %v", err)
	}
}

// ─── Read / event loop ────────────────────────────────────────────────────────

func (a *App) readLoop() {
	defer func() {
		_ = a.conn.Close()
		if a.onClose != nil {
			a.onClose()
		}
		close(a.done)
	}()

	for {
		f, err := readFrame(a.conn)
		if err != nil {
			if a.cfg.Logging {
				log.Printf("[arc] disconnected: %v", err)
			}
			return
		}
		a.handleFrame(f)
	}
}

func (a *App) handleFrame(f frame) {
	switch f.msgType {
	case msgTypeJSON:
		a.handleJSON(f.payload)
	case msgTypeBinary:
	}
}

type envelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Channel string          `json:"channel"`
	Body    json.RawMessage `json:"body"`
	Width   int             `json:"width"`
	Height  int             `json:"height"`
	X       int             `json:"x"`
	Y       int             `json:"y"`
	State   string          `json:"state"`
	URL     string          `json:"url"`
	Title   string          `json:"title"`
	Level   string          `json:"level"`
	Text    string          `json:"text"`
	Error   string          `json:"error"`
}

func (a *App) handleJSON(payload []byte) {
	var ev envelope
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	if a.cfg.Logging {
		log.Printf("[arc] ← %-30s id=%q", ev.Type, ev.ID)
	}

	switch ev.Type {

	case "host.ready":
		a.sendJSON(map[string]any{
			"type":     "host.configure",
			"app_name": a.cfg.Title,
		})

	case "host.configured":
		if a.onReady != nil {
			a.onReady()
		}

	case "host.pong":

	case "window.resized":
		if w := a.win(ev.ID); w != nil {
			w.mu.RLock()
			fn := w.onResize
			w.mu.RUnlock()
			if fn != nil {
				fn(ev.Width, ev.Height)
			}
		}

	case "window.moved":
		if w := a.win(ev.ID); w != nil {
			w.mu.RLock()
			fn := w.onMove
			w.mu.RUnlock()
			if fn != nil {
				fn(ev.X, ev.Y)
			}
		}

	case "window.focused":
		if w := a.win(ev.ID); w != nil {
			w.mu.RLock()
			fn := w.onFocus
			w.mu.RUnlock()
			if fn != nil {
				fn()
			}
		}

	case "window.blurred":
		if w := a.win(ev.ID); w != nil {
			w.mu.RLock()
			fn := w.onBlur
			w.mu.RUnlock()
			if fn != nil {
				fn()
			}
		}

	case "window.closed":
		if w := a.win(ev.ID); w != nil {
			w.mu.RLock()
			fn := w.onClose
			w.mu.RUnlock()
			if fn != nil {
				fn()
			}
		}

	case "window.state_changed":
		if w := a.win(ev.ID); w != nil {
			w.mu.RLock()
			fn := w.onStateChange
			w.mu.RUnlock()
			if fn != nil {
				fn(ev.State)
			}
		}

	case "webview.ready":
		if w := a.wvWin(ev.ID); w != nil {
			w.fireReady()
		} else if ov := a.ovl(ev.ID); ov != nil {
			ov.fireReady()
		}

	case "webview.ipc":
		msg := arcIpc.NewMessage(ev.Body)
		if w := a.wvWin(ev.ID); w != nil {
			w.ipcBridge.dispatch(ev.Channel, msg)
		} else if ov := a.ovl(ev.ID); ov != nil {
			ov.ipcBridge.dispatch(ev.Channel, msg)
		}

	case "webview.navigate":
		if w := a.wvWin(ev.ID); w != nil {
			w.mu.RLock()
			fn := w.onNavigate
			w.mu.RUnlock()
			if fn != nil {
				fn(ev.URL)
			}
		} else if ov := a.ovl(ev.ID); ov != nil {
			ov.mu.RLock()
			fn := ov.onNavigate
			ov.mu.RUnlock()
			if fn != nil {
				fn(ev.URL)
			}
		}

	case "webview.title":
		if w := a.wvWin(ev.ID); w != nil {
			w.mu.RLock()
			fn := w.onTitleChange
			w.mu.RUnlock()
			if fn != nil {
				fn(ev.Title)
			}
		} else if ov := a.ovl(ev.ID); ov != nil {
			ov.mu.RLock()
			fn := ov.onTitleChange
			ov.mu.RUnlock()
			if fn != nil {
				fn(ev.Title)
			}
		}

	case "webview.load_start":
		if w := a.wvWin(ev.ID); w != nil {
			w.mu.RLock()
			fn := w.onLoadStart
			w.mu.RUnlock()
			if fn != nil {
				fn(ev.URL)
			}
		} else if ov := a.ovl(ev.ID); ov != nil {
			ov.mu.RLock()
			fn := ov.onLoadStart
			ov.mu.RUnlock()
			if fn != nil {
				fn(ev.URL)
			}
		}

	case "webview.load_finish":
		if w := a.wvWin(ev.ID); w != nil {
			w.mu.RLock()
			fn := w.onLoadFinish
			w.mu.RUnlock()
			if fn != nil {
				fn(ev.URL)
			}
		} else if ov := a.ovl(ev.ID); ov != nil {
			ov.mu.RLock()
			fn := ov.onLoadFinish
			ov.mu.RUnlock()
			if fn != nil {
				fn(ev.URL)
			}
		}

	case "webview.load_failed":
		if w := a.wvWin(ev.ID); w != nil {
			w.mu.RLock()
			fn := w.onLoadFailed
			w.mu.RUnlock()
			if fn != nil {
				fn(ev.URL, ev.Error)
			}
		} else if ov := a.ovl(ev.ID); ov != nil {
			ov.mu.RLock()
			fn := ov.onLoadFailed
			ov.mu.RUnlock()
			if fn != nil {
				fn(ev.URL, ev.Error)
			}
		}

	case "webview.console":
		if a.cfg.Logging {
			log.Printf("[arc:console wv=%s] [%s] %s", ev.ID, ev.Level, ev.Text)
		}

	default:
		// Route to any integration package that registered a matching prefix.
		a.prefixMu.RLock()
		for prefix, fn := range a.prefixHandlers {
			if strings.HasPrefix(ev.Type, prefix) {
				fn(ev.Type, payload)
				break
			}
		}
		a.prefixMu.RUnlock()
	}
}

// ── Entity lookups ────────────────────────────────────────────────────────────

func (a *App) win(id string) *Window {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.windows[id]
}

func (a *App) wvWin(webviewID string) *Window {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.webviewOwn[webviewID]
}

func (a *App) ovl(id string) *WebView {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.overlayOwn[id]
}

// ─── Window factory ───────────────────────────────────────────────────────────

func (a *App) NewWindow(cfg wcfg.Config) *Window      { return a.newWindow(cfg) }
func (a *App) NewBrowserWindow(cfg wcfg.Config) *Window { return a.newWindow(cfg) }

func (a *App) newWindow(cfg wcfg.Config) *Window {
	winID := a.genID("win")
	wvID := a.genID("wv")

	w, h := cfg.Width, cfg.Height
	if w == 0 {
		w = 800
	}
	if h == 0 {
		h = 600
	}

	win := &Window{app: a, id: winID, webviewID: wvID}
	win.ipcBridge = newBridgeForWindow(win)

	a.mu.Lock()
	a.windows[winID] = win
	a.webviewOwn[wvID] = win
	a.mu.Unlock()

	a.sendJSON(map[string]any{
		"type":      "window.create",
		"id":        winID,
		"title":     cfg.Title,
		"width":     w,
		"height":    h,
		"resizable": !cfg.NoResize,
		"style":     "default",
	})

	a.sendJSON(map[string]any{
		"type":      "webview.create",
		"id":        wvID,
		"window_id": winID,
		"mode":      "window",
		"devtools":  cfg.Debug,
	})

	return win
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}