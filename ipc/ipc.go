// Package ipc contains the types shared between the Arc package and user code
// for the Go↔WebView IPC channel.
package ipc

import "encoding/json"

// Config holds connection parameters when the caller manages the arc-host
// process externally and Go should connect to an already-running instance.
type Config struct {
	// Channel is the channel ID to connect to.
	// If empty, arc.App will generate one automatically when it spawns arc-host.
	Channel string
}

// Message is an inbound IPC message received from a WebView's JavaScript context.
// It is delivered to handlers registered with IPCBridge.On.
type Message struct {
	raw json.RawMessage
}

// NewMessage wraps a raw JSON value as a Message.
// This is called internally by arc; user code receives ready-made Messages.
func NewMessage(raw json.RawMessage) Message { return Message{raw: raw} }

// Text returns the message body as a plain string.
// If the underlying JSON value is a quoted string it is unquoted;
// otherwise the raw JSON bytes are returned as a string.
func (m Message) Text() string {
	var s string
	if err := json.Unmarshal(m.raw, &s); err == nil {
		return s
	}
	return string(m.raw)
}

// Bytes returns the message body as a byte slice.
// If the body is a JSON array of numbers (how Go sends binary via SendBytes)
// it is decoded into []byte directly; otherwise the text representation is
// returned as bytes.
func (m Message) Bytes() []byte {
	var nums []uint8
	if err := json.Unmarshal(m.raw, &nums); err == nil {
		return nums
	}
	return []byte(m.Text())
}

// Raw returns the underlying JSON-encoded body for custom unmarshalling.
func (m Message) Raw() json.RawMessage { return m.raw }