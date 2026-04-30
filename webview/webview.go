// Package webview provides WebView creation config and the WebView handle.
package webview

import (
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/carbon-os/arc/ipc"
)

// Config holds WebView creation parameters.
type Config struct {
	// "root"    — fills the window content area and resizes with it.
	// "overlay" — absolutely positioned; X/Y/Width/Height are required.
	// Defaults to "overlay".
	Layout string

	X, Y          int
	Width, Height int

	// Z-order within the window.  Higher = on top.  Default 0.
	ZOrder int
}

func (c Config) layout() string {
	if c.Layout == "" {
		return "overlay"
	}
	return c.Layout
}

// NewWindowPolicy is the return value of an OnNewWindowRequested handler.
type NewWindowPolicy struct {
	Action      string // "allow" | "deny" | "redirect"
	RedirectURL string // used when Action == "redirect"
}

// DownloadPolicy is the return value of an OnDownloadRequested handler.
type DownloadPolicy struct {
	Action   string // "allow" | "deny"
	SavePath string // required when Action == "allow"
}

type (
	sendFn    = func(cmd any)
	sendBinFn = func(jsonCmd any, data []byte)
)

// WebView is a handle to a renderer WebView.
type WebView struct {
	mu sync.Mutex
	id string

	send    sendFn
	sendBin sendBinFn

	ipcOnce   sync.Once
	ipcHandle *ipc.Handle

	// lifecycle
	onReady   func()
	onDestroy func()

	// navigation events
	onLoadStart  func(url string)
	onLoadFinish func(url string)
	onLoadError  func(url string, code int, description string)
	onURLChanged func(url string)
	onNewWindow  func(url string) NewWindowPolicy

	// permission events
	onPermission  func(permType string) bool
	onGeolocate   func(origin string) bool
	onAuth        func(url string) (user, pass string)
	onCertError   func(url string) bool
	onDownload    func(url string) DownloadPolicy

	// pending Eval callbacks keyed by req_id
	evalMu   sync.Mutex
	evalPend map[string]func(result string)
	evalSeq  atomic.Uint64

	// pending GetURL callbacks (FIFO — no req_id in protocol)
	urlMu  sync.Mutex
	urlCbs []func(url string)

	// pending GetZoom callbacks (FIFO — no req_id in protocol)
	zoomMu  sync.Mutex
	zoomCbs []func(factor float64)
}

// New creates a WebView.  Called by arc internals.
func New(cfg Config, send sendFn, sendBin sendBinFn) *WebView {
	return &WebView{
		send:     send,
		sendBin:  sendBin,
		evalPend: make(map[string]func(string)),
	}
}

// SetID is called by arc internals once the renderer has assigned a view id.
// It triggers the OnReady callback.
func (v *WebView) SetID(id string) {
	v.mu.Lock()
	v.id = id
	fn := v.onReady
	v.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (v *WebView) getID() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.id
}

// IPC returns the IPC handle scoped to this WebView's JS context.
// This is the only source of IPC — windows have no JS context of their own.
func (v *WebView) IPC() *ipc.Handle {
	v.ipcOnce.Do(func() {
		v.ipcHandle = ipc.NewHandle(
			func(channel, payload string) {
				v.send(map[string]any{
					"cmd":     "webview_post_text",
					"view_id": v.getID(),
					"channel": channel,
					"payload": payload,
				})
			},
			func(channel string, data []byte) {
				v.sendBin(map[string]any{
					"cmd":     "webview_post_binary",
					"view_id": v.getID(),
					"channel": channel,
				}, data)
			},
		)
	})
	return v.ipcHandle
}

// ── Lifecycle events ──────────────────────────────────────────────────────────

func (v *WebView) OnReady(fn func())   { v.mu.Lock(); v.onReady = fn; v.mu.Unlock() }
func (v *WebView) OnDestroy(fn func()) { v.mu.Lock(); v.onDestroy = fn; v.mu.Unlock() }

// ── Navigation events ─────────────────────────────────────────────────────────

func (v *WebView) OnLoadStart(fn func(url string)) {
	v.mu.Lock(); v.onLoadStart = fn; v.mu.Unlock()
}
func (v *WebView) OnLoadFinish(fn func(url string)) {
	v.mu.Lock(); v.onLoadFinish = fn; v.mu.Unlock()
}
func (v *WebView) OnLoadError(fn func(url string, code int, description string)) {
	v.mu.Lock(); v.onLoadError = fn; v.mu.Unlock()
}
func (v *WebView) OnURLChanged(fn func(url string)) {
	v.mu.Lock(); v.onURLChanged = fn; v.mu.Unlock()
}
func (v *WebView) OnNewWindowRequested(fn func(url string) NewWindowPolicy) {
	v.mu.Lock(); v.onNewWindow = fn; v.mu.Unlock()
}

// ── Permission events ─────────────────────────────────────────────────────────

func (v *WebView) OnPermissionRequested(fn func(permType string) bool) {
	v.mu.Lock(); v.onPermission = fn; v.mu.Unlock()
}
func (v *WebView) OnGeolocationRequested(fn func(origin string) bool) {
	v.mu.Lock(); v.onGeolocate = fn; v.mu.Unlock()
}
func (v *WebView) OnAuthRequired(fn func(url string) (user, pass string)) {
	v.mu.Lock(); v.onAuth = fn; v.mu.Unlock()
}
func (v *WebView) OnCertificateError(fn func(url string) bool) {
	v.mu.Lock(); v.onCertError = fn; v.mu.Unlock()
}
func (v *WebView) OnDownloadRequested(fn func(url string) DownloadPolicy) {
	v.mu.Lock(); v.onDownload = fn; v.mu.Unlock()
}

// ── Geometry ──────────────────────────────────────────────────────────────────

func (v *WebView) Show() {
	v.send(map[string]any{"cmd": "view_show", "view_id": v.getID()})
}
func (v *WebView) Hide() {
	v.send(map[string]any{"cmd": "view_hide", "view_id": v.getID()})
}
func (v *WebView) Move(x, y int) {
	v.send(map[string]any{"cmd": "view_move", "view_id": v.getID(), "x": x, "y": y})
}
func (v *WebView) Resize(width, height int) {
	v.send(map[string]any{"cmd": "view_resize", "view_id": v.getID(), "width": width, "height": height})
}
func (v *WebView) SetBounds(x, y, width, height int) {
	v.send(map[string]any{
		"cmd": "view_set_bounds", "view_id": v.getID(),
		"x": x, "y": y, "width": width, "height": height,
	})
}
func (v *WebView) SetZOrder(z int) {
	v.send(map[string]any{"cmd": "view_set_z", "view_id": v.getID(), "z": z})
}
func (v *WebView) Destroy() {
	v.send(map[string]any{"cmd": "view_destroy", "view_id": v.getID()})
}

// ── Navigation ────────────────────────────────────────────────────────────────

func (v *WebView) LoadURL(url string) {
	v.send(map[string]any{"cmd": "webview_load_url", "view_id": v.getID(), "url": url})
}
func (v *WebView) LoadHTML(html string) {
	v.send(map[string]any{"cmd": "webview_load_html", "view_id": v.getID(), "html": html})
}
func (v *WebView) LoadFile(path string) {
	v.send(map[string]any{"cmd": "webview_load_file", "view_id": v.getID(), "path": path})
}
func (v *WebView) GoBack() {
	v.send(map[string]any{"cmd": "webview_go_back", "view_id": v.getID()})
}
func (v *WebView) GoForward() {
	v.send(map[string]any{"cmd": "webview_go_forward", "view_id": v.getID()})
}
func (v *WebView) Reload() {
	v.send(map[string]any{"cmd": "webview_reload", "view_id": v.getID()})
}
func (v *WebView) Stop() {
	v.send(map[string]any{"cmd": "webview_stop", "view_id": v.getID()})
}

// GetURL asynchronously retrieves the current URL; fn is called with the result.
func (v *WebView) GetURL(fn func(url string)) {
	v.urlMu.Lock()
	v.urlCbs = append(v.urlCbs, fn)
	v.urlMu.Unlock()
	v.send(map[string]any{"cmd": "webview_get_url", "view_id": v.getID()})
}

// ── JavaScript ────────────────────────────────────────────────────────────────

// Eval executes a JS expression.  If fn is non-nil the result string is passed
// to it asynchronously (called from the internal reader goroutine — do not block).
func (v *WebView) Eval(js string, fn func(result string)) {
	var reqID string
	if fn != nil {
		seq := v.evalSeq.Add(1)
		reqID = strconv.FormatUint(seq, 10)
		v.evalMu.Lock()
		v.evalPend[reqID] = fn
		v.evalMu.Unlock()
	}
	v.send(map[string]any{
		"cmd": "webview_eval", "view_id": v.getID(),
		"js": js, "req_id": reqID,
	})
}

// ── Zoom ──────────────────────────────────────────────────────────────────────

func (v *WebView) SetZoom(factor float64) {
	v.send(map[string]any{"cmd": "webview_set_zoom", "view_id": v.getID(), "factor": factor})
}

// GetZoom asynchronously retrieves the zoom factor; fn is called with the result.
func (v *WebView) GetZoom(fn func(factor float64)) {
	v.zoomMu.Lock()
	v.zoomCbs = append(v.zoomCbs, fn)
	v.zoomMu.Unlock()
	v.send(map[string]any{"cmd": "webview_get_zoom", "view_id": v.getID()})
}

// ── Find in page ──────────────────────────────────────────────────────────────

func (v *WebView) Find(query string, caseSensitive bool, wrap bool) {
	v.send(map[string]any{
		"cmd": "webview_find", "view_id": v.getID(),
		"query": query, "case_sensitive": caseSensitive, "wrap": wrap,
	})
}
func (v *WebView) FindNext() {
	v.send(map[string]any{"cmd": "webview_find_next", "view_id": v.getID()})
}
func (v *WebView) FindPrev() {
	v.send(map[string]any{"cmd": "webview_find_prev", "view_id": v.getID()})
}
func (v *WebView) FindStop() {
	v.send(map[string]any{"cmd": "webview_find_stop", "view_id": v.getID()})
}

// ── DispatchEvent — called by arc internals ───────────────────────────────────

// DispatchEvent routes an inbound event to the appropriate handler.
// binary is non-nil only for "ipc_binary" events (the following frame).
// Called from the reader goroutine — handlers must not block.
func (v *WebView) DispatchEvent(event string, j map[string]any, binary []byte) {
	// snapshot callbacks under lock to avoid holding it during calls
	v.mu.Lock()
	snap := struct {
		onLoadStart  func(string)
		onLoadFinish func(string)
		onLoadError  func(string, int, string)
		onURLChanged func(string)
		onNewWindow  func(string) NewWindowPolicy
		onPermission  func(string) bool
		onGeolocate   func(string) bool
		onAuth        func(string) (string, string)
		onCertError   func(string) bool
		onDownload    func(string) DownloadPolicy
		onDestroy    func()
	}{
		v.onLoadStart, v.onLoadFinish, v.onLoadError, v.onURLChanged, v.onNewWindow,
		v.onPermission, v.onGeolocate, v.onAuth, v.onCertError, v.onDownload, v.onDestroy,
	}
	v.mu.Unlock()

	vid := v.getID()

	switch event {
	case "load_start":
		if snap.onLoadStart != nil {
			snap.onLoadStart(strField(j, "url"))
		}

	case "load_finish":
		if snap.onLoadFinish != nil {
			snap.onLoadFinish(strField(j, "url"))
		}

	case "load_error":
		if snap.onLoadError != nil {
			code := 0
			if f, ok := j["code"].(float64); ok {
				code = int(f)
			}
			snap.onLoadError(strField(j, "url"), code, strField(j, "description"))
		}

	case "url_changed":
		if snap.onURLChanged != nil {
			snap.onURLChanged(strField(j, "url"))
		}

	case "new_window_requested":
		url := strField(j, "url")
		policy := NewWindowPolicy{Action: "deny"}
		if snap.onNewWindow != nil {
			policy = snap.onNewWindow(url)
		}
		cmd := map[string]any{
			"cmd": "webview_new_window_policy", "view_id": vid,
			"url": url, "policy": policy.Action,
		}
		if policy.RedirectURL != "" {
			cmd["redirect_url"] = policy.RedirectURL
		}
		v.send(cmd)

	case "permission_requested":
		permType := strField(j, "type")
		granted := false
		if snap.onPermission != nil {
			granted = snap.onPermission(permType)
		}
		v.send(map[string]any{
			"cmd": "webview_permission_response", "view_id": vid,
			"type": permType, "granted": granted,
		})

	case "geolocation_requested":
		origin := strField(j, "origin")
		granted := false
		if snap.onGeolocate != nil {
			granted = snap.onGeolocate(origin)
		}
		v.send(map[string]any{
			"cmd": "webview_geolocation_response", "view_id": vid,
			"origin": origin, "granted": granted,
		})

	case "auth_required":
		url := strField(j, "url")
		user, pass := "", ""
		if snap.onAuth != nil {
			user, pass = snap.onAuth(url)
		}
		v.send(map[string]any{
			"cmd": "webview_auth_response", "view_id": vid,
			"username": user, "password": pass,
		})

	case "certificate_error":
		url := strField(j, "url")
		proceed := false
		if snap.onCertError != nil {
			proceed = snap.onCertError(url)
		}
		v.send(map[string]any{
			"cmd": "webview_certificate_response", "view_id": vid,
			"proceed": proceed,
		})

	case "download_requested":
		url := strField(j, "url")
		dp := DownloadPolicy{Action: "deny"}
		if snap.onDownload != nil {
			dp = snap.onDownload(url)
		}
		cmd := map[string]any{
			"cmd": "webview_download_policy", "view_id": vid,
			"url": url, "policy": dp.Action,
		}
		if dp.SavePath != "" {
			cmd["save_path"] = dp.SavePath
		}
		v.send(cmd)

	case "ipc_text":
		if v.ipcHandle != nil {
			v.ipcHandle.Deliver(strField(j, "channel"),
				ipc.NewTextMessage(strField(j, "payload")))
		}

	case "ipc_binary":
		if v.ipcHandle != nil {
			v.ipcHandle.Deliver(strField(j, "channel"), ipc.NewBinaryMessage(binary))
		}

	case "eval_result":
		reqID := strField(j, "req_id")
		v.evalMu.Lock()
		fn := v.evalPend[reqID]
		delete(v.evalPend, reqID)
		v.evalMu.Unlock()
		if fn != nil {
			fn(strField(j, "result"))
		}

	case "webview_url":
		v.urlMu.Lock()
		var fn func(string)
		if len(v.urlCbs) > 0 {
			fn, v.urlCbs = v.urlCbs[0], v.urlCbs[1:]
		}
		v.urlMu.Unlock()
		if fn != nil {
			fn(strField(j, "url"))
		}

	case "webview_zoom":
		v.zoomMu.Lock()
		var fn func(float64)
		if len(v.zoomCbs) > 0 {
			fn, v.zoomCbs = v.zoomCbs[0], v.zoomCbs[1:]
		}
		v.zoomMu.Unlock()
		if fn != nil {
			factor := 1.0
			if f, ok := j["factor"].(float64); ok {
				factor = f
			}
			fn(factor)
		}

	case "view_destroy":
		if snap.onDestroy != nil {
			snap.onDestroy()
		}
	}
}

func strField(j map[string]any, key string) string {
	if s, ok := j[key].(string); ok {
		return s
	}
	return ""
}