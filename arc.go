// Package arc provides the application lifecycle and top-level entry point
// for building native desktop applications backed by a platform-native WebView
// renderer (arc-host).
package arc

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/carbon-os/arc/ipc"
	"github.com/carbon-os/arc/webview"
	"github.com/carbon-os/arc/window"
)

// AppConfig configures the application.
type AppConfig struct {
	// Human-readable name shown as the default window title.
	Title string

	// Emit verbose logs from the renderer and SDK.  Default false.
	Logging bool

	// RendererPath overrides the location of the arc-host binary.
	// Default: looks for arc-host next to the current executable.
	RendererPath string

	// Ipc lets the caller skip automatic renderer spawning and instead hand
	// Arc a pre-negotiated channel id (e.g. in tests).
	// Leave zero-value to let Arc spawn arc-host automatically.
	Ipc ipc.Config
}

// App is the top-level application handle.
type App struct {
	cfg AppConfig

	// transport
	conn    net.Conn
	writeMu sync.Mutex

	// FIFO queue of pending window_ready / view_ready resolutions.
	// Arc-host processes commands in order and responds in order, so
	// a simple FIFO is sufficient to match responses to requests.
	pendingMu sync.Mutex
	pending   []pendingEntry

	// registered window and view handles keyed by renderer-assigned id
	winMu   sync.RWMutex
	windows map[string]*window.Window

	viewMu sync.RWMutex
	views  map[string]*webview.WebView

	// lifecycle callbacks
	onReady func()
	onClose func() bool

	// closed when the reader loop exits
	done chan struct{}
}

type pendingEntry struct {
	event   string // "window_ready" or "view_ready"
	resolve func(id string)
}

// NewApp constructs an App.  No renderer is spawned yet.
func NewApp(cfg AppConfig) *App {
	return &App{
		cfg:     cfg,
		windows: make(map[string]*window.Window),
		views:   make(map[string]*webview.WebView),
		done:    make(chan struct{}),
	}
}

// OnReady registers a callback that fires once the renderer is connected and
// ready to accept commands.  Exactly one call.  Must be called before Run.
func (a *App) OnReady(fn func()) { a.onReady = fn }

// OnClose registers a callback that fires when the last window closes or the
// user asks to quit.  Return true to allow the quit, false to cancel it.
func (a *App) OnClose(fn func() bool) { a.onClose = fn }

// NewWindow creates a new native window.  Must be called from inside the
// OnReady callback (or any goroutine after ready has fired).
func (a *App) NewWindow(cfg window.Config) *window.Window {
	win := window.New(cfg, a.sendJSON, a.newWebViewFn)

	a.pendingMu.Lock()
	a.pending = append(a.pending, pendingEntry{
		event: "window_ready",
		resolve: func(id string) {
			win.SetID(id)
			a.winMu.Lock()
			a.windows[id] = win
			a.winMu.Unlock()
		},
	})
	a.pendingMu.Unlock()

	a.sendJSON(windowCreateCmd(cfg))
	return win
}

// newWebViewFn is passed to window.Window so it can create WebViews without
// importing the arc package (avoiding a circular dependency).
func (a *App) newWebViewFn(windowID string, cfg webview.Config) *webview.WebView {
	view := webview.New(cfg, a.sendJSON, a.sendBinaryMsg)

	a.pendingMu.Lock()
	a.pending = append(a.pending, pendingEntry{
		event: "view_ready",
		resolve: func(id string) {
			view.SetID(id)
			a.viewMu.Lock()
			a.views[id] = view
			a.viewMu.Unlock()
		},
	})
	a.pendingMu.Unlock()

	a.sendJSON(viewCreateCmd(windowID, cfg))
	return view
}

// Run starts the renderer process (unless Ipc.Channel is set), blocks on
// reading the event stream, and returns when the app has quit.
func (a *App) Run() error {
	conn, err := a.connect()
	if err != nil {
		return err
	}
	a.conn = conn
	defer conn.Close()

	a.readLoop()
	return nil
}

// Quit requests a clean application exit from any goroutine.
func (a *App) Quit() {
	a.sendJSON(map[string]any{"cmd": "quit"})
}

// ── Renderer connection ───────────────────────────────────────────────────────

func (a *App) connect() (net.Conn, error) {
	if a.cfg.Ipc.Channel != "" {
		path := channelToPath(a.cfg.Ipc.Channel)
		return dialRenderer(path)
	}

	// Locate the arc-host binary.
	rendererPath := a.cfg.RendererPath
	if rendererPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("arc: find executable: %w", err)
		}
		rendererPath = filepath.Join(filepath.Dir(exe), "arc-host")
		if runtime.GOOS == "windows" {
			rendererPath += ".exe"
		}
	}

	// Build argument list.
	var args []string
	if a.cfg.Logging {
		args = append(args, "--logging")
	}

	// Spawn arc-host.  It writes the socket/pipe path to stdout, then waits.
	cmd := exec.Command(rendererPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("arc: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("arc: start arc-host at %s: %w", rendererPath, err)
	}

	// Read the socket path arc-host writes to its stdout.
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("arc: arc-host did not write socket path to stdout")
	}
	socketPath := strings.TrimSpace(scanner.Text())

	conn, err := dialRenderer(socketPath)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("arc: connect to renderer at %s: %w", socketPath, err)
	}
	return conn, nil
}

// channelToPath derives the socket / named-pipe path from a bare channel id,
// mirroring the C++ id_to_path() logic in arc.cpp and arc_host_main.cpp.
func channelToPath(id string) string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\arc-` + id
	}
	tmp := os.Getenv("TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}
	return tmp + "/arc-" + id + ".sock"
}

// ── Event reader loop ─────────────────────────────────────────────────────────

// readLoop reads frames from arc-host until the connection closes or a
// "closed" event is received.  Blocks — called from Run on the caller's goroutine.
func (a *App) readLoop() {
	defer close(a.done)

	for {
		typ, payload, err := a.readFrame()
		if err != nil {
			return // EOF or connection closed → app done
		}

		switch typ {
		case 0x01: // JSON frame
			stop := a.dispatchJSON(payload)
			if stop {
				return
			}
		// 0x02 binary frames are consumed inline inside dispatchJSON for
		// ipc_binary; a standalone binary frame here is unexpected.
		}
	}
}

// dispatchJSON handles a JSON event frame.  Returns true when the read loop
// should stop (i.e. a "closed" event was received).
func (a *App) dispatchJSON(payload []byte) (stop bool) {
	var j map[string]any
	if err := json.Unmarshal(payload, &j); err != nil {
		return false
	}
	event, _ := j["event"].(string)

	switch event {
	case "ready":
		if a.onReady != nil {
			a.onReady()
		}

	case "closed":
		// Renderer is shutting down.  Honor the OnClose gate if set.
		if a.onClose != nil {
			if !a.onClose() {
				// User cancelled — but arc-host is already quitting.
				// Nothing meaningful we can do; just let it close.
			}
		}
		return true // signal readLoop to exit

	// ── Window events ─────────────────────────────────────────────────────

	case "window_ready":
		wid, _ := j["window_id"].(string)
		if fn := a.popPending("window_ready"); fn != nil {
			fn(wid)
		}

	case "window_closed":
		wid, _ := j["window_id"].(string)
		a.winMu.RLock()
		win := a.windows[wid]
		a.winMu.RUnlock()
		if win != nil {
			win.DispatchEvent(event, j)
		}
		a.winMu.Lock()
		delete(a.windows, wid)
		a.winMu.Unlock()

	case "window_resized", "window_moved", "window_focused", "window_unfocused":
		wid, _ := j["window_id"].(string)
		a.winMu.RLock()
		win := a.windows[wid]
		a.winMu.RUnlock()
		if win != nil {
			win.DispatchEvent(event, j)
		}

	// ── View events ───────────────────────────────────────────────────────

	case "view_ready":
		vid, _ := j["view_id"].(string)
		if fn := a.popPending("view_ready"); fn != nil {
			fn(vid)
		}

	// ipc_binary: the binary payload follows immediately as the next frame.
	case "ipc_binary":
		_, binPayload, err := a.readFrame()
		if err != nil {
			return true
		}
		vid, _ := j["view_id"].(string)
		a.viewMu.RLock()
		view := a.views[vid]
		a.viewMu.RUnlock()
		if view != nil {
			view.DispatchEvent(event, j, binPayload)
		}

	default:
		// All remaining events are scoped to a view_id.
		if vid, ok := j["view_id"].(string); ok && vid != "" {
			a.viewMu.RLock()
			view := a.views[vid]
			a.viewMu.RUnlock()
			if view != nil {
				view.DispatchEvent(event, j, nil)
			}
		}
	}
	return false
}

func (a *App) popPending(event string) func(id string) {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	for i, p := range a.pending {
		if p.event == event {
			a.pending = append(a.pending[:i], a.pending[i+1:]...)
			return p.resolve
		}
	}
	return nil
}

// ── Frame I/O ─────────────────────────────────────────────────────────────────

func (a *App) readFrame() (typ byte, payload []byte, err error) {
	var hdr [5]byte
	if _, err = io.ReadFull(a.conn, hdr[:]); err != nil {
		return
	}
	typ = hdr[0]
	n := binary.LittleEndian.Uint32(hdr[1:])
	if n > 0 {
		payload = make([]byte, n)
		_, err = io.ReadFull(a.conn, payload)
	}
	return
}

// sendJSON marshals v and sends it as a JSON frame.
// Safe to call from any goroutine.
func (a *App) sendJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	a.writeFrame(0x01, b)
}

// sendBinaryMsg sends a JSON frame and a binary frame atomically.
// Used for webview_post_binary where the renderer expects both frames
// back-to-back.  Safe to call from any goroutine.
func (a *App) sendBinaryMsg(jsonCmd any, data []byte) {
	b, err := json.Marshal(jsonCmd)
	if err != nil {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	a.writeFrameLocked(0x01, b)
	a.writeFrameLocked(0x02, data)
}

func (a *App) writeFrame(typ byte, payload []byte) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	a.writeFrameLocked(typ, payload)
}

func (a *App) writeFrameLocked(typ byte, payload []byte) {
	if a.conn == nil {
		return
	}
	var hdr [5]byte
	hdr[0] = typ
	binary.LittleEndian.PutUint32(hdr[1:], uint32(len(payload)))
	// Two writes under the same lock — no interleaving possible.
	_, _ = a.conn.Write(hdr[:])
	if len(payload) > 0 {
		_, _ = a.conn.Write(payload)
	}
}

// ── Command builders ──────────────────────────────────────────────────────────

func windowCreateCmd(cfg window.Config) map[string]any {
	cmd := map[string]any{
		"cmd":          "window_create",
		"title":        cfg.Title,
		"width":        cfg.Width,
		"height":       cfg.Height,
		"min_width":    cfg.MinWidth,
		"min_height":   cfg.MinHeight,
		"max_width":    cfg.MaxWidth,
		"max_height":   cfg.MaxHeight,
		"resizable":    cfg.Resizable,
		"center":       cfg.Center,
		"frameless":    cfg.Frameless,
		"transparent":  cfg.Transparent,
		"always_on_top": cfg.AlwaysOnTop,
	}
	// Apply defaults to match the C++ session defaults.
	if cfg.Width == 0 {
		cmd["width"] = 1280
	}
	if cfg.Height == 0 {
		cmd["height"] = 800
	}
	if !cfg.Resizable && cfg.Width == 0 {
		// Resizable defaults to true; only override when explicitly set.
		cmd["resizable"] = true
	}
	if !cfg.Center && cfg.Width == 0 {
		cmd["center"] = true
	}

	// Platform-specific extras.
	if cfg.MacVibrancy != "" || cfg.MacTitleBarStyle != "" || cfg.WinMica {
		plat := map[string]any{}
		if cfg.MacVibrancy != "" || cfg.MacTitleBarStyle != "" {
			mac := map[string]any{}
			if cfg.MacVibrancy != "" {
				mac["vibrancy"] = cfg.MacVibrancy
			}
			if cfg.MacTitleBarStyle != "" {
				mac["title_bar_style"] = cfg.MacTitleBarStyle
			}
			plat["mac"] = mac
		}
		if cfg.WinMica {
			plat["win"] = map[string]any{"mica": true}
		}
		cmd["platform"] = plat
	}
	return cmd
}

func viewCreateCmd(windowID string, cfg webview.Config) map[string]any {
	return map[string]any{
		"cmd":       "view_create",
		"window_id": windowID,
		"view_type": "webview",
		"layout":    cfg.Layout,
		"x":         cfg.X,
		"y":         cfg.Y,
		"width":     cfg.Width,
		"height":    cfg.Height,
		"z":         cfg.ZOrder,
	}
}