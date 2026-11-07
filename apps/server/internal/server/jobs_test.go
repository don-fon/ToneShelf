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

	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

func TestRecoveryRequeuesFullFileAndCleansInterruptedUpload(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "test.db")
	s, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s, Media: media.New(root), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	source := filepath.Join(root, "source.wav")
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=0.2", source).CombinedOutput(); err != nil {
		t.Fatalf("generate: %v %s", err, output)
	}
	sourceInfo, _ := os.Stat(source)
	job := store.UploadJob{ID: "recover", FileName: "source.wav", SourcePath: source, OutputPath: filepath.Join(root, "candidate.mp3")}
	if err = s.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveJob(ctx, job.ID, sourceInfo.Size()+10, QuotaBytes); err != nil {
		t.Fatal(err)
	}
	for _, transition := range [][2]string{{"receiving", "queued"}, {"queued", "processing"}} {
		if err = s.SetJobState(ctx, job.ID, transition[0], transition[1], ""); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(job.OutputPath, []byte("unfinished"), 0o600); err != nil {
		t.Fatal(err)
	}
	partial := store.UploadJob{ID: "partial", FileName: "partial.aac", SourcePath: filepath.Join(root, "partial.aac"), OutputPath: filepath.Join(root, "partial.mp3")}
	if err = s.CreateJob(ctx, partial); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveJob(ctx, partial.ID, 7, QuotaBytes); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(partial.SourcePath, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	api.Store = s
	if err = api.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if err = api.RecoverJobs(ctx); err != nil {
		t.Fatal("recovery must be idempotent", err)
	}
	for _, path := range []string{partial.SourcePath, job.OutputPath} {
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("candidate not cleaned %s: %v", path, err)
		}
	}
	jobs, _ := s.Jobs(ctx)
	for _, got := range jobs {
		if got.ID == job.ID && (got.State != "queued" || got.ReservedBytes != sourceInfo.Size()) {
			t.Fatalf("recovery=%#v", got)
		}
		if got.ID == partial.ID && (got.State != "failed" || got.ReservedBytes != 0) {
			t.Fatalf("partial=%#v", got)
		}
	}
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); api.RunJobs(workerCtx) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		jobs, _ = s.Jobs(ctx)
		var complete bool
		for _, got := range jobs {
			if got.ID == job.ID {
				if got.State == "failed" {
					t.Fatal(got.Error)
				}
				complete = got.State == "succeeded"
			}
		}
		if complete {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("recovered job did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A refreshed client gets status from the server without any browser state.
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "succeeded") {
		t.Fatalf("job status=%d body=%s", response.Code, response.Body.String())
	}
	tracks, err := s.Playlists(ctx)
	if err != nil || len(tracks[0].Tracks) != 1 {
		t.Fatalf("recovered tracks=%#v err=%v", tracks, err)
	}
}

func TestUploadQuotaRejectsBeforeWritingAndReleasesReservation(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(context.Background(), filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	api := &API{Store: s, Media: media.New(root), QuotaLimit: 10}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "too-large.aac")
	_, _ = part.Write([]byte("more than ten bytes"))
	_ = form.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/tracks", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusInsufficientStorage {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	jobs, _ := s.Jobs(context.Background())
	if len(jobs) != 1 || jobs[0].State != "failed" || jobs[0].ReservedBytes != 0 {
		t.Fatalf("failed upload=%#v", jobs)
	}
	if _, err = os.Stat(jobs[0].SourcePath); !os.IsNotExist(err) {
		t.Fatalf("failed source remains: %v", err)
	}
	quotaResponse := httptest.NewRecorder()
	api.Handler().ServeHTTP(quotaResponse, httptest.NewRequest(http.MethodGet, "/api/quota", nil))
	var quota map[string]int64
	if err = json.Unmarshal(quotaResponse.Body.Bytes(), &quota); err != nil {
		t.Fatal(err)
	}
	if quota["remainingBytes"] != 10 || quota["reservedBytes"] != 0 {
		t.Fatalf("quota=%#v", quota)
	}
}

func TestSmallUploadFitsNearQuotaAndReleasesUnusedBatch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := store.Open(ctx, filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	api := &API{Store: s, Media: media.New(root), QuotaLimit: 10}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "small.mp3")
	_, _ = part.Write([]byte("abc"))
	_ = form.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/tracks", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	jobs, err := s.Jobs(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].State != "queued" || jobs[0].ReservedBytes != 3 {
		t.Fatalf("settlement=%#v err=%v", jobs, err)
	}
	data, err := os.ReadFile(jobs[0].SourcePath)
	if err != nil || string(data) != "abc" {
		t.Fatalf("source=%q err=%v", data, err)
	}
}

func TestTranscodeQuotaFailurePreservesOldAudioAndCleansCandidates(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	ctx := context.Background()
	root := t.TempDir()
	s, err := store.Open(ctx, filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source := filepath.Join(root, "candidate.wav")
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=0.2", source).CombinedOutput(); err != nil {
		t.Fatalf("generate: %v %s", err, output)
	}
	info, _ := os.Stat(source)
	oldPath := filepath.Join(root, "old.mp3")
	if err = os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.InsertTrack(ctx, store.NewTrack{ID: "old", Title: "old", OriginalPath: oldPath, PlayablePath: oldPath, SizeBytes: 3, DurationSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	job := store.UploadJob{ID: "quota", FileName: "candidate.wav", TargetTrackID: "old", SourcePath: source, OutputPath: filepath.Join(root, "candidate.mp3")}
	if err = s.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveJob(ctx, job.ID, info.Size(), QuotaBytes); err != nil {
		t.Fatal(err)
	}
	if err = s.SetJobState(ctx, job.ID, "receiving", "queued", ""); err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s, Media: media.New(root), QuotaLimit: info.Size() + 3 + 100}
	failed := finishTestJob(t, api, job.ID, "failed")
	if !strings.Contains(failed.Error, "空间不足") || failed.ReservedBytes != 0 {
		t.Fatalf("quota failure=%#v", failed)
	}
	for _, path := range []string{job.SourcePath, job.OutputPath} {
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("candidate leaked %s: %v", path, err)
		}
	}
	if data, err := os.ReadFile(oldPath); err != nil || string(data) != "old" {
		t.Fatalf("old audio changed: %q %v", data, err)
	}
	used, err := s.Usage(ctx)
	if err != nil || used != 3 {
		t.Fatalf("usage=%d err=%v", used, err)
	}
}

func TestStartupCleansOrphansAndProtectsReferencedMedia(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := store.Open(ctx, filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dir := filepath.Join(root, "media", "originals")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(dir, "active.mp3")
	orphan := filepath.Join(dir, "orphan.aac")
	for _, path := range []string{active, orphan} {
		if err = os.WriteFile(path, []byte("audio"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Equivalent paths must be protected on both Windows and Linux.
	withDot := dir + string(filepath.Separator) + "." + string(filepath.Separator) + "active.mp3"
	if _, err = s.InsertTrack(ctx, store.NewTrack{ID: "active", Title: "active", OriginalPath: withDot, PlayablePath: withDot, SizeBytes: 5, DurationSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s, Media: media.New(root)}
	if err = api.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(active); err != nil {
		t.Fatalf("active file removed: %v", err)
	}
	if _, err = os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan remained: %v", err)
	}
}

func TestFailedJobCleanupRetriesWithoutReleasingQuotaEarly(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	blocked := filepath.Join(root, "blocked")
	if err = os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(blocked, "child")
	if err = os.WriteFile(child, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := store.UploadJob{ID: "blocked", FileName: "blocked", SourcePath: blocked, OutputPath: filepath.Join(root, "missing")}
	if err = s.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveJob(ctx, job.ID, 1, QuotaBytes); err != nil {
		t.Fatal(err)
	}
	if err = s.SetJobState(ctx, job.ID, "receiving", "failed", "disk error"); err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s}
	api.Maintain(ctx)
	reserved, _ := s.ReservedBytes(ctx)
	if reserved != 1 {
		t.Fatal("failed cleanup released quota early")
	}
	if err = os.Remove(child); err != nil {
		t.Fatal(err)
	}
	api.Maintain(ctx)
	reserved, _ = s.ReservedBytes(ctx)
	if reserved != 0 {
		t.Fatal("cleanup did not release quota")
	}
}
