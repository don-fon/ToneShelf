package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

func TestPlaylistsEndpointUsesEmptyArrays(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(context.Background(), filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	api := &API{Store: s, Media: media.New(root), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/playlists", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var playlists []struct {
		Tracks json.RawMessage `json:"tracks"`
	}
	if err = json.Unmarshal(res.Body.Bytes(), &playlists); err != nil {
		t.Fatal(err)
	}
	if len(playlists) != 1 || string(playlists[0].Tracks) != "[]" {
		t.Fatalf("empty tracks must be encoded as [], body=%s", res.Body.String())
	}
}

func TestAudioEndpointSupportsRange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.mp3")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(context.Background(), filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.InsertTrack(context.Background(), store.NewTrack{ID: "track-1", Title: "test", OriginalPath: path, PlayablePath: path, ContentType: "audio/mpeg", SizeBytes: 10, DurationSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s, Media: media.New(root), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	req := httptest.NewRequest(http.MethodGet, "/api/tracks/track-1/audio", nil)
	req.Header.Set("Range", "bytes=2-5")
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, body=%s", res.Code, res.Body.String())
	}
	if strings.TrimSpace(res.Body.String()) != "2345" {
		t.Fatalf("body = %q", res.Body.String())
	}
	if res.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("content range = %q", res.Header().Get("Content-Range"))
	}
}

func TestTrackUpdateRejectsInvalidRange(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(context.Background(), filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.InsertTrack(context.Background(), store.NewTrack{ID: "track-1", Title: "test", OriginalPath: "a", PlayablePath: "b", ContentType: "audio/mpeg", SizeBytes: 1, DurationSeconds: 5})
	if err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s, Media: media.New(root), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	req := httptest.NewRequest(http.MethodPatch, "/api/tracks/track-1", strings.NewReader(`{"baseRandomWeight":1,"playbackStartSeconds":4,"playbackEndSeconds":3,"volumeRatio":0.8,"lyrics":""}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestWebClientAndSPAFallback(t *testing.T) {
	root := t.TempDir()
	web := filepath.Join(root, "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<h1>music</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(context.Background(), filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	api := &API{Store: s, Media: media.New(root), Log: slog.New(slog.NewTextHandler(io.Discard, nil)), WebDir: web}
	for _, path := range []string{"/", "/playlist/example"} {
		res := httptest.NewRecorder()
		api.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "music") {
			t.Fatalf("path=%s status=%d body=%s", path, res.Code, res.Body.String())
		}
	}
}
