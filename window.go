package arc

// window.go — Window: a native window with a primary window-backed WebView.

import (
	"sync"

	"github.com/carbon-os/arc/webview"
	wcfg "github.com/carbon-os/arc/window"
)

// Window represents a native window and its primary window-backed WebView.
// Obtain a Window from App.NewWindow or App.NewBrowserWindow inside the
// App.OnReady callback.
type Window struct {
	app       *App
	id        string // window ID (arc-internal, sent as "id" in window.* commands)
	webviewID string // primary window-backed WebView ID

	mu              sync.RWMutex
	onReady         func()
	onResize        func(w, h int)
	onMove          func(x, y int)
	onFocus         func()
	onBlur          func()
	onClose         func()
	onStateChange   func(state string)
	onNavigate      func(url string)
	onTitleChange   func(title string)
	onLoadStart     func(url string)
	onLoadFinish    func(url string)
	onLoadFailed    func(url, errMsg string)

	ipcBridge *IPCBridge
	overlays  sync.Map // id → *WebView
}

// ── Event registration ────────────────────────────────────────────────────────

// OnReady registers fn to be called once the window's primary WebView has
// finished initialising. Load content and register IPC handlers here.
func (w *Window) OnReady(fn func()) {
	w.mu.Lock()
	w.onReady = fn
	w.mu.Unlock()
}

// OnResize registers fn to be called whenever the window is resized.
// width and height are the content-area dimensions in logical pixels
// (excluding the native title bar on macOS).
func (w *Window) OnResize(fn func(width, height int)) {
	w.mu.Lock()
	w.onResize = fn
	w.mu.Unlock()
}

// OnMove registers fn to be called when the window is moved.
func (w *Window) OnMove(fn func(x, y int)) {
	w.mu.Lock()
	w.onMove = fn
	w.mu.Unlock()
}

// OnFocus registers fn to be called when the window gains keyboard focus.
func (w *Window) OnFocus(fn func()) {
	w.mu.Lock()
	w.onFocus = fn
	w.mu.Unlock()
}

// OnBlur registers fn to be called when the window loses keyboard focus.
func (w *Window) OnBlur(fn func()) {
	w.mu.Lock()
	w.onBlur = fn
	w.mu.Unlock()
}

// OnClose registers fn to be called when the user closes the window.
// The window is destroyed by the host; perform any cleanup here.
func (w *Window) OnClose(fn func()) {
	w.mu.Lock()
	w.onClose = fn
	w.mu.Unlock()
}

// OnStateChange registers fn to be called when the window's display state
// changes. state is one of "normal", "minimized", "maximized", "fullscreen".
func (w *Window) OnStateChange(fn func(state string)) {
	w.mu.Lock()
	w.onStateChange = fn
	w.mu.Unlock()
}

// OnNavigate registers fn to be called when the primary WebView navigates
// to a new URL.
func (w *Window) OnNavigate(fn func(url string)) {
	w.mu.Lock()
	w.onNavigate = fn
	w.mu.Unlock()
}

// OnTitleChange registers fn to be called when the page <title> changes.
func (w *Window) OnTitleChange(fn func(title string)) {
	w.mu.Lock()
	w.onTitleChange = fn
	w.mu.Unlock()
}

// OnLoadStart registers fn to be called when the primary WebView begins
// navigating to a new URL. Fired before any content is received.
func (w *Window) OnLoadStart(fn func(url string)) {
	w.mu.Lock()
	w.onLoadStart = fn
	w.mu.Unlock()
}

// OnLoadFinish registers fn to be called when the primary WebView has
// finished loading its content successfully.
func (w *Window) OnLoadFinish(fn func(url string)) {
	w.mu.Lock()
	w.onLoadFinish = fn
	w.mu.Unlock()
}

// OnLoadFailed registers fn to be called when the primary WebView fails
// to load a URL. errMsg contains a human-readable description.
func (w *Window) OnLoadFailed(fn func(url, errMsg string)) {
	w.mu.Lock()
	w.onLoadFailed = fn
	w.mu.Unlock()
}

// ── IPC ───────────────────────────────────────────────────────────────────────

// IPC returns the IPC bridge for this window's primary WebView.
func (w *Window) IPC() *IPCBridge { return w.ipcBridge }

// ── Content ───────────────────────────────────────────────────────────────────

// LoadURL navigates the primary WebView to url.
func (w *Window) LoadURL(url string) {
	w.app.sendJSON(map[string]any{"type": "webview.load_url", "id": w.webviewID, "url": url})
}

// LoadHTML loads a raw HTML string into the primary WebView.
func (w *Window) LoadHTML(html string) {
	w.app.sendJSON(map[string]any{"type": "webview.load_html", "id": w.webviewID, "html": html})
}

// LoadFile loads a local HTML file (by filesystem path) into the primary WebView.
func (w *Window) LoadFile(path string) {
	w.app.sendJSON(map[string]any{"type": "webview.load_file", "id": w.webviewID, "path": path})
}

// Reload reloads the current page in the primary WebView.
func (w *Window) Reload() {
	w.app.sendJSON(map[string]any{"type": "webview.reload", "id": w.webviewID})
}

// GoBack navigates the primary WebView back in its history.
func (w *Window) GoBack() {
	w.app.sendJSON(map[string]any{"type": "webview.go_back", "id": w.webviewID})
}

// GoForward navigates the primary WebView forward in its history.
func (w *Window) GoForward() {
	w.app.sendJSON(map[string]any{"type": "webview.go_forward", "id": w.webviewID})
}

// Eval executes js in the primary WebView's JavaScript context.
// Results are discarded; use IPC if you need a return value.
func (w *Window) Eval(js string) {
	w.app.sendJSON(map[string]any{"type": "webview.eval", "id": w.webviewID, "js": js})
}

// SetZoom sets the zoom factor of the primary WebView (1.0 = 100%).
func (w *Window) SetZoom(factor float64) {
	w.app.sendJSON(map[string]any{"type": "webview.set_zoom", "id": w.webviewID, "zoom": factor})
}

// ── Window management ─────────────────────────────────────────────────────────

// Show makes the window visible.
func (w *Window) Show() {
	w.app.sendJSON(map[string]any{"type": "window.show", "id": w.id})
}

// Hide hides the window without destroying it.
func (w *Window) Hide() {
	w.app.sendJSON(map[string]any{"type": "window.hide", "id": w.id})
}

// Focus brings the window to the front and gives it keyboard focus.
func (w *Window) Focus() {
	w.app.sendJSON(map[string]any{"type": "window.focus", "id": w.id})
}

// Minimize minimises the window to the taskbar / Dock.
func (w *Window) Minimize() {
	w.app.sendJSON(map[string]any{"type": "window.minimize", "id": w.id})
}

// Maximize maximises the window to fill the screen.
func (w *Window) Maximize() {
	w.app.sendJSON(map[string]any{"type": "window.maximize", "id": w.id})
}

// Restore restores the window from a minimised or maximised state.
func (w *Window) Restore() {
	w.app.sendJSON(map[string]any{"type": "window.restore", "id": w.id})
}

// SetFullscreen enters or exits fullscreen mode.
func (w *Window) SetFullscreen(enabled bool) {
	w.app.sendJSON(map[string]any{"type": "window.set_fullscreen", "id": w.id, "fullscreen": enabled})
}

// SetTitle changes the window title bar text.
func (w *Window) SetTitle(title string) {
	w.app.sendJSON(map[string]any{"type": "window.set_title", "id": w.id, "title": title})
}

// SetSize changes the window's client-area dimensions.
func (w *Window) SetSize(width, height int) {
	w.app.sendJSON(map[string]any{"type": "window.set_size", "id": w.id, "width": width, "height": height})
}

// SetPosition moves the window to the given screen coordinates.
func (w *Window) SetPosition(x, y int) {
	w.app.sendJSON(map[string]any{"type": "window.set_position", "id": w.id, "x": x, "y": y})
}

// Center centres the window on its current display.
func (w *Window) Center() {
	w.app.sendJSON(map[string]any{"type": "window.center", "id": w.id})
}

// SetMinSize constrains the minimum resizable dimensions of the window.
func (w *Window) SetMinSize(width, height int) {
	w.app.sendJSON(map[string]any{"type": "window.set_min_size", "id": w.id, "width": width, "height": height})
}

// SetMaxSize constrains the maximum resizable dimensions of the window.
func (w *Window) SetMaxSize(width, height int) {
	w.app.sendJSON(map[string]any{"type": "window.set_max_size", "id": w.id, "width": width, "height": height})
}

// SetAlwaysOnTop pins the window above all other windows when true.
func (w *Window) SetAlwaysOnTop(on bool) {
	w.app.sendJSON(map[string]any{"type": "window.set_always_on_top", "id": w.id, "value": on})
}

// SetEffect applies a platform backdrop effect to the window.
// effect is one of: "vibrancy" (macOS), "acrylic" (Windows), "mica" (Windows),
// "mica_alt" (Windows). Pass an empty string to clear.
func (w *Window) SetEffect(effect string) {
	if effect == "" {
		w.ClearEffect()
		return
	}
	w.app.sendJSON(map[string]any{"type": "window.set_effect", "id": w.id, "effect": effect})
}

// ClearEffect removes any backdrop effect from the window.
func (w *Window) ClearEffect() {
	w.app.sendJSON(map[string]any{"type": "window.clear_effect", "id": w.id})
}

// Destroy removes this window and all its WebViews from the host.
func (w *Window) Destroy() {
	w.app.sendJSON(map[string]any{"type": "window.destroy", "id": w.id})
	w.app.mu.Lock()
	delete(w.app.windows, w.id)
	delete(w.app.webviewOwn, w.webviewID)
	w.app.mu.Unlock()
}

// ── Overlay WebViews ──────────────────────────────────────────────────────────

// NewWebView creates a view-backed (overlay) WebView on this window.
// Overlays float on top of the window's primary WebView at a fixed position and
// can be shown, hidden, repositioned, and z-ordered independently.
//
// Call inside win.OnReady (or later). The overlay is registered immediately;
// content commands are queued by the host until the WebView is ready.
func (w *Window) NewWebView(cfg webview.Config) *WebView {
	id := w.app.genID("view")

	width := cfg.Width
	if width == 0 {
		width = 400
	}
	height := cfg.Height
	if height == 0 {
		height = 300
	}

	ov := &WebView{app: w.app, id: id, win: w}
	ov.ipcBridge = newBridgeForOverlay(ov)

	w.overlays.Store(id, ov)
	w.app.mu.Lock()
	w.app.overlayOwn[id] = ov
	w.app.mu.Unlock()

	w.app.sendJSON(map[string]any{
		"type":      "webview.create",
		"id":        id,
		"window_id": w.id,
		"mode":      "view",
		"x":         cfg.X,
		"y":         cfg.Y,
		"width":     width,
		"height":    height,
		"z":         cfg.ZOrder,
		"devtools":  cfg.Debug,
	})

	return ov
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// fireReady calls onReady under the read lock.
func (w *Window) fireReady() {
	w.mu.RLock()
	fn := w.onReady
	w.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// Satisfy the wcfg import so the compiler doesn't prune it.
var _ = wcfg.Config{}