package arc

// webview.go — WebView: a view-backed (overlay) WebView inside a Window.

import "sync"

// WebView represents a view-backed (overlay) WebView floating inside a Window.
// Obtain one from Window.NewWebView; do not construct directly.
type WebView struct {
	app *App
	id  string
	win *Window

	mu              sync.RWMutex
	onReady         func()
	onNavigate      func(url string)
	onTitleChange   func(title string)
	onLoadStart     func(url string)
	onLoadFinish    func(url string)
	onLoadFailed    func(url, errMsg string)

	ipcBridge *IPCBridge
}

// IPC returns the IPC bridge for this overlay WebView.
func (wv *WebView) IPC() *IPCBridge { return wv.ipcBridge }

// ── Event registration ────────────────────────────────────────────────────────

// OnReady registers fn to be called once this overlay WebView is initialised.
func (wv *WebView) OnReady(fn func()) {
	wv.mu.Lock()
	wv.onReady = fn
	wv.mu.Unlock()
}

// OnNavigate registers fn to be called when this overlay navigates to a new URL.
func (wv *WebView) OnNavigate(fn func(url string)) {
	wv.mu.Lock()
	wv.onNavigate = fn
	wv.mu.Unlock()
}

// OnTitleChange registers fn to be called when the overlay page <title> changes.
func (wv *WebView) OnTitleChange(fn func(title string)) {
	wv.mu.Lock()
	wv.onTitleChange = fn
	wv.mu.Unlock()
}

// OnLoadStart registers fn to be called when this overlay begins loading a URL.
func (wv *WebView) OnLoadStart(fn func(url string)) {
	wv.mu.Lock()
	wv.onLoadStart = fn
	wv.mu.Unlock()
}

// OnLoadFinish registers fn to be called when this overlay finishes loading successfully.
func (wv *WebView) OnLoadFinish(fn func(url string)) {
	wv.mu.Lock()
	wv.onLoadFinish = fn
	wv.mu.Unlock()
}

// OnLoadFailed registers fn to be called when this overlay fails to load a URL.
func (wv *WebView) OnLoadFailed(fn func(url, errMsg string)) {
	wv.mu.Lock()
	wv.onLoadFailed = fn
	wv.mu.Unlock()
}

// ── Content ───────────────────────────────────────────────────────────────────

// LoadURL navigates this overlay to url.
func (wv *WebView) LoadURL(url string) {
	wv.app.sendJSON(map[string]any{"type": "webview.load_url", "id": wv.id, "url": url})
}

// LoadHTML loads a raw HTML string into this overlay.
func (wv *WebView) LoadHTML(html string) {
	wv.app.sendJSON(map[string]any{"type": "webview.load_html", "id": wv.id, "html": html})
}

// Reload reloads the current page in this overlay.
func (wv *WebView) Reload() {
	wv.app.sendJSON(map[string]any{"type": "webview.reload", "id": wv.id})
}

// GoBack navigates this overlay back in its history.
func (wv *WebView) GoBack() {
	wv.app.sendJSON(map[string]any{"type": "webview.go_back", "id": wv.id})
}

// GoForward navigates this overlay forward in its history.
func (wv *WebView) GoForward() {
	wv.app.sendJSON(map[string]any{"type": "webview.go_forward", "id": wv.id})
}

// Eval executes js in this overlay's JavaScript context.
func (wv *WebView) Eval(js string) {
	wv.app.sendJSON(map[string]any{"type": "webview.eval", "id": wv.id, "js": js})
}

// SetZoom sets the zoom factor of this overlay (1.0 = 100%).
func (wv *WebView) SetZoom(factor float64) {
	wv.app.sendJSON(map[string]any{"type": "webview.set_zoom", "id": wv.id, "zoom": factor})
}

// ── Geometry ──────────────────────────────────────────────────────────────────

// Show makes this overlay visible.
func (wv *WebView) Show() {
	wv.app.sendJSON(map[string]any{"type": "webview.show", "id": wv.id})
}

// Hide hides this overlay.
func (wv *WebView) Hide() {
	wv.app.sendJSON(map[string]any{"type": "webview.hide", "id": wv.id})
}

// SetPosition moves this overlay within its parent window.
func (wv *WebView) SetPosition(x, y int) {
	wv.app.sendJSON(map[string]any{"type": "webview.set_position", "id": wv.id, "x": x, "y": y})
}

// SetSize resizes this overlay.
func (wv *WebView) SetSize(width, height int) {
	wv.app.sendJSON(map[string]any{"type": "webview.set_size", "id": wv.id, "width": width, "height": height})
}

// SetBounds is a convenience method combining SetPosition and SetSize.
func (wv *WebView) SetBounds(x, y, width, height int) {
	wv.SetPosition(x, y)
	wv.SetSize(width, height)
}

// SetZOrder changes the stacking order of this overlay among its siblings.
// Higher z values appear on top of lower z values. The host re-sorts all
// overlays on this window immediately so the change takes effect at once.
func (wv *WebView) SetZOrder(z int) {
	wv.app.sendJSON(map[string]any{"type": "webview.set_zorder", "id": wv.id, "z": z})
}

// BringToFront is a convenience wrapper for SetZOrder that moves this overlay
// above all siblings by setting an arbitrarily high z value.
// Prefer SetZOrder when you need precise layering among several overlays.
func (wv *WebView) BringToFront() {
	wv.SetZOrder(1<<30)
}

// SendToBack is a convenience wrapper for SetZOrder that moves this overlay
// below all siblings by setting an arbitrarily low z value.
func (wv *WebView) SendToBack() {
	wv.SetZOrder(-(1 << 30))
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

// Destroy removes this overlay WebView from the host and the parent window.
func (wv *WebView) Destroy() {
	wv.app.sendJSON(map[string]any{"type": "webview.destroy", "id": wv.id})
	wv.win.overlays.Delete(wv.id)
	wv.app.mu.Lock()
	delete(wv.app.overlayOwn, wv.id)
	wv.app.mu.Unlock()
}

// ── Internal ──────────────────────────────────────────────────────────────────

func (wv *WebView) fireReady() {
	wv.mu.RLock()
	fn := wv.onReady
	wv.mu.RUnlock()
	if fn != nil {
		fn()
	}
}