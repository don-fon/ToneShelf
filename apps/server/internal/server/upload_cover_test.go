package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

func coverUploadFixture(t *testing.T) (*API, store.Track, string, []byte) {
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
	api := &API{Store: s, Media: media.New(root)}
	t.Cleanup(func() { api.Store.Close() })
	original := filepath.Join(root, "media", "originals", "song.mp3")
	if err = os.WriteFile(original, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	track, err := s.InsertTrack(testContext(), store.NewTrack{ID: "song", Title: "Song", Artist: "Singer", OriginalPath: original, PlayablePath: original, ContentType: "audio/mpeg", OriginalContentType: "audio/mpeg", PlaybackIndexVersion: 1, DurationProbeVersion: 1, SizeBytes: 8, DurationSeconds: 120})
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 700, 350))
	for y := 0; y < 350; y++ {
		for x := 0; x < 700; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	return api, track, root, encoded.Bytes()
}
func postCover(t *testing.T, api *API, data []byte, want int) store.UploadJob {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "cover.png")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	form.Close()
	request := httptest.NewRequest("POST", "/api/tracks/song/cover", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("upload=%d want=%d %s", response.Code, want, response.Body.String())
	}
	if want != 202 {
		return store.UploadJob{}
	}
	var reply store.UploadJob
	if err = json.Unmarshal(response.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	jobs, err := api.Store.Jobs(testContext())
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.ID == reply.ID {
			return job
		}
	}
	t.Fatal("job missing")
	return store.UploadJob{}
}
func finishCoverUpload(t *testing.T, api *API, job store.UploadJob) {
	t.Helper()
	if err := api.Store.SetJobState(testContext(), job.ID, "queued", "processing", ""); err != nil {
		t.Fatal(err)
	}
	api.processJob(testContext(), job)
}

func TestUploadedCoverReplacesAtomicallyWithoutChangingAudio(t *testing.T) {
	api, track, _, picture := coverUploadFixture(t)
	ctx := testContext()
	job := postCover(t, api, picture, 202)
	before, _ := api.Store.Track(ctx, track.ID)
	if before.CoverPath != "" {
		t.Fatal("upload activated before conversion")
	}
	finishCoverUpload(t, api, job)
	first, err := api.Store.Track(ctx, track.ID)
	if err != nil || first.AudioVersion != track.AudioVersion || first.CoverVersion != 2 || first.Title != track.Title {
		t.Fatalf("first=%+v %v", first, err)
	}
	file, err := os.Open(first.CoverPath)
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(file)
	file.Close()
	if err != nil || config.Width != 512 || config.Height != 256 {
		t.Fatalf("JPEG=%+v %v", config, err)
	}
	job = postCover(t, api, picture, 202)
	finishCoverUpload(t, api, job)
	second, err := api.Store.Track(ctx, track.ID)
	if err != nil || second.CoverVersion != 3 || second.CoverURL == first.CoverURL || second.AudioVersion != first.AudioVersion || second.SizeBytes != first.SizeBytes {
		t.Fatalf("replacement=%+v %v", second, err)
	}
	for _, candidate := range []string{first.CoverPath, job.CoverInputPath} {
		if _, err = os.Stat(candidate); !os.IsNotExist(err) {
			t.Fatal("old or temporary file retained", candidate, err)
		}
	}
	used, reserved, err := api.Store.Quota(ctx)
	if err != nil || used != second.SizeBytes || reserved != 0 {
		t.Fatalf("quota=%d %d %v", used, reserved, err)
	}
}

func TestCoverUploadFailuresKeepOldCoverAndReleaseQuota(t *testing.T) {
	api, track, _, picture := coverUploadFixture(t)
	ctx := testContext()
	job := postCover(t, api, picture, 202)
	finishCoverUpload(t, api, job)
	old, _ := api.Store.Track(ctx, track.ID)
	api.QuotaLimit = old.SizeBytes + 1
	postCover(t, api, picture, 507)
	api.QuotaLimit = 0
	for _, invalid := range [][]byte{nil, bytes.Repeat([]byte("x"), int(media.MaxCoverBytes)+1)} {
		postCover(t, api, invalid, 400)
	}
	job = postCover(t, api, []byte("not a picture"), 202)
	finishCoverUpload(t, api, job)
	current, _ := api.Store.Track(ctx, track.ID)
	if current.CoverPath != old.CoverPath || current.AudioVersion != old.AudioVersion {
		t.Fatal("failure changed cover or audio")
	}
	for _, file := range []string{track.OriginalPath, old.CoverPath} {
		if _, err := os.Stat(file); err != nil {
			t.Fatal("original removed", err)
		}
	}
	for _, file := range []string{job.OutputPath, job.CoverInputPath} {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatal("failed candidate retained", file, err)
		}
	}
	used, reserved, err := api.Store.Quota(ctx)
	if err != nil || used != old.SizeBytes || reserved != 0 {
		t.Fatalf("failure quota=%d %d %v", used, reserved, err)
	}
}

func TestCoverUploadRestartPreservesInputAndResumesConversion(t *testing.T) {
	api, track, root, picture := coverUploadFixture(t)
	ctx := testContext()
	job := postCover(t, api, picture, 202)
	if err := api.Store.SetJobState(ctx, job.ID, "queued", "processing", ""); err != nil {
		t.Fatal(err)
	}
	if err := api.Store.ReserveJob(ctx, job.ID, 7, QuotaBytes); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(job.OutputPath, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	api.Store.Close()
	s, err := store.Open(ctx, filepath.Join(root, "test.db"), store.WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	api.Store = s
	if err = api.RecoverJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	jobs, _ := s.Jobs(ctx)
	for _, recovered := range jobs {
		if recovered.ID == job.ID {
			if recovered.State != "queued" || recovered.ReservedBytes != int64(len(picture)) {
				t.Fatalf("recovery=%+v", recovered)
			}
			if _, err = os.Stat(recovered.CoverInputPath); err != nil {
				t.Fatal("restart removed upload", err)
			}
			finishCoverUpload(t, api, recovered)
		}
	}
	updated, _ := s.Track(ctx, track.ID)
	if updated.CoverPath == "" {
		t.Fatal("cover not recovered")
	}
}

func TestCoverUploadOwnerChecksAndRetiredOnlineRoutes(t *testing.T) {
	api, _, _, picture := coverUploadFixture(t)
	api.ResolveUser = func(*http.Request) (string, error) { return "other", nil }
	postCover(t, api, picture, 404)
	api.ResolveUser = nil
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, httptest.NewRequest("POST", "/api/tracks/song/cover/search", strings.NewReader(`{}`)))
	if response.Code != 404 {
		t.Fatalf("retired route=%d", response.Code)
	}
	response = httptest.NewRecorder()
	api.Handler().ServeHTTP(response, httptest.NewRequest("POST", "/api/tracks/song/cover", strings.NewReader(`{"releaseId":"old"}`)))
	if response.Code != 400 {
		t.Fatalf("JSON request=%d", response.Code)
	}
}

func TestInterruptedCoverUploadCleansInputAndPreservesAudio(t *testing.T) {
	api, track, _, picture := coverUploadFixture(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "interrupted.png")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(picture) // No closing multipart boundary: interrupted transfer.
	request := httptest.NewRequest("POST", "/api/tracks/song/cover", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, request)
	if response.Code != 400 {
		t.Fatalf("interrupted=%d %s", response.Code, response.Body.String())
	}
	jobs, err := api.Store.Jobs(testContext())
	if err != nil || len(jobs) != 1 || jobs[0].State != "failed" || jobs[0].ReservedBytes != 0 {
		t.Fatalf("interrupted job=%+v %v", jobs, err)
	}
	if _, err = os.Stat(jobs[0].CoverInputPath); !os.IsNotExist(err) {
		t.Fatal("partial upload retained", err)
	}
	if _, err = os.Stat(track.OriginalPath); err != nil {
		t.Fatal("interruption removed audio", err)
	}
}

func TestRetiredOnlineJobsAreCleanedDuringMigrationWithoutDeletingAudio(t *testing.T) {
	api, track, root, _ := coverUploadFixture(t)
	ctx := testContext()
	output := filepath.Join(root, "media", "playable", "legacy.jpg")
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output+".download", []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	api.Store.Close()
	db, err := sql.Open("sqlite", filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = goose.DownTo(db, ".", 13); err != nil {
		db.Close()
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO upload_jobs(id,owner_user_id,file_name,kind,target_track_id,source_path,output_path,state,reserved_bytes,cover_release_id,created_at,updated_at)VALUES('legacy',?,'legacy.jpg','cover','song','media/originals/song.mp3','media/playable/legacy.jpg','queued',7,'legacy-release',?,?)`, store.DefaultUserID, "2026-11-09T00:00:00Z", "2026-11-09T00:00:00Z")
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	s, err := store.Open(ctx, filepath.Join(root, "test.db"), store.WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	api.Store = s
	if err = api.RecoverJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	jobs, _ := s.Jobs(ctx)
	for _, job := range jobs {
		if job.ID == "legacy" && (job.State != "failed" || job.ReservedBytes != 0) {
			t.Fatalf("retired job=%+v", job)
		}
	}
	if _, err = os.Stat(track.OriginalPath); err != nil {
		t.Fatal("migration removed audio", err)
	}
	if _, err = os.Stat(output + ".download"); !os.IsNotExist(err) {
		t.Fatal("legacy download retained", err)
	}
}
