package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func prepareConflictJob(t *testing.T, s *Store, id, title string, playlistID ...string) (UploadJob, NewTrack) {
	t.Helper()
	ctx := testContext()
	job := UploadJob{ID: id, FileName: id + ".flac", SourcePath: id + ".flac", OutputPath: id + ".mp3"}
	if len(playlistID) > 0 {
		job.PlaylistID = playlistID[0]
	}
	for _, err := range []error{s.CreateJob(ctx, job), s.ReserveJob(ctx, id, 100, 10000), s.SetJobState(ctx, id, "receiving", "queued", ""), s.SetJobState(ctx, id, "queued", "processing", "")} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return job, NewTrack{ID: id, Title: title, Artist: "different artist", OriginalPath: job.SourcePath, PlayablePath: job.OutputPath, SizeBytes: 80, DurationSeconds: 10}
}

func TestDeletedUploadTargetCannotBeOverwritten(t *testing.T) {
	ctx := testContext()
	s := openTestStore(t)
	if _, err := s.InsertTrack(ctx, NewTrack{ID: "old", Title: "same", OriginalPath: "old", PlayablePath: "old", SizeBytes: 10, DurationSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	job, candidate := prepareConflictJob(t, s, "new", "same")
	if _, err := s.CompleteJob(ctx, job, candidate, nil); !errors.Is(err, ErrUploadConflict) {
		t.Fatal(err)
	}
	jobs, err := s.Jobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SoftDeleteTrack(ctx, "old", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConfirmUpload(ctx, jobs[0], []PendingFile{{Path: "old", SizeBytes: 10}}); err == nil {
		t.Fatal("deleted song was overwritten")
	}
	// A later upload does not silently restore or overwrite the trash entry.
	other, next := prepareConflictJob(t, s, "other", "same")
	if _, err = s.CompleteJob(ctx, other, next, nil); err != nil {
		t.Fatal(err)
	}
	jobs, err = s.Jobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range jobs {
		if item.ID == "new" && !item.RequiresConfirmation {
			t.Fatal("failed confirmation discarded candidate")
		}
	}
}

func TestConcurrentSameTitleUploadsRequireConfirmation(t *testing.T) {
	s := openTestStore(t)
	ctx := testContext()
	first, a := prepareConflictJob(t, s, "a", " Song ")
	second, b := prepareConflictJob(t, s, "b", "song")
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, item := range []struct {
		job   UploadJob
		track NewTrack
	}{{first, a}, {second, b}} {
		workers.Add(1)
		go func() { defer workers.Done(); _, err := s.CompleteJob(ctx, item.job, item.track, nil); results <- err }()
	}
	workers.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrUploadConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	used, reserved, err := s.Quota(ctx)
	if err != nil || used != 80 || reserved != 80 {
		t.Fatalf("quota %d %d %v", used, reserved, err)
	}
}

func TestUploadConflictRestartAndConfirmation(t *testing.T) {
	ctx := testContext()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	old, err := s.InsertTrack(ctx, NewTrack{ID: "old", Title: "Song", OriginalPath: "old", PlayablePath: "old", SizeBytes: 10, DurationSeconds: 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateTrack(ctx, old.ID, 2, 1, nil, .5, "lyrics"); err != nil {
		t.Fatal(err)
	}
	job, candidate := prepareConflictJob(t, s, "new", "song")
	if _, err = s.CompleteJob(ctx, job, candidate, nil); !errors.Is(err, ErrUploadConflict) {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := s.Jobs(ctx)
	if err != nil || len(jobs) != 1 || !jobs[0].RequiresConfirmation || jobs[0].Candidate == nil {
		t.Fatalf("jobs %#v %v", jobs, err)
	}
	waiting := jobs[0]
	renamed := "renamed"
	if _, err = s.UpdateTrack(ctx, old.ID, 2, 1, nil, .5, "lyrics", &renamed); err != nil {
		t.Fatal(err)
	}
	files := []PendingFile{{Path: "old", SizeBytes: 10}}
	if _, err = s.ConfirmUpload(ctx, waiting, files); err == nil {
		t.Fatal("renamed target was overwritten")
	}
	title := "Song"
	if _, err = s.UpdateTrack(ctx, old.ID, 2, 1, nil, .5, "lyrics", &title); err != nil {
		t.Fatal(err)
	}
	updated, err := s.ConfirmUpload(ctx, waiting, files)
	if err != nil || updated.ID != "old" || updated.AudioVersion != old.AudioVersion+1 || updated.BaseRandomWeight != 2 || updated.VolumeRatio != .5 || updated.PlaybackStartSeconds != 1 || updated.Lyrics != "[offset:0]\nlyrics" {
		t.Fatalf("updated %#v %v", updated, err)
	}
	if _, err = s.ConfirmUpload(ctx, waiting, files); err == nil {
		t.Fatal("confirmed twice")
	}
	pending, err := s.PendingFiles(ctx)
	if err != nil || len(pending) != 1 || pending[0].Path != "old" {
		t.Fatalf("cleanup %#v %v", pending, err)
	}
	jobs, err = s.Jobs(ctx)
	if err != nil || jobs[0].RequiresConfirmation || jobs[0].State != "succeeded" {
		t.Fatalf("jobs %#v %v", jobs, err)
	}
}
