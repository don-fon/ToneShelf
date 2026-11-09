package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"music-player/server/internal/artwork"
	"music-player/server/internal/currentuser"
	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

const coverTestRelease = "11111111-1111-4111-8111-111111111111"

func onlineCoverFixture(t *testing.T) (*API, store.Track, string) {
	t.Helper()
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "media", "originals"), 0700); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(testContext(), filepath.Join(root, "test.db"), store.WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	original := filepath.Join(root, "media", "originals", "song.mp3")
	if err = os.WriteFile(original, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	track, err := s.InsertTrack(testContext(), store.NewTrack{ID: "song", Title: "Song", Artist: "Singer", Album: "Album", OriginalPath: original, PlayablePath: original, ContentType: "audio/mpeg", OriginalContentType: "audio/mpeg", PlaybackIndexVersion: 1, DurationProbeVersion: 1, SizeBytes: 8, DurationSeconds: 120})
	if err != nil {
		t.Fatal(err)
	}
	var jpegBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 700, 350))
	for y := 0; y < 350; y++ {
		for x := 0; x < 700; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	if err = jpeg.Encode(&jpegBytes, img, nil); err != nil {
		t.Fatal(err)
	}
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/release/"):
			json.NewEncoder(w).Encode(map[string]any{"releases": []any{map[string]any{"id": coverTestRelease, "title": "Album", "artist-credit": []any{map[string]any{"name": "Singer"}}}}})
		case strings.HasPrefix(r.URL.Path, "/ws/2/recording/"):
			w.Write([]byte(`{"recordings":[]}`))
		case strings.HasPrefix(r.URL.Path, "/release/"):
			json.NewEncoder(w).Encode(map[string]any{"images": []any{map[string]any{"front": true, "thumbnails": map[string]string{"500": upstream.URL + "/image"}}}})
		case r.URL.Path == "/image":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(jpegBytes.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	api := &API{Store: s, Media: media.New(root), Artwork: artwork.NewWithEndpoints(upstream.URL, upstream.URL, 0)}
	return api, track, root
}

func queueCover(t *testing.T, api *API) store.UploadJob {
	t.Helper()
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, httptest.NewRequest("POST", "/api/tracks/song/cover", strings.NewReader(`{"releaseId":"`+coverTestRelease+`"}`)))
	if response.Code != 202 {
		t.Fatalf("apply=%d %s", response.Code, response.Body.String())
	}
	var job store.UploadJob
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	jobs, err := api.Store.Jobs(testContext())
	if err != nil {
		t.Fatal(err)
	}
	for _, stored := range jobs {
		if stored.ID == job.ID {
			return stored
		}
	}
	t.Fatal("missing job")
	return job
}
func completeCover(t *testing.T, api *API, job store.UploadJob) {
	t.Helper()
	if err := api.Store.SetJobState(testContext(), job.ID, "queued", "processing", ""); err != nil {
		t.Fatal(err)
	}
	api.processJob(testContext(), job)
}

func TestOnlineCoverSearchPreviewAndAtomicReplacement(t *testing.T) {
	api, track, _ := onlineCoverFixture(t)
	ctx := testContext()
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, httptest.NewRequest("POST", "/api/tracks/song/cover/search", strings.NewReader(`{"title":"Song","artist":"Singer","album":"Album"}`)))
	if response.Code != 200 {
		t.Fatalf("search=%d %s", response.Code, response.Body.String())
	}
	untouched, _ := api.Store.Track(ctx, track.ID)
	if untouched.CoverPath != "" {
		t.Fatal("search saved an image")
	}
	response = httptest.NewRecorder()
	api.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/tracks/song/cover/candidates/"+coverTestRelease, nil))
	if response.Code != 200 || response.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("preview=%d %s", response.Code, response.Body.String())
	}
	job := queueCover(t, api)
	completeCover(t, api, job)
	first, err := api.Store.Track(ctx, track.ID)
	if err != nil || first.CoverPath == "" || first.AudioVersion != track.AudioVersion || first.CoverVersion != 2 {
		t.Fatalf("first=%+v %v", first, err)
	}
	f, err := os.Open(first.CoverPath)
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(f)
	f.Close()
	if err != nil || config.Width != 512 || config.Height != 256 {
		t.Fatalf("JPEG=%+v %v", config, err)
	}
	job = queueCover(t, api)
	completeCover(t, api, job)
	second, err := api.Store.Track(ctx, track.ID)
	if err != nil || second.CoverVersion != 3 || second.AudioVersion != track.AudioVersion || second.CoverURL == first.CoverURL || second.SizeBytes != first.SizeBytes {
		t.Fatalf("replacement=%+v %v", second, err)
	}
	if _, err = os.Stat(first.CoverPath); !os.IsNotExist(err) {
		t.Fatal("old cover not removed", err)
	}
	used, reserved, err := api.Store.Quota(ctx)
	if err != nil || used != second.SizeBytes || reserved != 0 {
		t.Fatalf("quota=%d %d %v", used, reserved, err)
	}
}

func TestOnlineCoverFailureAndRestartRetainAudioAndExistingCover(t *testing.T) {
	api, track, _ := onlineCoverFixture(t)
	ctx := testContext()
	job := queueCover(t, api)
	completeCover(t, api, job)
	old, _ := api.Store.Track(ctx, track.ID)
	api.QuotaLimit = old.SizeBytes + 1
	job = queueCover(t, api)
	completeCover(t, api, job)
	jobs, _ := api.Store.Jobs(ctx)
	for _, result := range jobs {
		if result.ID == job.ID && result.State != "failed" {
			t.Fatalf("quota failure=%+v", result)
		}
	}
	kept, _ := api.Store.Track(ctx, track.ID)
	if kept.CoverPath != old.CoverPath || kept.AudioVersion != old.AudioVersion {
		t.Fatal("failure changed existing cover")
	}
	for _, file := range []string{track.OriginalPath, old.CoverPath} {
		if _, err := os.Stat(file); err != nil {
			t.Fatal("failure removed original", err)
		}
	}
	api.QuotaLimit = 0
	job = queueCover(t, api)
	if err := api.Store.SetJobState(ctx, job.ID, "queued", "processing", ""); err != nil {
		t.Fatal(err)
	}
	if err := api.Store.ReserveJob(ctx, job.ID, 7, QuotaBytes); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(job.OutputPath+".download", []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := api.RecoverJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	jobs, _ = api.Store.Jobs(ctx)
	for _, result := range jobs {
		if result.ID == job.ID {
			if result.CoverReleaseID != coverTestRelease || result.State != "queued" || result.ReservedBytes != 0 {
				t.Fatalf("recovery=%+v", result)
			}
			completeCover(t, api, result)
		}
	}
	if _, err := os.Stat(job.OutputPath + ".download"); !os.IsNotExist(err) {
		t.Fatal("raw candidate survived restart", err)
	}
}

func TestOnlineCoverEndpointsRejectOtherUsersAndBadIdentifiers(t *testing.T) {
	api, _, _ := onlineCoverFixture(t)
	api.ResolveUser = func(*http.Request) (string, error) { return "foreign", nil }
	for _, endpoint := range []string{"/api/tracks/song/cover/search", "/api/tracks/song/cover"} {
		response := httptest.NewRecorder()
		api.Handler().ServeHTTP(response, httptest.NewRequest("POST", endpoint, strings.NewReader(`{}`)))
		if response.Code != 404 {
			t.Fatalf("foreign=%d", response.Code)
		}
	}
	api.ResolveUser = func(*http.Request) (string, error) { return currentuser.ID(testContext()), nil }
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, httptest.NewRequest("POST", "/api/tracks/song/cover", strings.NewReader(`{"releaseId":"http://localhost/private"}`)))
	if response.Code != 400 {
		t.Fatalf("identifier=%d", response.Code)
	}
}

func TestInvalidDownloadedCoverKeepsExistingCoverAndCleansTemporaryFiles(t *testing.T) {
	api, track, _ := onlineCoverFixture(t)
	ctx := testContext()
	initial := queueCover(t, api)
	completeCover(t, api, initial)
	old, _ := api.Store.Track(ctx, track.ID)
	var invalid *httptest.Server
	invalid = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/release/") {
			json.NewEncoder(w).Encode(map[string]any{"images": []any{map[string]any{"front": true, "image": invalid.URL + "/image"}}})
		} else {
			w.Write([]byte("not an image"))
		}
	}))
	defer invalid.Close()
	api.Artwork = artwork.NewWithEndpoints(invalid.URL, invalid.URL, 0)
	job := queueCover(t, api)
	completeCover(t, api, job)
	current, err := api.Store.Track(ctx, track.ID)
	if err != nil || current.CoverPath != old.CoverPath || current.CoverVersion != old.CoverVersion {
		t.Fatalf("failure changed cover=%+v %v", current, err)
	}
	for _, file := range []string{job.OutputPath, job.OutputPath + ".download"} {
		if _, err = os.Stat(file); !os.IsNotExist(err) {
			t.Fatal("failed candidate retained", file, err)
		}
	}
	used, reserved, err := api.Store.Quota(ctx)
	if err != nil || used != old.SizeBytes || reserved != 0 {
		t.Fatalf("quota after invalid image=%d %d %v", used, reserved, err)
	}
}
