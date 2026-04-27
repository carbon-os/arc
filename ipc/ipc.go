package ipc

import (
	"log"

	"github.com/carbon-os/arc/internal/runtime"
)

// Message is an inbound message from the renderer's JavaScript side.
type Message struct {
	text   string
	data   []byte
	binary bool
}

// IsText reports whether this message carries a UTF-8 string payload.
func (m Message) IsText() bool { return !m.binary }

// IsBinary reports whether this message carries a raw byte payload.
func (m Message) IsBinary() bool { return m.binary }

// Text returns the string payload. Empty if IsBinary is true.
func (m Message) Text() string { return m.text }

// Bytes returns the binary payload. Nil if IsText is true.
func (m Message) Bytes() []byte { return m.data }

// IPC is the messaging handle for a single BrowserWindow.
// Obtain one via win.IPC() — do not construct directly.
type IPC struct {
	rt *runtime.Runtime
}

// New creates an IPC handle backed by the given runtime.
// Called by BrowserWindow.IPC() — do not call directly.
func New(rt *runtime.Runtime) *IPC {
	return &IPC{rt: rt}
}

// On registers a handler for inbound messages on the named channel.
// Replaces any previously registered handler for that channel.
// Handlers are called on a dedicated goroutine.
func (c *IPC) On(channel string, fn func(Message)) {
	log.Printf("[go] ipc.On: registering channel=%q", channel)
	c.rt.OnMessage(channel, func(text string, data []byte, binary bool) {
		log.Printf("[go] ipc: handler fired channel=%q binary=%v", channel, binary)
		if binary {
			fn(Message{data: data, binary: true})
		} else {
			fn(Message{text: text})
		}
	})
}

// Off removes the handler registered for the named channel.
func (c *IPC) Off(channel string) {
	c.rt.OffMessage(channel)
}

// Send delivers a UTF-8 string to the named channel in JavaScript.
// Safe to call from any goroutine.
func (c *IPC) Send(channel, text string) {
	log.Printf("[go] ipc.Send: channel=%q text=%q", channel, text)
	payload := append(runtime.EncodeStr(channel), runtime.EncodeStr(text)...)
	c.rt.Send(runtime.CmdPostText, payload)
}

// SendBytes delivers a binary message to the named channel in JavaScript.
// Safe to call from any goroutine.
func (c *IPC) SendBytes(channel string, data []byte) {
	log.Printf("[go] ipc.SendBytes: channel=%q bytes=%d", channel, len(data))
	payload := append(runtime.EncodeStr(channel), data...)
	c.rt.Send(runtime.CmdPostBinary, payload)
}