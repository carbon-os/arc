package runtime

import (
	"encoding/binary"
	"fmt"
	"io"
)

// ── Command / event type bytes ────────────────────────────────────────────────

type CmdByte = uint8

const (
	CmdWindowCreate   CmdByte = 0x01
	CmdLoadFile       CmdByte = 0x02
	CmdLoadHTML       CmdByte = 0x03
	CmdLoadURL        CmdByte = 0x04
	CmdEval           CmdByte = 0x05
	CmdSetTitle       CmdByte = 0x06
	CmdSetSize        CmdByte = 0x07
	CmdPostText       CmdByte = 0x08
	CmdPostBinary     CmdByte = 0x09
	CmdQuit           CmdByte = 0x0A
	CmdBillingInit    CmdByte = 0x0B
	CmdBillingBuy     CmdByte = 0x0C
	CmdBillingRestore CmdByte = 0x0D

	// Embedded web view commands
	CmdWebViewCreate    CmdByte = 0x10
	CmdWebViewLoadURL   CmdByte = 0x11
	CmdWebViewLoadFile  CmdByte = 0x12
	CmdWebViewLoadHTML  CmdByte = 0x13
	CmdWebViewShow      CmdByte = 0x14
	CmdWebViewHide      CmdByte = 0x15
	CmdWebViewMove      CmdByte = 0x16
	CmdWebViewResize    CmdByte = 0x17
	CmdWebViewSetBounds CmdByte = 0x18
	CmdWebViewSetZOrder CmdByte = 0x19
	CmdWebViewDestroy   CmdByte = 0x1A
)

type evtByte = uint8

const (
	evtReady           evtByte = 0x81
	evtClosed          evtByte = 0x82
	evtIpcText         evtByte = 0x83
	evtIpcBinary       evtByte = 0x84
	evtBillingProducts evtByte = 0x85
	evtBillingPurchase evtByte = 0x86
)

// ── TitleBarStyle ─────────────────────────────────────────────────────────────

// TitleBarStyle controls the appearance of the native window title bar.
type TitleBarStyle uint8

const (
	// TitleBarDefault shows the standard OS title bar.
	TitleBarDefault TitleBarStyle = 0

	// TitleBarHidden hides the title bar while keeping the window border,
	// shadow, resize handles, and traffic lights (macOS).
	TitleBarHidden TitleBarStyle = 1
)

// ── Billing wire types ────────────────────────────────────────────────────────

// BillingProductDecl is the outbound product declaration sent in CmdBillingInit.
type BillingProductDecl struct {
	ID   string
	Kind uint8 // 0 = Subscription, 1 = OneTime
}

// BillingProduct is a decoded product entry from evtBillingProducts.
type BillingProduct struct {
	ID             string
	Title          string
	Description    string
	FormattedPrice string
	Kind           uint8
}

// BillingPurchaseEvent is the decoded payload from evtBillingPurchase.
type BillingPurchaseEvent struct {
	ProductID string
	Status    uint8
	ErrorMsg  string
}

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
//	width:u32  height:u32  debug:u8  title:str  titleBarStyle:u8
func EncodeWindowCreate(width, height int, debug bool, title string, titleBarStyle TitleBarStyle) []byte {
	var debugByte byte
	if debug {
		debugByte = 1
	}
	payload := EncodeU32U32(uint32(width), uint32(height))
	payload = append(payload, debugByte)
	payload = append(payload, EncodeStr(title)...)
	payload = append(payload, byte(titleBarStyle))
	return payload
}

// EncodeBillingInit builds the CmdBillingInit payload.
//
//	count:u32 [ id:str kind:u8 ] ...
func EncodeBillingInit(products []BillingProductDecl) []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(len(products)))
	for _, p := range products {
		buf = append(buf, EncodeStr(p.ID)...)
		buf = append(buf, p.Kind)
	}
	return buf
}

// ── Web view payload builders ─────────────────────────────────────────────────

// EncodeWebViewCreate builds the WebViewCreate payload.
//
//	id:u32  x:i32  y:i32  width:u32  height:u32  zorder:i32
func EncodeWebViewCreate(id uint32, x, y, width, height, zorder int) []byte {
	b := make([]byte, 24)
	binary.LittleEndian.PutUint32(b[0:], id)
	binary.LittleEndian.PutUint32(b[4:], uint32(int32(x)))
	binary.LittleEndian.PutUint32(b[8:], uint32(int32(y)))
	binary.LittleEndian.PutUint32(b[12:], uint32(width))
	binary.LittleEndian.PutUint32(b[16:], uint32(height))
	binary.LittleEndian.PutUint32(b[20:], uint32(int32(zorder)))
	return b
}

// EncodeWebViewID encodes a bare web view ID — used for Show, Hide, Destroy.
func EncodeWebViewID(id uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, id)
	return b
}

// EncodeWebViewLoad encodes a web view ID followed by a string — used for
// LoadURL, LoadFile, and LoadHTML.
func EncodeWebViewLoad(id uint32, s string) []byte {
	return append(EncodeWebViewID(id), EncodeStr(s)...)
}

// EncodeWebViewMove encodes id + x + y.
func EncodeWebViewMove(id uint32, x, y int) []byte {
	b := make([]byte, 12)
	binary.LittleEndian.PutUint32(b[0:], id)
	binary.LittleEndian.PutUint32(b[4:], uint32(int32(x)))
	binary.LittleEndian.PutUint32(b[8:], uint32(int32(y)))
	return b
}

// EncodeWebViewResize encodes id + width + height.
func EncodeWebViewResize(id uint32, width, height int) []byte {
	b := make([]byte, 12)
	binary.LittleEndian.PutUint32(b[0:], id)
	binary.LittleEndian.PutUint32(b[4:], uint32(width))
	binary.LittleEndian.PutUint32(b[8:], uint32(height))
	return b
}

// EncodeWebViewSetBounds encodes id + x + y + width + height atomically.
func EncodeWebViewSetBounds(id uint32, x, y, width, height int) []byte {
	b := make([]byte, 20)
	binary.LittleEndian.PutUint32(b[0:], id)
	binary.LittleEndian.PutUint32(b[4:], uint32(int32(x)))
	binary.LittleEndian.PutUint32(b[8:], uint32(int32(y)))
	binary.LittleEndian.PutUint32(b[12:], uint32(width))
	binary.LittleEndian.PutUint32(b[16:], uint32(height))
	return b
}

// EncodeWebViewSetZOrder encodes id + zorder.
func EncodeWebViewSetZOrder(id uint32, zorder int) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:], id)
	binary.LittleEndian.PutUint32(b[4:], uint32(int32(zorder)))
	return b
}

// ── Frame I/O ─────────────────────────────────────────────────────────────────

// WriteFrame writes a length-prefixed frame to w:
//
//	[4] uint32_le payload_length
//	[N] payload
//
// Not goroutine-safe on its own — callers must hold the write mutex.
func WriteFrame(w io.Writer, typ CmdByte, payload []byte) error {
	total := 1 + len(payload)
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
	Type            evtByte
	Channel         string
	Text            string
	Data            []byte
	BillingProducts []BillingProduct
	BillingPurchase BillingPurchaseEvent
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
		evt.Data = make([]byte, len(cur))
		copy(evt.Data, cur)

	case evtBillingProducts:
		if len(cur) < 4 {
			return nil, fmt.Errorf("arc: evtBillingProducts truncated reading count")
		}
		count := binary.LittleEndian.Uint32(cur[:4])
		cur = cur[4:]

		evt.BillingProducts = make([]BillingProduct, 0, count)
		for i := uint32(0); i < count; i++ {
			var p BillingProduct
			if p.ID, err = readStr(); err != nil {
				return nil, err
			}
			if p.Title, err = readStr(); err != nil {
				return nil, err
			}
			if p.Description, err = readStr(); err != nil {
				return nil, err
			}
			if p.FormattedPrice, err = readStr(); err != nil {
				return nil, err
			}
			if len(cur) < 1 {
				return nil, fmt.Errorf("arc: evtBillingProducts truncated reading kind")
			}
			p.Kind = cur[0]
			cur = cur[1:]
			evt.BillingProducts = append(evt.BillingProducts, p)
		}

	case evtBillingPurchase:
		if len(cur) < 1 {
			return nil, fmt.Errorf("arc: evtBillingPurchase truncated reading status")
		}
		evt.BillingPurchase.Status = cur[0]
		cur = cur[1:]
		if evt.BillingPurchase.ProductID, err = readStr(); err != nil {
			return nil, err
		}
		if evt.BillingPurchase.ErrorMsg, err = readStr(); err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("arc: unknown event type 0x%02X", evt.Type)
	}

	return evt, nil
}