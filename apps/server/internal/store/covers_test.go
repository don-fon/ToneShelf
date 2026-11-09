package store

import (
	"music-player/server/internal/currentuser"
	"path/filepath"
	"testing"
)

func TestLegacyCoverBackfillRestartQuotaAndReplacementRace(t *testing.T) {
	ctx := testContext()
	db := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	track, err := s.InsertTrack(ctx, NewTrack{ID: "song", Title: "song", OriginalPath: "original", PlayablePath: "original", SizeBytes: 10, DurationSeconds: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE tracks SET cover_scan_version=0 WHERE id='song'`); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.CoverMaintenanceTracks(ctx)
	if err != nil || len(legacy) != 1 {
		t.Fatalf("legacy=%v %v", legacy, err)
	}
	if err = s.ScheduleCover(ctx, track, "cover-job", "cover.jpg"); err != nil {
		t.Fatal(err)
	}
	if err = s.ScheduleCover(ctx, track, "duplicate", "duplicate.jpg"); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.Jobs(ctx)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%v %v", jobs, err)
	}
	if err = s.SetJobState(ctx, "cover-job", "queued", "processing", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveJob(ctx, "cover-job", 5, 100); err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteCover(currentuser.WithID(ctx, "other"), jobs[0], "cover.jpg", 5); err == nil {
		t.Fatal("foreign owner attached cover")
	}
	if err = s.CompleteCover(ctx, jobs[0], "cover.jpg", 6); err == nil {
		t.Fatal("unreserved cover accepted")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteCover(ctx, jobs[0], "cover.jpg", 5); err != nil {
		t.Fatal(err)
	}
	got, err := s.Track(ctx, track.ID)
	if err != nil || got.SizeBytes != 15 || got.CoverURL != "/api/tracks/song/cover?v=1&c=1" || got.AudioVersion != track.AudioVersion {
		t.Fatalf("backfill=%+v %v", got, err)
	}
	used, reserved, err := s.Quota(ctx)
	if err != nil || used != 15 || reserved != 0 {
		t.Fatalf("quota=%d %d %v", used, reserved, err)
	}
	known, err := s.KnownMediaPaths(ctx)
	if err != nil || !known["cover.jpg"] {
		t.Fatalf("known=%v %v", known, err)
	}
	old := []PendingFile{{Path: "original", SizeBytes: 10}, {Path: "cover.jpg", SizeBytes: 5}}
	got, err = s.ReplaceTrack(ctx, track.ID, NewTrack{OriginalPath: "new", PlayablePath: "new", DurationSeconds: 2, SizeBytes: 8}, old)
	if err != nil || got.CoverURL != "" || got.CoverPath != "" {
		t.Fatalf("replace=%+v %v", got, err)
	}
	if err = s.CompleteCover(ctx, jobs[0], "stale.jpg", 5); err == nil {
		t.Fatal("stale completion changed replaced song")
	}
	pending, err := s.PendingFiles(ctx)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending=%v %v", pending, err)
	}
	legacy, err = s.CoverMaintenanceTracks(ctx)
	if err != nil || len(legacy) != 0 {
		t.Fatalf("scanned again=%v %v", legacy, err)
	}
}
