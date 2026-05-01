//go:build windows

package arc

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// dialHost connects to arc-host via a Windows named pipe.
// arc-host creates the pipe at \\.\pipe\arc-ipc-<channelID>.
func dialHost(channelID string) (net.Conn, error) {
	timeout := 5 * time.Second
	return winio.DialPipe(`\\.\pipe\arc-ipc-`+channelID, &timeout)
}