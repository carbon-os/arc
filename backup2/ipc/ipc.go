// Package ipc provides the IPC channel types shared by the arc SDK.
package ipc

import "sync"

// Config lets the caller skip automatic renderer spawning by providing a
// pre-negotiated channel id.  Leave zero-value to let arc spawn arc-host.
type Config struct {
	Channel string
}

// Message is a message delivered from the JS context via arc.post().
type Message struct {
	text   string
	data   []byte
	binary bool
}

// NewTextMessage wraps a text payload.  Called by arc internals.
func NewTextMessage(text string) Message { return Message{text: text} }

// NewBinaryMessage wraps a binary payload.  Called by arc internals.
func NewBinaryMessage(data []byte) Message { return Message{data: data, binary: true} }

func (m Message) Text() string   { return m.text }
func (m Message) Bytes() []byte  { return m.data }
func (m Message) IsBinary() bool { return m.binary }

// Handle is an IPC endpoint scoped to one WebView's JS context.
// All methods are safe to call from any goroutine.
type Handle struct {
	mu       sync.RWMutex
	handlers map[string]func(Message)

	sendText   func(channel, payload string)
	sendBinary func(channel string, data []byte)
}

// NewHandle is called by arc internals when a WebView becomes ready.
func NewHandle(
	sendText func(channel, payload string),
	sendBinary func(channel string, data []byte),
) *Handle {
	return &Handle{
		handlers:   make(map[string]func(Message)),
		sendText:   sendText,
		sendBinary: sendBinary,
	}
}

// On registers a handler for messages arriving on channel from JS.
// Replaces any previous handler for the same channel.
func (h *Handle) On(channel string, fn func(msg Message)) {
	h.mu.Lock()
	h.handlers[channel] = fn
	h.mu.Unlock()
}

// Off removes the handler for channel.
func (h *Handle) Off(channel string) {
	h.mu.Lock()
	delete(h.handlers, channel)
	h.mu.Unlock()
}

// Send sends a text message to the JS side on channel.
func (h *Handle) Send(channel string, text string) { h.sendText(channel, text) }

// SendBytes sends a binary message to the JS side on channel.
func (h *Handle) SendBytes(channel string, data []byte) { h.sendBinary(channel, data) }

// Deliver routes an incoming message to the registered handler.
// Called by arc internals from the reader goroutine — do not block.
func (h *Handle) Deliver(channel string, msg Message) {
	h.mu.RLock()
	fn := h.handlers[channel]
	h.mu.RUnlock()
	if fn != nil {
		fn(msg)
	}
}