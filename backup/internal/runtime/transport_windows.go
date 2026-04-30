//go:build windows

package runtime

import (
	"net"

	"github.com/Microsoft/go-winio"
)

func channelPath(id string) string {
	return `\\.\pipe\arc-` + id
}

func listenPlatform(path string) (net.Listener, error) {
	return winio.ListenPipe(path, &winio.PipeConfig{
		InputBufferSize:  65536,
		OutputBufferSize: 65536,
	})
}

func dialPlatform(path string) (net.Conn, error) {
	return winio.DialPipe(path, nil)
}