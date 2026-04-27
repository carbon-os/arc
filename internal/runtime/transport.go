package runtime

import (
	"fmt"
	"net"
	"os"
	"runtime"
)

// Transport is a connected net.Conn to the renderer process.
// On Linux/macOS it is a Unix socket; on Windows a named pipe.
type Transport struct {
	ID       string // the opaque random ID passed to the renderer via --channel
	Path     string // the full platform-specific socket/pipe path
	conn     net.Conn
	listener net.Listener
}

// ListenTransport creates the server-side socket/pipe and returns a Transport
// whose Accept method blocks until the renderer connects.
func ListenTransport(id string) (*Transport, error) {
	path := channelPath(id)

	if runtime.GOOS != "windows" {
		_ = os.Remove(path)
	}

	ln, err := listenPlatform(path)
	if err != nil {
		return nil, fmt.Errorf("arc: transport listen %s: %w", path, err)
	}

	return &Transport{ID: id, Path: path, listener: ln}, nil
}

// Accept blocks until the renderer connects.
// The listener is intentionally left open until Close is called — on Windows,
// go-winio shares its IOCP completion port between the listener and any
// accepted connections, so closing the listener early silently breaks reads
// on the accepted connection.
func (t *Transport) Accept() error {
	conn, err := t.listener.Accept()
	if err != nil {
		return fmt.Errorf("arc: transport accept: %w", err)
	}
	t.conn = conn
	return nil
}

// Conn returns the active connection. Nil until Accept returns.
func (t *Transport) Conn() net.Conn { return t.conn }

// Close tears down the transport.
func (t *Transport) Close() error {
	if t.conn != nil {
		t.conn.Close()
	}
	if t.listener != nil {
		t.listener.Close()
	}
	if runtime.GOOS != "windows" {
		os.Remove(t.Path)
	}
	return nil
}