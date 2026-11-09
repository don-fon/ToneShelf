package store

import "testing"

func TestConcurrentCoverCompletionCannotReplaceANewerSelection(t *testing.T) {
	ctx := testContext()
	s := openTestStore(t)
	if _, err := s.InsertTrack(ctx, NewTrack{ID: "song", OriginalPath: "original", PlayablePath: "original", CoverPath: "old-cover", DurationSeconds: 10, SizeBytes: 12}); err != nil {
		t.Fatal(err)
	}
	jobs := []UploadJob{{ID: "first", Kind: "cover", CoverInputPath: "first-input", SourcePath: "original", OutputPath: "first-cover", TargetTrackID: "song"}, {ID: "second", Kind: "cover", CoverInputPath: "second-input", SourcePath: "original", OutputPath: "second-cover", TargetTrackID: "song"}}
	for _, job := range jobs {
		if err := s.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
		if err := s.SetJobState(ctx, job.ID, "receiving", "processing", ""); err != nil {
			t.Fatal(err)
		}
		if err := s.ReserveJob(ctx, job.ID, 20, 1000); err != nil {
			t.Fatal(err)
		}
	}
	old := PendingFile{Path: "old-cover", SizeBytes: 4}
	if err := s.CompleteUploadedCover(ctx, jobs[0], old, 3, 5); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteUploadedCover(ctx, jobs[1], old, 3, 6); err == nil {
		t.Fatal("stale task replaced a newer cover")
	}
	track, err := s.Track(ctx, "song")
	if err != nil || track.CoverPath != "first-cover" || track.CoverVersion != 2 || track.AudioVersion != 1 || track.SizeBytes != 13 {
		t.Fatalf("track=%+v %v", track, err)
	}
	used, reserved, err := s.Quota(ctx)
	if err != nil || used != 20 || reserved != 20 {
		t.Fatalf("quota=%d %d %v", used, reserved, err)
	}
}
