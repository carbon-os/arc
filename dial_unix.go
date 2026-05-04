//go:build !windows

package arc

import "net"

// dialHost connects to arc-host via a Unix domain socket.
// arc-host binds the socket at /tmp/arc-ipc-<channelID> before accepting.
func dialHost(channelID string) (net.Conn, error) {
	return net.Dial("unix", "/tmp/arc-ipc-"+channelID)
}