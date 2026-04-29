//go:build !windows

package runtime

import (
	"fmt"
	"net"
	"os"
)

func channelPath(id string) string {
	return fmt.Sprintf("%s/arc-%s.sock", os.TempDir(), id)
}

func listenPlatform(path string) (net.Listener, error) {
	return net.Listen("unix", path)
}

func dialPlatform(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}