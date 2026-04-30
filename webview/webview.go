package webview

import (
	"sync/atomic"

	"github.com/carbon-os/arc/internal/runtime"
)

// Config holds the initial geometry and stacking order for a new WebView.
// X and Y are relative to the main window's top-left corner.
type Config struct {
	X      int
	Y      int
	Width  int
	Height int
	// ZOrder sets the initial stacking position among web views on this window.
	// Lower values are further back; higher values are closer to the front.
	// Has no effect on the main window, which is always behind all web views.
	ZOrder int
}

// WebView is an isolated browser view embedded inside a BrowserWindow.
// It runs in a fully isolated context — separate session, storage, and JS
// environment — with no access to the main window or other web views.
//
// Obtain one via win.NewWebView; do not construct directly.
// WebViews do not support IPC.
type WebView struct {
	id        uint32
	rt        *runtime.Runtime
	destroyed uint32 // atomic; 1 once Destroy has been called
}

// New creates a WebView, sends CmdWebViewCreate, and returns the handle.
// Called by BrowserWindow.NewWebView — do not call directly.
func New(rt *runtime.Runtime, cfg Config) *WebView {
	id := rt.NextWebViewID()
	wv := &WebView{id: id, rt: rt}
	rt.Send(
		runtime.CmdWebViewCreate,
		runtime.EncodeWebViewCreate(id, cfg.X, cfg.Y, cfg.Width, cfg.Height, cfg.ZOrder),
	)
	return wv
}

func (wv *WebView) alive() bool {
	return atomic.LoadUint32(&wv.destroyed) == 0
}

// LoadURL navigates the web view to an external URL.
func (wv *WebView) LoadURL(url string) {
	if !wv.alive() {
		return
	}
	wv.rt.Send(runtime.CmdWebViewLoadURL, runtime.EncodeWebViewLoad(wv.id, url))
}

// LoadFile navigates the web view to a local HTML file.
func (wv *WebView) LoadFile(path string) {
	if !wv.alive() {
		return
	}
	wv.rt.Send(runtime.CmdWebViewLoadFile, runtime.EncodeWebViewLoad(wv.id, path))
}

// LoadHTML loads an inline HTML string into the web view.
func (wv *WebView) LoadHTML(html string) {
	if !wv.alive() {
		return
	}
	wv.rt.Send(runtime.CmdWebViewLoadHTML, runtime.EncodeWebViewLoad(wv.id, html))
}

// Show makes the web view visible.
func (wv *WebView) Show() {
	if !wv.alive() {
		return
	}
	wv.rt.Send(runtime.CmdWebViewShow, runtime.EncodeWebViewID(wv.id))
}

// Hide makes the web view invisible without destroying it.
func (wv *WebView) Hide() {
	if !wv.alive() {
		return
	}
	wv.rt.Send(runtime.CmdWebViewHide, runtime.EncodeWebViewID(wv.id))
}

// Move repositions the web view relative to the main window's top-left corner.
// Prefer SetBounds when repositioning at the same time as calling Show, to
// avoid a visible jump from the two operations arriving as separate frames.
func (wv *WebView) Move(x, y int) {
	if !wv.alive() {
		return
	}
	wv.rt.Send(runtime.CmdWebViewMove, runtime.EncodeWebViewMove(wv.id, x, y))
}

// Resize changes the web view's dimensions.
func (wv *WebView) Resize(width, height int) {
	if !wv.alive() {
		return
	}
	wv.rt.Send(runtime.CmdWebViewResize, runtime.EncodeWebViewResize(wv.id, width, height))
}

// SetBounds repositions and resizes the web view atomically.
// Prefer this over separate Move + Resize calls whenever both change together.
func (wv *WebView) SetBounds(x, y, width, height int) {
	if !wv.alive() {
		return
	}
	wv.rt.Send(runtime.CmdWebViewSetBounds, runtime.EncodeWebViewSetBounds(wv.id, x, y, width, height))
}

// SetZOrder re-stacks this web view relative to other web views on the same
// window. Only affects ordering among web views; the main window is always
// behind all of them.
func (wv *WebView) SetZOrder(zorder int) {
	if !wv.alive() {
		return
	}
	wv.rt.Send(runtime.CmdWebViewSetZOrder, runtime.EncodeWebViewSetZOrder(wv.id, zorder))
}

// Destroy tears down the native window and releases all associated resources.
// After Destroy returns, the WebView handle is invalid and all further method
// calls are silent no-ops.
func (wv *WebView) Destroy() {
	if !atomic.CompareAndSwapUint32(&wv.destroyed, 0, 1) {
		return // already destroyed
	}
	wv.rt.Send(runtime.CmdWebViewDestroy, runtime.EncodeWebViewID(wv.id))
}