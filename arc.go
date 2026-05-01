// Package arc is the Go controller for the Arc native UI runtime (arc-host).
//
// arc-host owns the platform event loop, windows, views, and WebViews.
// This package is the controller: it connects to arc-host over libipc,
// issues commands (create windows, load content, …), and receives events
// back (resize, close, JS IPC messages, …).
//
// # Deployment modes
//
// Managed — Go spawns arc-host itself:
//
//	app := arc.NewApp(arc.AppConfig{
//	    Renderer: arc.RendererConfig{Path: "/path/to/arc-host"},
//	})
//
// External — arc-host is already running (useful for testing):
//
//	app := arc.NewApp(arc.AppConfig{
//	    Ipc: ipc.Config{Channel: "my-channel-id"},
//	})
//
// # Typical usage
//
//	app.OnReady(func() {
//	    win := app.NewBrowserWindow(window.Config{Title: "My App", Width: 1280, Height: 800})
//	    win.OnReady(func() {
//	        win.LoadHTML(myHTML)
//	    })
//	})
//	app.OnClose(func() bool { return true })
//	if err := app.Run(); err != nil {
//	    log.Fatal(err)
//	}
package arc

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	arcIpc "github.com/carbon-os/arc/ipc"
	wcfg "github.com/carbon-os/arc/window"
)

// ─── Configuration ────────────────────────────────────────────────────────────

// RendererConfig tells the App how to locate and launch the arc-host binary.
type RendererConfig struct {
	// Path is the filesystem path to the arc-host executable.
	Path string
}

// AppConfig holds top-level application parameters.
type AppConfig struct {
	// Title is the application name forwarded to arc-host in host.configure.
	Title string

	// Logging enables verbose debug logging to stderr.
	Logging bool

	// Renderer causes Run to spawn arc-host from the given path.
	// Leave zero-value if arc-host is managed externally.
	Renderer RendererConfig

	// Ipc allows connecting to an already-running arc-host instance by
	// specifying its channel ID.  Takes effect when Renderer.Path is empty.
	Ipc arcIpc.Config
}

// ─── App ──────────────────────────────────────────────────────────────────────

// App is the root object that manages the connection to arc-host.
// Create with NewApp, register callbacks, then call Run.
type App struct {
	cfg AppConfig

	conn   net.Conn   // set during Run; not accessed after readLoop exits
	sendMu sync.Mutex // serialises concurrent writes to conn

	onReady func()
	onClose func() bool

	// Entity maps — protected by mu.
	mu         sync.RWMutex
	windows    map[string]*Window  // window ID  → Window
	webviewOwn map[string]*Window  // webview ID → owning Window (primary wv only)
	overlayOwn map[string]*WebView // overlay ID → WebView

	hostCmd   *exec.Cmd
	channelID string
	done      chan struct{}
	idSeq     uint64 // accessed via sync/atomic
}

// NewApp creates a new App from cfg. Register callbacks before calling Run.
func NewApp(cfg AppConfig) *App {
	return &App{
		cfg:        cfg,
		windows:    make(map[string]*Window),
		webviewOwn: make(map[string]*Window),
		overlayOwn: make(map[string]*WebView),
		done:       make(chan struct{}),
	}
}

// OnReady registers fn to be called after arc-host has connected and confirmed
// configuration. Create windows and WebViews here.
func (a *App) OnReady(fn func()) { a.onReady = fn }

// OnClose registers fn to be called when arc-host disconnects (e.g. after the
// last window is closed or Shutdown is called). Return true to allow the
// process to exit normally.
func (a *App) OnClose(fn func() bool) { a.onClose = fn }

// Shutdown sends a graceful shutdown command to arc-host.
// Run will return shortly after.
func (a *App) Shutdown() {
	a.sendJSON(map[string]any{"type": "host.shutdown"})
}

// Run connects to (or spawns) arc-host and enters the event loop.
// It blocks until arc-host disconnects and returns any fatal error.
//
// Run must be called from the main goroutine (or at minimum only once).
func (a *App) Run() error {
	// ── Determine channel ID ──────────────────────────────────────────────────
	a.channelID = a.cfg.Ipc.Channel
	if a.channelID == "" {
		a.channelID = fmt.Sprintf("arc-%d-%d", os.Getpid(), time.Now().UnixNano())
	}

	// ── Spawn arc-host if a renderer path is configured ───────────────────────
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

	// ── Connect — retry for up to 10 s to allow arc-host time to bind ─────────
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

	// ── Start read loop ───────────────────────────────────────────────────────
	go a.readLoop()

	// ── Block until arc-host disconnects ──────────────────────────────────────
	<-a.done

	if a.hostCmd != nil {
		_ = a.hostCmd.Wait()
	}
	return nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// genID returns a unique ID string with prefix.
func (a *App) genID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, atomic.AddUint64(&a.idSeq, 1))
}

// sendJSON marshals v and writes it as a JSON frame. Errors are logged.
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
		// Binary frames are not used at the app level in the current protocol.
		// WebView binary IPC is mediated through JSON webview.ipc events.
	}
}

// envelope is the minimal set of fields needed to route an inbound event.
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

	// ── Host lifecycle ────────────────────────────────────────────────────────

	case "host.ready":
		a.sendJSON(map[string]any{
			"type":     "host.configure",
			"app_name": a.cfg.Title,
		})

	case "host.configured":
		if a.onReady != nil {
			a.onReady()
		}

	case "host.pong": // response to a Ping — no action needed

	// ── Window events (ev.ID = window ID) ────────────────────────────────────

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

	// ── WebView events (ev.ID = webview or overlay ID) ────────────────────────

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
		}

	case "webview.title":
		if w := a.wvWin(ev.ID); w != nil {
			w.mu.RLock()
			fn := w.onTitleChange
			w.mu.RUnlock()
			if fn != nil {
				fn(ev.Title)
			}
		}

	case "webview.console":
		if a.cfg.Logging {
			log.Printf("[arc:console wv=%s] [%s] %s", ev.ID, ev.Level, ev.Text)
		}
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

// NewWindow creates a native window with an embedded window-backed WebView.
// Call inside the App.OnReady callback.
// Use win.OnReady to know when the WebView is initialised and ready for content.
func (a *App) NewWindow(cfg wcfg.Config) *Window {
	return a.newWindow(cfg)
}

// NewBrowserWindow is a semantic alias for NewWindow, intended for windows
// whose primary purpose is to host a navigable web surface.
func (a *App) NewBrowserWindow(cfg wcfg.Config) *Window {
	return a.newWindow(cfg)
}

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

// ── Utilities ─────────────────────────────────────────────────────────────────

// clip truncates s to at most n bytes, appending "…" if trimmed.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}