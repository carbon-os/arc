// Package window provides window creation config and the Window handle.
package window

import (
	"sync"

	"github.com/carbon-os/arc/webview"
)

// Config holds window creation parameters.
type Config struct {
	Title  string
	Width  int // default 1280
	Height int // default 800

	MinWidth  int
	MinHeight int
	MaxWidth  int // 0 = unlimited
	MaxHeight int // 0 = unlimited

	Resizable   bool
	Center      bool
	Frameless   bool
	Transparent bool
	AlwaysOnTop bool

	// macOS-specific
	MacVibrancy      string
	MacTitleBarStyle string

	// Windows-specific
	WinMica bool
}

type (
	sendFn       = func(cmd any)
	newWebViewFn = func(windowID string, cfg webview.Config) *webview.WebView
)

// Window is a handle to a native application window.
type Window struct {
	mu    sync.Mutex
	id    string
	ready bool // true once SetID has been called

	send       sendFn
	newWebView newWebViewFn

	// lazy root WebView for the convenience LoadHTML/LoadURL/LoadFile methods
	rootOnce sync.Once
	rootWV   *webview.WebView

	// event callbacks
	onReady  func()
	onClose  func()
	onResize func(width, height int)
	onMove   func(x, y int)
	onFocus  func()
	onBlur   func()
}

// New creates a Window. Called by arc internals.
func New(cfg Config, send sendFn, newWebView newWebViewFn) *Window {
	return &Window{send: send, newWebView: newWebView}
}

// SetID is called by arc internals when the renderer assigns a window id.
// It triggers the OnReady callback if one has already been registered.
func (w *Window) SetID(id string) {
	w.mu.Lock()
	w.id = id
	w.ready = true
	fn := w.onReady
	w.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (w *Window) getID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.id
}

// ── Events ────────────────────────────────────────────────────────────────────

// OnReady registers fn to be called once the native window is ready.
// If the window is already ready when OnReady is called, fn is called immediately.
func (w *Window) OnReady(fn func()) {
	w.mu.Lock()
	already := w.ready
	w.onReady = fn
	w.mu.Unlock()
	if already {
		fn()
	}
}

func (w *Window) OnClose(fn func())  { w.mu.Lock(); w.onClose = fn; w.mu.Unlock() }
func (w *Window) OnResize(fn func(width, height int)) {
	w.mu.Lock(); w.onResize = fn; w.mu.Unlock()
}
func (w *Window) OnMove(fn func(x, y int)) { w.mu.Lock(); w.onMove = fn; w.mu.Unlock() }
func (w *Window) OnFocus(fn func())        { w.mu.Lock(); w.onFocus = fn; w.mu.Unlock() }
func (w *Window) OnBlur(fn func())         { w.mu.Lock(); w.onBlur = fn; w.mu.Unlock() }

// ── Content (convenience wrappers over an implicit root WebView) ──────────────

func (w *Window) LoadHTML(html string) { w.rootWebView().LoadHTML(html) }
func (w *Window) LoadURL(url string)   { w.rootWebView().LoadURL(url) }
func (w *Window) LoadFile(path string) { w.rootWebView().LoadFile(path) }

func (w *Window) rootWebView() *webview.WebView {
	w.rootOnce.Do(func() {
		w.rootWV = w.newWebView(w.getID(), webview.Config{Layout: "root"})
	})
	return w.rootWV
}

// ── WebView management ────────────────────────────────────────────────────────

// NewWebView creates an overlay WebView inside this window.
// Must be called from within OnReady or after.
func (w *Window) NewWebView(cfg webview.Config) *webview.WebView {
	return w.newWebView(w.getID(), cfg)
}

// ── Window control ────────────────────────────────────────────────────────────

func (w *Window) SetTitle(title string) {
	w.send(map[string]any{"cmd": "window_set_title", "window_id": w.getID(), "title": title})
}
func (w *Window) SetSize(width, height int) {
	w.send(map[string]any{"cmd": "window_set_size", "window_id": w.getID(), "width": width, "height": height})
}
func (w *Window) SetMinSize(width, height int) {
	w.send(map[string]any{"cmd": "window_set_min_size", "window_id": w.getID(), "width": width, "height": height})
}
func (w *Window) SetMaxSize(width, height int) {
	w.send(map[string]any{"cmd": "window_set_max_size", "window_id": w.getID(), "width": width, "height": height})
}
func (w *Window) Center() {
	w.send(map[string]any{"cmd": "window_center", "window_id": w.getID()})
}
func (w *Window) SetFullscreen(on bool) {
	w.send(map[string]any{"cmd": "window_fullscreen", "window_id": w.getID(), "enabled": on})
}
func (w *Window) Minimize() {
	w.send(map[string]any{"cmd": "window_minimize", "window_id": w.getID()})
}
func (w *Window) Maximize() {
	w.send(map[string]any{"cmd": "window_maximize", "window_id": w.getID()})
}
func (w *Window) Close() {
	w.send(map[string]any{"cmd": "window_close", "window_id": w.getID()})
}

// ── DispatchEvent — called by arc internals ───────────────────────────────────

func (w *Window) DispatchEvent(event string, j map[string]any) {
	w.mu.Lock()
	onClose  := w.onClose
	onResize := w.onResize
	onMove   := w.onMove
	onFocus  := w.onFocus
	onBlur   := w.onBlur
	w.mu.Unlock()

	switch event {
	case "window_closed":
		if onClose != nil {
			onClose()
		}
	case "window_resized":
		if onResize != nil {
			onResize(intField(j, "width"), intField(j, "height"))
		}
	case "window_moved":
		if onMove != nil {
			onMove(intField(j, "x"), intField(j, "y"))
		}
	case "window_focused":
		if onFocus != nil {
			onFocus()
		}
	case "window_unfocused":
		if onBlur != nil {
			onBlur()
		}
	}
}

func intField(j map[string]any, key string) int {
	if f, ok := j[key].(float64); ok {
		return int(f)
	}
	return 0
}