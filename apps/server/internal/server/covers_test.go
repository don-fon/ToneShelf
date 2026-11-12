package server

import (
	"music-player/server/internal/currentuser"
	"music-player/server/internal/media"
	"music-player/server/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCoverAccessReplacementAndTrashLifecycle(t *testing.T) {
	ctx := testContext()
	root := t.TempDir()
	s, err := store.Open(ctx, filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	original, cover := filepath.Join(root, "audio.mp3"), filepath.Join(root, "art.jpg")
	for _, path := range []string{original, cover} {
		if err = os.WriteFile(path, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	track, err := s.InsertTrack(ctx, store.NewTrack{ID: "song", Title: "song", OriginalPath: original, PlayablePath: original, CoverPath: cover, SizeBytes: 8, DurationSeconds: 2})
	if err != nil {
		t.Fatal(err)
	}
	api := &API{ResolveUser: testUserResolver, Store: s, Media: media.New(root)}
	get := func(id string, want int) {
		t.Helper()
		response := httptest.NewRecorder()
		api.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/tracks/"+id+"/cover", nil))
		if response.Code != want {
			t.Fatalf("cover status=%d body=%s", response.Code, response.Body.String())
		}
		if want == 200 && response.Header().Get("Content-Type") != "image/jpeg" {
			t.Fatal("incorrect image MIME")
		}
	}
	get(track.ID, 200)
	get("missing", 404)
	if err = s.EnsureUser(ctx, "other", "other", "other-default"); err != nil {
		t.Fatal(err)
	}
	_, err = s.InsertTrack(currentuser.WithID(ctx, "other"), store.NewTrack{ID: "foreign", Title: "foreign", OriginalPath: original, PlayablePath: original, CoverPath: cover, SizeBytes: 0, DurationSeconds: 2})
	if err != nil {
		t.Fatal(err)
	}
	get("foreign", 404)
	if err = s.SoftDeleteTrack(ctx, track.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	get(track.ID, 404)
	if _, err = os.Stat(cover); err != nil {
		t.Fatal("trash must retain cover", err)
	}
	if _, err = s.RestoreTrack(ctx, track.ID, "restored-item"); err != nil {
		t.Fatal(err)
	}
	get(track.ID, 200)
	replacement := filepath.Join(root, "new.mp3")
	if err = os.WriteFile(replacement, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReplaceTrack(ctx, track.ID, store.NewTrack{OriginalPath: replacement, PlayablePath: replacement, SizeBytes: 3, DurationSeconds: 2}, storedFiles(track)); err != nil {
		t.Fatal(err)
	}
	api.Maintain(ctx)
	get(track.ID, 200)
	if _, err = os.Stat(cover); err != nil {
		t.Fatal("retained cover removed", err)
	}
	for _, path := range []string{original} {
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("old version retained: %s %v", path, err)
		}
	}
}

func TestCoverRecoveryNeverDeletesOriginal(t *testing.T) {
	ctx := testContext()
	root := t.TempDir()
	db := filepath.Join(root, "test.db")
	s, err := store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	original, output := filepath.Join(root, "source.mp3"), filepath.Join(root, "candidate.jpg")
	if err = os.WriteFile(original, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	track, err := s.InsertTrack(ctx, store.NewTrack{ID: "song", Title: "song", OriginalPath: original, PlayablePath: original, SizeBytes: 8, DurationSeconds: 2})
	if err != nil {
		t.Fatal(err)
	}
	job := store.UploadJob{ID: "cover-job", Kind: "cover", State: "queued", TargetTrackID: track.ID, SourcePath: original, OutputPath: output, FileName: "source.mp3"}
	if err = s.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err = s.SetJobState(ctx, job.ID, "receiving", "processing", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveJob(ctx, job.ID, 7, 100); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	api := &API{ResolveUser: testUserResolver, Store: s, Media: media.New(root)}
	if err = api.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.Jobs(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].State != "queued" || jobs[0].ReservedBytes != 0 {
		t.Fatalf("recovery=%+v %v", jobs, err)
	}
	if _, err = os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("partial retained", err)
	}
	if _, err = os.Stat(original); err != nil {
		t.Fatal("recovery removed original", err)
	}
	api.cleanFailedJob(ctx, job)
	if _, err = os.Stat(original); err != nil {
		t.Fatal("failure cleanup removed original", err)
	}
}
