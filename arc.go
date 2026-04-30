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

// HostConfig controls how the arc-host process is located or connected to.
type HostConfig struct {
	// Path is the absolute (or relative) path to the arc-host binary.
	// Default: looks for arc-host next to the running executable.
	Path string

	// Channel is a pre-negotiated channel id. When non-empty, arc will NOT
	// spawn arc-host — it will connect to the already-running instance at
	// the socket / named-pipe derived from this id.
	// Useful for tests or when arc-host is managed externally.
	Channel string
}

// AppConfig configures the application.
type AppConfig struct {
	// Human-readable name shown as the default window title.
	Title string

	// Emit verbose logs from arc-host and the SDK. Default false.
	Logging bool

	// Host controls arc-host binary location and connection.
	// Leave zero-value for automatic discovery next to the executable.
	Host HostConfig
}

// NewApp constructs an App. No host process is spawned yet.
func NewApp(cfg AppConfig) *App {
	return &App{
		cfg:     cfg,
		windows: make(map[string]*window.Window),
		views:   make(map[string]*webview.WebView),
		done:    make(chan struct{}),
	}
}

// App is the top-level application handle.
type App struct {
	cfg AppConfig

	conn    net.Conn
	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   []pendingEntry

	winMu   sync.RWMutex
	windows map[string]*window.Window

	viewMu sync.RWMutex
	views  map[string]*webview.WebView

	onReady func()
	onClose func() // bool removed: "closed" fires after NSApp has already exited, nothing to veto

	done chan struct{}
}

type pendingEntry struct {
	event   string
	resolve func(id string)
}

func (a *App) OnReady(fn func())      { a.onReady = fn }
func (a *App) OnClose(fn func())      { a.onClose = fn }

func (a *App) NewWindow(cfg window.Config) *window.Window {
	win := window.New(cfg, a.sendJSON, a.newWebViewFn)

	// NOTE: pending entries are consumed FIFO per event type.
	// window_ready entries must be resolved in the same order the host emits them,
	// which matches the order window_create commands were sent — safe as long as
	// commands are written serially (they are, via writeMu).
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

func (a *App) newWebViewFn(windowID string, cfg webview.Config) *webview.WebView {
	view := webview.New(cfg, a.sendJSON, a.sendBinaryMsg)

	// Same FIFO ordering assumption as NewWindow above.
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

func (a *App) Quit() {
	a.sendJSON(map[string]any{"cmd": "quit"})
}

// ── Connection ────────────────────────────────────────────────────────────────

func (a *App) connect() (net.Conn, error) {
	// Pre-negotiated channel — connect directly, no spawn.
	if a.cfg.Host.Channel != "" {
		return dialRenderer(channelToPath(a.cfg.Host.Channel))
	}

	// Locate arc-host binary.
	hostPath := a.cfg.Host.Path
	if hostPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("arc: locate executable: %w", err)
		}
		hostPath = filepath.Join(filepath.Dir(exe), "arc-host")
		if runtime.GOOS == "windows" {
			hostPath += ".exe"
		}
	}

	var args []string
	if a.cfg.Logging {
		args = append(args, "--logging")
	}

	cmd := exec.Command(hostPath, args...)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("arc: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("arc: spawn arc-host (%s): %w", hostPath, err)
	}

	// arc-host writes the socket path to stdout before blocking on accept().
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("arc: arc-host did not write socket path to stdout")
	}
	socketPath := strings.TrimSpace(scanner.Text())

	// Drain the rest of stdout so arc-host never blocks on a full pipe.
	go io.Copy(io.Discard, stdout)

	conn, err := dialRenderer(socketPath)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("arc: connect to arc-host at %s: %w", socketPath, err)
	}
	return conn, nil
}

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

// ── Event reader ──────────────────────────────────────────────────────────────

func (a *App) readLoop() {
	defer close(a.done)
	for {
		typ, payload, err := a.readFrame()
		if err != nil {
			return
		}
		if typ == 0x01 {
			if stop := a.dispatchJSON(payload); stop {
				return
			}
		}
	}
}

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
		// Fired after NSApp has already exited — inform the caller, then stop.
		if a.onClose != nil {
			a.onClose()
		}
		return true

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

	case "view_ready":
		vid, _ := j["view_id"].(string)
		if fn := a.popPending("view_ready"); fn != nil {
			fn(vid)
		}

	// ipc_text is listed explicitly so it doesn't fall through to the generic
	// view-event default branch (which would also work today, but is fragile).
	case "ipc_text":
		vid, _ := j["view_id"].(string)
		a.viewMu.RLock()
		view := a.views[vid]
		a.viewMu.RUnlock()
		if view != nil {
			view.DispatchEvent(event, j, nil)
		}

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
		// All remaining events that carry a view_id are routed to the WebView.
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

func (a *App) sendJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	a.writeFrame(0x01, b)
}

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
	_, _ = a.conn.Write(hdr[:])
	if len(payload) > 0 {
		_, _ = a.conn.Write(payload)
	}
}

// ── Command builders ──────────────────────────────────────────────────────────

func windowCreateCmd(cfg window.Config) map[string]any {
	cmd := map[string]any{
		"cmd": "window_create", "title": cfg.Title,
		"width": cfg.Width, "height": cfg.Height,
		"min_width": cfg.MinWidth, "min_height": cfg.MinHeight,
		"max_width": cfg.MaxWidth, "max_height": cfg.MaxHeight,
		"resizable": cfg.Resizable, "center": cfg.Center,
		"frameless": cfg.Frameless, "transparent": cfg.Transparent,
		"always_on_top": cfg.AlwaysOnTop,
	}
	if cfg.Width == 0  { cmd["width"] = 1280 }
	if cfg.Height == 0 { cmd["height"] = 800 }
	if cfg.MacVibrancy != "" || cfg.MacTitleBarStyle != "" || cfg.WinMica {
		plat := map[string]any{}
		if cfg.MacVibrancy != "" || cfg.MacTitleBarStyle != "" {
			mac := map[string]any{}
			if cfg.MacVibrancy != ""      { mac["vibrancy"] = cfg.MacVibrancy }
			if cfg.MacTitleBarStyle != "" { mac["title_bar_style"] = cfg.MacTitleBarStyle }
			plat["mac"] = mac
		}
		if cfg.WinMica { plat["win"] = map[string]any{"mica": true} }
		cmd["platform"] = plat
	}
	return cmd
}

func viewCreateCmd(windowID string, cfg webview.Config) map[string]any {
	return map[string]any{
		"cmd": "view_create", "window_id": windowID, "view_type": "webview",
		"layout": cfg.Layout, "x": cfg.X, "y": cfg.Y,
		"width": cfg.Width, "height": cfg.Height, "z": cfg.ZOrder,
	}
}

// keep ipc imported (used by callers via re-export of ipc.Handle)
var _ = ipc.NewHandle