package runtime

import (
	"io"
	"log"
	"os"
)

// NewLogger returns a logger writing to stderr when enabled,
// or silently discarding all output when disabled.
func NewLogger(enabled bool) *log.Logger {
	if enabled {
		return log.New(os.Stderr, "", log.LstdFlags)
	}
	return log.New(io.Discard, "", 0)
}