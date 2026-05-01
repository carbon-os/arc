//go:build !windows

package arc

import "net"

func dialRenderer(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}