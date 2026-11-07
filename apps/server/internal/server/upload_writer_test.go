package server

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestUploadWriterAvoidsPerFragmentReservations(t *testing.T) {
	const size = 77 << 20
	requests := 0
	var reserved int64
	writer := &uploadWriter{writer: io.Discard, reserve: func(minimum, preferred int64) (int64, error) {
		requests++
		reserved += preferred
		return preferred, nil
	}}
	written, err := io.CopyBuffer(writer, io.LimitReader(zeroReader{}, size), make([]byte, 4096))
	if err != nil || written != size {
		t.Fatalf("written=%d err=%v", written, err)
	}
	if requests > 32 {
		t.Fatalf("small multipart fragments triggered %d reservations", requests)
	}
	if reserved < size || reserved-size >= 4<<20 {
		t.Fatalf("invalid headroom: reserved=%d written=%d", reserved, written)
	}
}

func TestUploadWriterDeniesUnreservedBytesAndKeepsPartialWritesCharged(t *testing.T) {
	var disk bytes.Buffer
	denied := errors.New("quota")
	writer := &uploadWriter{writer: &disk, reserve: func(minimum, preferred int64) (int64, error) {
		if disk.Len() == 0 {
			return minimum, nil
		}
		return 0, denied
	}}
	if n, err := writer.Write([]byte("abc")); n != 3 || err != nil {
		t.Fatalf("initial n=%d err=%v", n, err)
	}
	if n, err := writer.Write([]byte("extra")); n != 0 || !errors.Is(err, denied) {
		t.Fatalf("overflow n=%d err=%v", n, err)
	}
	if disk.String() != "abc" {
		t.Fatal("unreserved bytes written")
	}
	short := &uploadWriter{writer: partialWriter{}, reserve: func(minimum, preferred int64) (int64, error) { return 10, nil }}
	n, err := short.Write([]byte("abc"))
	if n != 1 || err == nil || short.available != 9 {
		t.Fatalf("partial n=%d headroom=%d err=%v", n, short.available, err)
	}
}

type partialWriter struct{}

func (partialWriter) Write([]byte) (int, error) { return 1, io.ErrShortWrite }
