package arc

// wire.go — IPC frame encoding and decoding.
//
// The frame format (all fields little-endian) matches the C++ FrameHeader in framing.hpp:
//
//	[4] magic       = 0x41524349 ("ARCI")
//	[1] version     = 1
//	[1] type        (0 = JSON, 1 = Binary)
//	[2] reserved    = 0
//	[4] payload_len
//	[N] payload

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

const (
	frameMagic      uint32 = 0x41524349
	frameVersion    uint8  = 1
	frameHeaderSize        = 12
	maxPayloadBytes uint32 = 64 * 1024 * 1024 // 64 MiB — matches kMaxPayloadSize

	msgTypeJSON   uint8 = 0
	msgTypeBinary uint8 = 1
)

type frame struct {
	msgType uint8
	payload []byte
}

// writeFrame serialises msg and writes it to w under mu.
// mu serialises concurrent writes from different goroutines.
func writeFrame(w io.Writer, mu *sync.Mutex, msgType uint8, payload []byte) error {
	if uint32(len(payload)) > maxPayloadBytes {
		return fmt.Errorf("arc: payload too large: %d bytes", len(payload))
	}

	var hdr [frameHeaderSize]byte
	binary.LittleEndian.PutUint32(hdr[0:4], frameMagic)
	hdr[4] = frameVersion
	hdr[5] = msgType
	// hdr[6:8] reserved — already zero
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(len(payload)))

	mu.Lock()
	defer mu.Unlock()

	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("arc: write frame header: %w", err)
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return fmt.Errorf("arc: write frame payload: %w", err)
		}
	}
	return nil
}

// readFrame reads the next complete frame from r, blocking until available.
// Returns io.EOF (or a wrapped error) when the connection is closed.
func readFrame(r io.Reader) (frame, error) {
	var hdr [frameHeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return frame{}, err
	}

	magic := binary.LittleEndian.Uint32(hdr[0:4])
	version := hdr[4]
	msgType := hdr[5]
	payloadLen := binary.LittleEndian.Uint32(hdr[8:12])

	if magic != frameMagic {
		return frame{}, fmt.Errorf("arc: bad magic 0x%08X (expected 0x%08X)", magic, frameMagic)
	}
	if version != frameVersion {
		return frame{}, fmt.Errorf("arc: unsupported frame version %d", version)
	}
	if payloadLen > maxPayloadBytes {
		return frame{}, fmt.Errorf("arc: payload length %d exceeds 64 MiB cap", payloadLen)
	}

	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return frame{}, fmt.Errorf("arc: read frame payload: %w", err)
		}
	}

	return frame{msgType: msgType, payload: payload}, nil
}