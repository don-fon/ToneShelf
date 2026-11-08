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
	"time"

	"music-player/server/internal/lyrics"
	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

func TestLyricsSearchEndpointReturnsCandidatesWithoutSaving(t *testing.T) {
	var gotTitle, gotArtist string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotTitle == "" {
			gotTitle, gotArtist = r.URL.Query().Get("track_name"), r.URL.Query().Get("artist_name")
		}
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
	if uploadResponse.Code != http.StatusAccepted {
		t.Fatalf("upload status=%d body=%s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var queued store.UploadJob
	if err = json.Unmarshal(uploadResponse.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	job := finishTestJob(t, api, queued.ID, "succeeded")
	track, err := s.Track(context.Background(), job.TrackID)
	if err != nil {
		t.Fatal(err)
	}
	storedTrack, err := s.Track(context.Background(), track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if track.OriginalContentType == "" || (track.OriginalContentType != "audio/aac" && track.OriginalAudioURL == "") || (track.OriginalContentType == "audio/aac" && track.OriginalAudioURL != "") {
		t.Fatalf("missing original source: %#v", track)
	}
	originalRequest := httptest.NewRequest(http.MethodGet, "/api/tracks/"+track.ID+"/audio?source=original", nil)
	originalRequest.Header.Set("Range", "bytes=0-31")
	originalResponse := httptest.NewRecorder()
	handler.ServeHTTP(originalResponse, originalRequest)
	if originalResponse.Code != http.StatusPartialContent || originalResponse.Header().Get("Content-Type") != track.OriginalContentType {
		t.Fatalf("original status=%d type=%s", originalResponse.Code, originalResponse.Header().Get("Content-Type"))
	}
	originalBytes, err := os.ReadFile(sourcePath)
	if err != nil || !bytes.Equal(originalResponse.Body.Bytes(), originalBytes[:32]) {
		t.Fatalf("original bytes differ: %v", err)
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

	oldPaths := []string{storedTrack.OriginalPath, storedTrack.PlayablePath}
	var invalidBody bytes.Buffer
	invalidForm := multipart.NewWriter(&invalidBody)
	invalidPart, err := invalidForm.CreateFormFile("file", "broken.aac")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = invalidPart.Write([]byte("not audio"))
	_ = invalidForm.Close()
	invalidRequest := httptest.NewRequest(http.MethodPut, "/api/tracks/"+track.ID+"/audio", &invalidBody)
	invalidRequest.Header.Set("Content-Type", invalidForm.FormDataContentType())
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusAccepted {
		t.Fatalf("invalid replacement status=%d body=%s", invalidResponse.Code, invalidResponse.Body.String())
	}
	if err = json.Unmarshal(invalidResponse.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	finishTestJob(t, api, queued.ID, "failed")
	afterFailure, err := s.Track(context.Background(), track.ID)
	if err != nil || afterFailure.AudioVersion != track.AudioVersion || afterFailure.PlayablePath != storedTrack.PlayablePath {
		t.Fatalf("failed replacement changed active audio: %#v, %v", afterFailure, err)
	}
	for _, oldPath := range oldPaths {
		if _, statErr := os.Stat(oldPath); statErr != nil {
			t.Fatalf("failed replacement removed old audio: %s (%v)", oldPath, statErr)
		}
	}

	var replacementBody bytes.Buffer
	replacementForm := multipart.NewWriter(&replacementBody)
	replacementPart, err := replacementForm.CreateFormFile("file", "replacement"+filepath.Ext(filename))
	if err != nil {
		t.Fatal(err)
	}
	replacementSource, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(replacementPart, replacementSource); err != nil {
		t.Fatal(err)
	}
	_ = replacementSource.Close()
	if err = replacementForm.Close(); err != nil {
		t.Fatal(err)
	}
	replaceRequest := httptest.NewRequest(http.MethodPut, "/api/tracks/"+track.ID+"/audio", &replacementBody)
	replaceRequest.Header.Set("Content-Type", replacementForm.FormDataContentType())
	replaceResponse := httptest.NewRecorder()
	handler.ServeHTTP(replaceResponse, replaceRequest)
	if replaceResponse.Code != http.StatusAccepted {
		t.Fatalf("replace status=%d body=%s", replaceResponse.Code, replaceResponse.Body.String())
	}
	if err = json.Unmarshal(replaceResponse.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	job = finishTestJob(t, api, queued.ID, "succeeded")
	replaced, err := s.Track(context.Background(), job.TrackID)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.ID != track.ID || replaced.AudioVersion != 2 || replaced.AudioURL == track.AudioURL {
		t.Fatalf("replacement identity/version mismatch: %#v", replaced)
	}
	for _, oldPath := range oldPaths {
		if oldPath != "" {
			if _, statErr := os.Stat(oldPath); !os.IsNotExist(statErr) {
				t.Fatalf("old audio was not removed immediately: %s (%v)", oldPath, statErr)
			}
		}
	}
}

func TestPlaylistTrashAndRestoreEndpoints(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := store.Open(ctx, filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	path := filepath.Join(root, "audio.mp3")
	if err = os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.InsertTrack(ctx, store.NewTrack{ID: "track-1", Title: "test", OriginalPath: path, PlayablePath: path, ContentType: "audio/mpeg", SizeBytes: 5, DurationSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s, Media: media.New(root), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := api.Handler()

	conflict := httptest.NewRecorder()
	handler.ServeHTTP(conflict, httptest.NewRequest(http.MethodDelete, "/api/playlists/"+store.DefaultPlaylistID+"/tracks/track-1", nil))
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "requiresConfirmation") {
		t.Fatalf("missing confirmation response: status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	deleted := httptest.NewRecorder()
	handler.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/api/playlists/"+store.DefaultPlaylistID+"/tracks/track-1?confirmDelete=true", nil))
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	trash := httptest.NewRecorder()
	handler.ServeHTTP(trash, httptest.NewRequest(http.MethodGet, "/api/trash", nil))
	if trash.Code != http.StatusOK || !strings.Contains(trash.Body.String(), "track-1") {
		t.Fatalf("trash status=%d body=%s", trash.Code, trash.Body.String())
	}
	restored := httptest.NewRecorder()
	handler.ServeHTTP(restored, httptest.NewRequest(http.MethodPost, "/api/tracks/track-1/restore", nil))
	if restored.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", restored.Code, restored.Body.String())
	}
	playlists, err := s.Playlists(ctx)
	if err != nil || len(playlists[0].Tracks) != 1 {
		t.Fatalf("restored track missing: %#v, %v", playlists, err)
	}
}

func TestMaintenanceRetainsFailedPurgeAndRetries(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := store.Open(ctx, filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	blocked := filepath.Join(root, "blocked")
	if err = os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(blocked, "child"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.InsertTrack(ctx, store.NewTrack{ID: "track-1", Title: "test", OriginalPath: blocked, PlayablePath: blocked, ContentType: "audio/mpeg", SizeBytes: 1, DurationSeconds: 1}); err != nil {
		t.Fatal(err)
	}
	if err = s.SoftDeleteTrack(ctx, "track-1", time.Now().Add(-73*time.Hour)); err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	api.Maintain(ctx)
	trash, err := s.Trash(ctx)
	if err != nil || len(trash) != 1 || trash[0].PurgeError == "" {
		t.Fatalf("failed purge must remain retryable: %#v, %v", trash, err)
	}
	if err = os.Remove(filepath.Join(blocked, "child")); err != nil {
		t.Fatal(err)
	}
	api.Maintain(ctx)
	trash, err = s.Trash(ctx)
	if err != nil || len(trash) != 0 {
		t.Fatalf("retry did not complete purge: %#v, %v", trash, err)
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
	for _, test := range []struct {
		title  string
		status int
	}{
		{title: "新名称", status: http.StatusOK},
		{title: "  ", status: http.StatusBadRequest},
	} {
		payload, _ := json.Marshal(map[string]any{"title": test.title, "artist": "新歌手", "baseRandomWeight": 1, "playbackStartSeconds": 0, "playbackEndSeconds": nil, "volumeRatio": .8, "lyrics": ""})
		response := httptest.NewRecorder()
		api.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPatch, "/api/tracks/track-1", bytes.NewReader(payload)))
		if response.Code != test.status {
			t.Fatalf("identity update status=%d body=%s", response.Code, response.Body.String())
		}
	}
	stored, err := s.Track(context.Background(), "track-1")
	if err != nil || stored.Title != "新名称" || stored.Artist != "新歌手" {
		t.Fatalf("identity=%#v err=%v", stored, err)
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
func finishTestJob(t *testing.T, api *API, id, state string) store.UploadJob {
	t.Helper()
	jobs, err := api.Store.Jobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.ID != id {
			continue
		}
		if job.State == "queued" {
			if err = api.Store.SetJobState(context.Background(), id, "queued", "processing", ""); err != nil {
				t.Fatal(err)
			}
			api.processJob(context.Background(), job)
		}
	}
	jobs, err = api.Store.Jobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.ID == id {
			if job.State != state {
				t.Fatalf("job state=%s want=%s error=%s", job.State, state, job.Error)
			}
			return job
		}
	}
	t.Fatal("job not found")
	return store.UploadJob{}
}
