package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"music-player/server/internal/lyrics"
	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

func TestLyricsSearchEndpointReturnsCandidatesWithoutSaving(t *testing.T) {
	var gotTitle, gotArtist string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle, gotArtist = r.URL.Query().Get("track_name"), r.URL.Query().Get("artist_name")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":29152005,"trackName":"ワールドイズマイン [World Is Mine]","artistName":"ryo","albumName":"supercell","duration":253,"plainLyrics":"plain","syncedLyrics":"[00:01.00] synced"}]`))
	}))
	defer provider.Close()

	root := t.TempDir()
	s, err := store.Open(context.Background(), filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	track, err := s.InsertTrack(context.Background(), store.NewTrack{ID: "track-1", Title: "ryo _ 初音未来 - ワールドイズマイン (World is Mine)_H", OriginalPath: "a", PlayablePath: "b", ContentType: "audio/mpeg", SizeBytes: 1, DurationSeconds: 255.240726})
	if err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s, Media: media.New(root), Lyrics: lyrics.New(provider.URL), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/tracks/track-1/lyrics/search", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if gotTitle != "ワールドイズマイン" || gotArtist != "ryo" {
		t.Fatalf("provider query title=%q artist=%q", gotTitle, gotArtist)
	}
	var result lyrics.SearchResult
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].ID != 29152005 || !result.Candidates[0].Synced {
		t.Fatalf("unexpected result: %#v", result)
	}
	stored, err := s.Track(context.Background(), track.ID)
	if err != nil || stored.Lyrics != "" {
		t.Fatalf("search must not save lyrics: %#v, %v", stored, err)
	}
}

func TestUploadTranscodePlaylistAndRangeFlow(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not available")
	}
	tests := []struct {
		name          string
		filename      string
		encodeArgs    []string
		wantTranscode bool
	}{
		{name: "mp3 direct", filename: "sample.mp3", encodeArgs: []string{"-c:a", "libmp3lame", "-q:a", "4"}},
		{name: "wav", filename: "sample.wav", encodeArgs: []string{"-c:a", "pcm_s16le"}, wantTranscode: true},
		{name: "aac adts", filename: "sample.aac", encodeArgs: []string{"-c:a", "aac", "-b:a", "128k", "-f", "adts"}, wantTranscode: true},
		{name: "aac in m4a", filename: "sample.m4a", encodeArgs: []string{"-c:a", "aac", "-b:a", "128k"}, wantTranscode: true},
		{name: "aac with misleading extension", filename: "renamed.mp3", encodeArgs: []string{"-c:a", "aac", "-b:a", "128k", "-f", "adts"}, wantTranscode: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testUploadFlow(t, test.filename, test.encodeArgs, test.wantTranscode)
		})
	}
}

func testUploadFlow(t *testing.T, filename string, encodeArgs []string, wantTranscode bool) {
	t.Helper()
	root := t.TempDir()
	sourcePath := filepath.Join(root, filename)
	args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.2"}
	args = append(args, encodeArgs...)
	args = append(args, sourcePath)
	cmd := exec.Command("ffmpeg", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate %s: %v: %s", filename, err, output)
	}

	s, err := store.Open(context.Background(), filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	api := &API{Store: s, Media: media.New(root), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := api.Handler()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	filePart, err := form.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(filePart, source); err != nil {
		source.Close()
		t.Fatal(err)
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}

	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/tracks", &body)
	uploadRequest.Header.Set("Content-Type", form.FormDataContentType())
	uploadResponse := httptest.NewRecorder()
	handler.ServeHTTP(uploadResponse, uploadRequest)
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var track store.Track
	if err = json.Unmarshal(uploadResponse.Body.Bytes(), &track); err != nil {
		t.Fatal(err)
	}
	storedTrack, err := s.Track(context.Background(), track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if track.ContentType != "audio/mpeg" || filepath.Ext(storedTrack.PlayablePath) != ".mp3" {
		t.Fatalf("unexpected playable track: %#v", track)
	}
	if got := storedTrack.PlayablePath != storedTrack.OriginalPath; got != wantTranscode {
		t.Fatalf("transcoded=%v, want %v; original=%s playable=%s", got, wantTranscode, storedTrack.OriginalPath, storedTrack.PlayablePath)
	}

	playlistResponse := httptest.NewRecorder()
	handler.ServeHTTP(playlistResponse, httptest.NewRequest(http.MethodGet, "/api/playlists", nil))
	var playlists []store.Playlist
	if err = json.Unmarshal(playlistResponse.Body.Bytes(), &playlists); err != nil {
		t.Fatal(err)
	}
	if len(playlists) != 1 || len(playlists[0].Tracks) != 1 || playlists[0].Tracks[0].ID != track.ID {
		t.Fatalf("uploaded track missing from default playlist: %s", playlistResponse.Body.String())
	}

	rangeRequest := httptest.NewRequest(http.MethodGet, track.AudioURL, nil)
	rangeRequest.Header.Set("Range", "bytes=0-31")
	rangeResponse := httptest.NewRecorder()
	handler.ServeHTTP(rangeResponse, rangeRequest)
	if rangeResponse.Code != http.StatusPartialContent || rangeResponse.Body.Len() != 32 {
		t.Fatalf("range status=%d length=%d", rangeResponse.Code, rangeResponse.Body.Len())
	}
}

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
