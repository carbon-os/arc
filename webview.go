package arc

// webview.go — WebView: a view-backed (overlay) WebView inside a Window.

import "sync"

// WebView represents a view-backed (overlay) WebView floating inside a Window.
// Obtain one from Window.NewWebView; do not construct directly.
type WebView struct {
	app *App
	id  string
	win *Window

	mu      sync.RWMutex
	onReady func()

	ipcBridge *IPCBridge
}

// IPC returns the IPC bridge for this overlay WebView.
func (wv *WebView) IPC() *IPCBridge { return wv.ipcBridge }

// OnReady registers fn to be called once this overlay WebView is initialised.
func (wv *WebView) OnReady(fn func()) {
	wv.mu.Lock()
	wv.onReady = fn
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

// Eval executes js in this overlay's JavaScript context.
func (wv *WebView) Eval(js string) {
	wv.app.sendJSON(map[string]any{"type": "webview.eval", "id": wv.id, "js": js})
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