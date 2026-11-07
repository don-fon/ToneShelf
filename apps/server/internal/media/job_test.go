package media

import (
	"bytes"
	"errors"
	"testing"
)

func TestOutputWriterNeverWritesUnreservedBytes(t *testing.T) {
	var file bytes.Buffer
	denied := errors.New("quota")
	writer := &reservedWriter{writer: &file, available: 3, reserve: func(int64) error { return denied }}
	if n, err := writer.Write([]byte("abc")); n != 3 || err != nil {
		t.Fatalf("write n=%d err=%v", n, err)
	}
	if n, err := writer.Write([]byte("d")); n != 0 || !errors.Is(err, denied) {
		t.Fatalf("quota n=%d err=%v", n, err)
	}
	if file.String() != "abc" {
		t.Fatalf("unreserved bytes reached file: %q", file.String())
	}
}
