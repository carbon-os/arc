package arc

// ipcbridge.go — IPCBridge: the Go↔JavaScript message channel for a WebView.

import (
	arcIpc "github.com/carbon-os/arc/ipc"
	"sync"
)

// IPCBridge provides bidirectional messaging between Go and a WebView's
// JavaScript context.
//
// JavaScript calls ipc.post(channel, body) to send messages to Go.
// Go calls Send / SendBytes to push messages back to JavaScript.
//
// The bridge is obtained from Window.IPC() or WebView.IPC(); do not
// construct it directly.
type IPCBridge struct {
	// Exactly one of win or wv is set, depending on whether this bridge
	// belongs to a window's primary WebView or to an overlay WebView.
	win *Window
	wv  *WebView

	mu       sync.RWMutex
	handlers map[string]func(arcIpc.Message) // channel → handler
}

func newBridgeForWindow(win *Window) *IPCBridge {
	return &IPCBridge{win: win, handlers: make(map[string]func(arcIpc.Message))}
}

func newBridgeForOverlay(wv *WebView) *IPCBridge {
	return &IPCBridge{wv: wv, handlers: make(map[string]func(arcIpc.Message))}
}

// targetID returns the webview ID this bridge is bound to.
func (b *IPCBridge) targetID() string {
	if b.wv != nil {
		return b.wv.id
	}
	return b.win.webviewID
}

func (b *IPCBridge) appRef() *App {
	if b.wv != nil {
		return b.wv.app
	}
	return b.win.app
}

// On registers fn to be called whenever JavaScript sends a message on channel.
// Calling On with the same channel name replaces the previous handler.
// Handlers are invoked on the IPC read goroutine; avoid blocking inside them.
func (b *IPCBridge) On(channel string, fn func(arcIpc.Message)) {
	b.mu.Lock()
	b.handlers[channel] = fn
	b.mu.Unlock()
}

// Off removes the handler for channel.
func (b *IPCBridge) Off(channel string) {
	b.mu.Lock()
	delete(b.handlers, channel)
	b.mu.Unlock()
}

// Send pushes a string value to JavaScript on channel.
// JavaScript receives it as the payload in its ipc.on(channel, fn) callback.
func (b *IPCBridge) Send(channel, text string) {
	b.appRef().sendJSON(map[string]any{
		"type":    "webview.send_ipc",
		"id":      b.targetID(),
		"channel": channel,
		"body":    text,
	})
}

// SendBytes pushes binary data to JavaScript on channel, encoded as a JSON
// array of uint8 values.  JavaScript can reconstruct it with
//
//	const buf = new Uint8Array(payload)
func (b *IPCBridge) SendBytes(channel string, data []byte) {
	// JSON-encode as an array of unsigned integers so JS can read it back
	// as new Uint8Array(payload) without needing base64 decode.
	nums := make([]int, len(data))
	for i, v := range data {
		nums[i] = int(v)
	}
	b.appRef().sendJSON(map[string]any{
		"type":    "webview.send_ipc",
		"id":      b.targetID(),
		"channel": channel,
		"body":    nums,
	})
}

// dispatch delivers msg to the registered handler for channel, if any.
// Called internally by the App event loop.
func (b *IPCBridge) dispatch(channel string, msg arcIpc.Message) {
	b.mu.RLock()
	fn := b.handlers[channel]
	b.mu.RUnlock()
	if fn != nil {
		fn(msg)
	}
}