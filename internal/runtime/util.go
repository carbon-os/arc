package runtime

import (
	"fmt"
	"io"
	"math/rand"
	"os"
)

// uniqueID returns a short random hex string suitable for socket/pipe names.
func uniqueID() string {
	return fmt.Sprintf("%08x", rand.Uint32())
}

// prefixWriter wraps an io.Writer (typically os.Stderr) and prepends a
// fixed prefix to every line. Used to label renderer stderr output.
type prefixedWriter struct {
	prefix string
	w      io.Writer
}

func prefixWriter(prefix string) io.Writer {
	return &prefixedWriter{prefix: prefix, w: os.Stderr}
}

func (p *prefixedWriter) Write(b []byte) (int, error) {
	_, _ = fmt.Fprint(p.w, p.prefix)
	return p.w.Write(b)
}