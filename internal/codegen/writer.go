package codegen

import (
	"fmt"
	"io"
)

// errWriter wraps an io.Writer and records the first write error. Emitters
// write freely through it and report the recorded error once at the end,
// instead of checking every Fprintf. After an error is recorded, further
// writes are dropped, so the underlying writer never sees output past the
// failure point.
type errWriter struct {
	w   io.Writer
	err error
}

func newErrWriter(w io.Writer) *errWriter {
	return &errWriter{w: w}
}

func (ew *errWriter) print(s string) {
	if ew.err != nil {
		return
	}
	_, ew.err = io.WriteString(ew.w, s)
}

func (ew *errWriter) println(s string) {
	ew.print(s + "\n")
}

func (ew *errWriter) printf(format string, args ...any) {
	if ew.err != nil {
		return
	}
	_, ew.err = fmt.Fprintf(ew.w, format, args...)
}
