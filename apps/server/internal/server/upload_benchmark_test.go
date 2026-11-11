package server

import (
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

// Exercises real loopback HTTP, multipart parsing, SQLite reservations, and
// file sync. No worker runs: this measures reception, excluding transcoding.
func BenchmarkUpload77MiB(b *testing.B) {
	root := b.TempDir()
	s, err := store.Open(testContext(), filepath.Join(root, "benchmark.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
	api := &API{ResolveUser: testUserResolver, Store: s, Media: media.New(root)}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	const size = 77 << 20
	b.SetBytes(size)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader, writer := io.Pipe()
		form := multipart.NewWriter(writer)
		contentType := form.FormDataContentType()
		go func() {
			part, err := form.CreateFormFile("file", "benchmark.mp3")
			if err == nil {
				_, err = io.CopyN(part, zeroReader{}, size)
			}
			if err == nil {
				err = form.Close()
			}
			_ = writer.CloseWithError(err)
		}()
		response, err := http.Post(server.URL+"/api/tracks", contentType, reader)
		if err != nil {
			_ = reader.Close()
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		_ = reader.Close()
		if response.StatusCode != http.StatusAccepted {
			b.Fatalf("status=%d", response.StatusCode)
		}
	}
	b.StopTimer()
}

type zeroReader struct{}

func (zeroReader) Read(data []byte) (int, error) { clear(data); return len(data), nil }
