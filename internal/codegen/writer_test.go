package codegen

import (
	"bytes"
	"errors"
	"testing"
)

// failAfterWriter succeeds for a fixed number of writes, then fails. It keeps
// the buffer in a named field on purpose: embedding bytes.Buffer would promote
// its WriteString method, and io.WriteString would use that instead of Write.
type failAfterWriter struct {
	buf       bytes.Buffer
	remaining int
	err       error
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if w.remaining == 0 {
		return 0, w.err
	}
	w.remaining--
	return w.buf.Write(p)
}

func TestErrWriterStopsAtFirstFailure(t *testing.T) {
	writeErr := errors.New("disk full")
	under := &failAfterWriter{remaining: 2, err: writeErr}
	ew := newErrWriter(under)

	ew.println("one")
	ew.printf("%s\n", "two")
	ew.print("three\n") // fails
	ew.println("four")  // must be dropped
	ew.printf("%s", "five")

	if !errors.Is(ew.err, writeErr) {
		t.Fatalf("err = %v, want %v", ew.err, writeErr)
	}
	if got, want := under.buf.String(), "one\ntwo\n"; got != want {
		t.Fatalf("underlying writer received %q, want %q (nothing past the failure)", got, want)
	}
}

func TestErrWriterSuccessPassesEverythingThrough(t *testing.T) {
	var buf bytes.Buffer
	ew := newErrWriter(&buf)
	ew.print("a")
	ew.println("b")
	ew.printf("%d-%s", 1, "c")
	if ew.err != nil {
		t.Fatalf("err = %v, want nil", ew.err)
	}
	if got, want := buf.String(), "ab\n1-c"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
