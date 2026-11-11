package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	"music-player/server/internal/currentuser"
	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

func TestRequestCurrentUserIsolationAndSpoofing(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(context.Background(), filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.EnsureUser(context.Background(), "user-b", "B", "default-b"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{store.DefaultUserID, "user-b"} {
		ctx := currentuser.WithID(context.Background(), id)
		path := filepath.Join(root, id+".mp3")
		if err = os.WriteFile(path, []byte("private-"+id), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = s.InsertTrack(ctx, store.NewTrack{ID: id, Title: "secret-" + id, OriginalPath: path, PlayablePath: path, SizeBytes: 10, DurationSeconds: 10}); err != nil {
			t.Fatal(err)
		}
		if err = s.CreateJob(ctx, store.UploadJob{ID: "job-" + id, FileName: id, SourcePath: id + "-source", OutputPath: id + "-output"}); err != nil {
			t.Fatal(err)
		}
	}
	api := &API{Store: s, Media: media.New(root), ResolveUser: func(*http.Request) (string, error) { return "user-b", nil }}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-User-ID", "user-b")
		out := httptest.NewRecorder()
		api.Handler().ServeHTTP(out, r)
		return out
	}
	out := request(http.MethodGet, "/api/playlists?userId="+store.DefaultUserID, "")
	if out.Code != http.StatusOK || !strings.Contains(out.Body.String(), "secret-user-b") || strings.Contains(out.Body.String(), "secret-"+store.DefaultUserID) {
		t.Fatalf("lists %d %s", out.Code, out.Body.String())
	}
	out = request(http.MethodGet, "/api/jobs", "")
	if out.Code != http.StatusOK || strings.Contains(out.Body.String(), "job-"+store.DefaultUserID) || !strings.Contains(out.Body.String(), "job-user-b") {
		t.Fatalf("jobs %s", out.Body.String())
	}
	if out = request(http.MethodGet, "/api/tracks/"+store.DefaultUserID+"/audio", ""); out.Code != http.StatusNotFound {
		t.Fatalf("foreign audio %d", out.Code)
	}
	if out = request(http.MethodPatch, "/api/tracks/"+store.DefaultUserID, `{"baseRandomWeight":1,"volumeRatio":0.8}`); out.Code < 400 {
		t.Fatalf("foreign update %d", out.Code)
	}
	if out = request(http.MethodPost, "/api/jobs/job-"+store.DefaultUserID+"/resolve", `{"overwrite":false}`); out.Code != http.StatusNotFound {
		t.Fatalf("foreign job %d", out.Code)
	}
	if out = request(http.MethodGet, "/api/tracks/user-b/audio", ""); out.Code != http.StatusOK {
		t.Fatalf("own audio %d", out.Code)
	}
	api.ResolveUser = nil
	out = request(http.MethodGet, "/api/playlists?userId=user-b", "")
	if out.Code != http.StatusUnauthorized {
		t.Fatalf("default resolver %s", out.Body.String())
	}
	for _, resolver := range []UserResolver{func(*http.Request) (string, error) { return "", nil }, func(*http.Request) (string, error) { return "", errors.New("no session") }} {
		api.ResolveUser = resolver
		if out = request(http.MethodGet, "/api/playlists", ""); out.Code != http.StatusUnauthorized {
			t.Fatalf("resolver failure fell back %d", out.Code)
		}
	}
}

func TestBackgroundRecoveryAndPlaybackFilesUsePersistedOwners(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	root := t.TempDir()
	db := filepath.Join(root, "test.db")
	ctx := context.Background()
	s, err := store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err = s.EnsureUser(ctx, "user-b", "B", "default-b"); err != nil {
		t.Fatal(err)
	}
	sample := filepath.Join(root, "sample.mp3")
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=0.2", "-metadata", "title=Same title", sample).CombinedOutput(); err != nil {
		t.Fatalf("fixture %s %v", output, err)
	}
	data, err := os.ReadFile(sample)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(root, "media", "originals"), 0700); err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{store.DefaultUserID, "user-b"} {
		userCtx := currentuser.WithID(ctx, id)
		playlist := "source-" + id
		if _, err = s.CreatePlaylist(userCtx, playlist, playlist); err != nil {
			t.Fatal(err)
		}
		job := store.UploadJob{ID: "job-" + id, PlaylistID: playlist, FileName: "same.mp3", SourcePath: filepath.Join(root, "media", "originals", id+".mp3"), OutputPath: filepath.Join(root, "media", "playable", id+".mp3")}
		if err = os.WriteFile(job.SourcePath, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err = s.CreateJob(userCtx, job); err != nil {
			t.Fatal(err)
		}
		if err = s.ReserveJob(userCtx, job.ID, int64(len(data)), QuotaBytes); err != nil {
			t.Fatal(err)
		}
		if err = s.QueueUpload(userCtx, job.ID, int64(len(data))); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			if err = s.SetJobState(userCtx, job.ID, "queued", "processing", ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.Close()
	s, err = store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	api := &API{ResolveUser: testUserResolver, Store: s, Media: media.New(root), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err = api.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{store.DefaultUserID, "user-b"} {
		jobs, err := s.Jobs(currentuser.WithID(ctx, id))
		if err != nil || len(jobs) != 1 || jobs[0].State != "queued" {
			t.Fatalf("recovery %s %#v %v", id, jobs, err)
		}
		if _, err = os.Stat(jobs[0].SourcePath); err != nil {
			t.Fatalf("other owner's source cleaned %v", err)
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); api.RunJobs(runCtx) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(8 * time.Second)
	for {
		complete := true
		for _, id := range []string{store.DefaultUserID, "user-b"} {
			jobs, err := s.Jobs(currentuser.WithID(ctx, id))
			if err != nil {
				t.Fatal(err)
			}
			if jobs[0].State == "failed" {
				t.Fatalf("worker owner %s %#v", id, jobs[0])
			}
			complete = complete && jobs[0].State == "succeeded"
		}
		if complete {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("workers did not finish")
		}
		time.Sleep(25 * time.Millisecond)
	}
	for _, id := range []string{store.DefaultUserID, "user-b"} {
		lists, err := s.Playlists(currentuser.WithID(ctx, id))
		if err != nil || len(lists) != 2 {
			t.Fatalf("lists %#v %v", lists, err)
		}
		for _, list := range lists {
			if len(list.Tracks) != 1 || list.Tracks[0].ID != "job-"+id {
				t.Fatalf("wrong owner activation %#v", list)
			}
		}
	}
}

func TestFailedReceptionCleanupRetainsCurrentUser(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.EnsureUser(ctx, "user-b", "B", "default-b"); err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s, Media: media.New(root), QuotaLimit: 1, ResolveUser: func(*http.Request) (string, error) { return "user-b", nil }}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "large.mp3")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("audio"))
	form.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/tracks", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusInsufficientStorage {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
	jobs, err := s.Jobs(currentuser.WithID(ctx, "user-b"))
	if err != nil || len(jobs) != 1 || jobs[0].State != "failed" || jobs[0].ReservedBytes != 0 {
		t.Fatalf("cleanup owner %#v %v", jobs, err)
	}
	if _, err = os.Stat(jobs[0].SourcePath); !os.IsNotExist(err) {
		t.Fatalf("source leaked %v", err)
	}
	own, err := s.Jobs(testContext())
	if err != nil || len(own) != 0 {
		t.Fatalf("default account polluted %#v %v", own, err)
	}
	encoded, _ := json.Marshal(jobs)
	if !strings.Contains(string(encoded), "存储空间不足") {
		t.Fatalf("lost diagnostic %s", encoded)
	}
}
