package runtime

import (
	"encoding/binary"
	"fmt"
	"io"
)

// ── Command / event type bytes ────────────────────────────────────────────────

type CmdByte = uint8

const (
	CmdWindowCreate CmdByte = 0x01
	CmdLoadFile     CmdByte = 0x02
	CmdLoadHTML     CmdByte = 0x03
	CmdLoadURL      CmdByte = 0x04
	CmdEval         CmdByte = 0x05
	CmdSetTitle     CmdByte = 0x06
	CmdSetSize      CmdByte = 0x07
	CmdPostText     CmdByte = 0x08
	CmdPostBinary   CmdByte = 0x09
	CmdQuit         CmdByte = 0x0A
)

type evtByte = uint8

const (
	evtReady     evtByte = 0x81
	evtClosed    evtByte = 0x82
	evtIpcText   evtByte = 0x83
	evtIpcBinary evtByte = 0x84
)

// ── Payload builders ──────────────────────────────────────────────────────────

// EncodeStr returns a length-prefixed UTF-8 string: [uint32_le len][bytes].
func EncodeStr(s string) []byte {
	b := make([]byte, 4+len(s))
	binary.LittleEndian.PutUint32(b[:4], uint32(len(s)))
	copy(b[4:], s)
	return b
}

// EncodeU32U32 returns two consecutive little-endian uint32 values.
func EncodeU32U32(a, b uint32) []byte {
	out := make([]byte, 8)
	binary.LittleEndian.PutUint32(out[:4], a)
	binary.LittleEndian.PutUint32(out[4:], b)
	return out
}

// EncodeWindowCreate builds the WindowCreate payload.
//
//	width:u32 height:u32 debug:u8 title:str
func EncodeWindowCreate(width, height int, debug bool, title string) []byte {
	var debugByte byte
	if debug {
		debugByte = 1
	}
	payload := EncodeU32U32(uint32(width), uint32(height))
	payload = append(payload, debugByte)
	payload = append(payload, EncodeStr(title)...)
	return payload
}

// ── Frame I/O ─────────────────────────────────────────────────────────────────

// WriteFrame writes a length-prefixed frame to w:
//
//	[4] uint32_le payload_length
//	[N] payload
//
// Not goroutine-safe on its own — callers must hold the write mutex.
func WriteFrame(w io.Writer, typ CmdByte, payload []byte) error {
	total := 1 + len(payload) // type byte + payload
	hdr := make([]byte, 4)
	binary.LittleEndian.PutUint32(hdr, uint32(total))

	frame := make([]byte, 0, 4+total)
	frame = append(frame, hdr...)
	frame = append(frame, typ)
	frame = append(frame, payload...)

	_, err := w.Write(frame)
	return err
}

// ── Inbound event ─────────────────────────────────────────────────────────────

// Event is a decoded inbound frame from the renderer.
type Event struct {
	Type    evtByte
	Channel string
	Text    string
	Data    []byte
}

// ReadEvent reads and decodes one inbound frame from r.
// Blocks until a full frame is available or an error occurs.
func ReadEvent(r io.Reader) (*Event, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	length := binary.LittleEndian.Uint32(hdr[:])
	if length == 0 {
		return nil, fmt.Errorf("arc: received zero-length frame")
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	evt := &Event{Type: payload[0]}
	cur := payload[1:]

	readStr := func() (string, error) {
		if len(cur) < 4 {
			return "", fmt.Errorf("arc: frame truncated reading string length")
		}
		n := binary.LittleEndian.Uint32(cur[:4])
		cur = cur[4:]
		if uint32(len(cur)) < n {
			return "", fmt.Errorf("arc: frame truncated reading string data (%d bytes, want %d)", len(cur), n)
		}
		s := string(cur[:n])
		cur = cur[n:]
		return s, nil
	}

	var err error
	switch evt.Type {
	case evtReady, evtClosed:
		// no payload

	case evtIpcText:
		if evt.Channel, err = readStr(); err != nil {
			return nil, err
		}
		if evt.Text, err = readStr(); err != nil {
			return nil, err
		}

	case evtIpcBinary:
		if evt.Channel, err = readStr(); err != nil {
			return nil, err
		}
		// Remaining bytes are the binary payload.
		evt.Data = make([]byte, len(cur))
		copy(evt.Data, cur)

	default:
		return nil, fmt.Errorf("arc: unknown event type 0x%02X", evt.Type)
	}

	return evt, nil
}