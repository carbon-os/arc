//go:build windows

package arc

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

// dialRenderer connects to a Windows named pipe, retrying while the pipe is busy.
func dialRenderer(path string) (net.Conn, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("arc: invalid pipe path: %w", err)
	}
	const fileFlagOverlapped = 0x40000000
	const errorPipeBusy = syscall.Errno(231)

	for {
		h, err := syscall.CreateFile(
			p,
			syscall.GENERIC_READ|syscall.GENERIC_WRITE,
			0, nil,
			syscall.OPEN_EXISTING,
			fileFlagOverlapped,
			0,
		)
		if err == nil {
			f := os.NewFile(uintptr(h), path)
			conn, err := net.FileConn(f)
			f.Close() // net.FileConn dups the handle
			return conn, err
		}
		if err == errorPipeBusy {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
}