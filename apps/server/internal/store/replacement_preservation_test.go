package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplacementRetainsCoverAndDiscardsCandidateAcrossRestart(t *testing.T) {
	ctx := testContext()
	root := t.TempDir()
	db := filepath.Join(root, "test.db")
	s, err := Open(ctx, db, WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err = os.MkdirAll(filepath.Join(root, "media"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"old.jpg": "cover", "new.jpg": "new"} {
		if err = os.WriteFile(filepath.Join(root, "media", name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	old, err := s.InsertTrack(ctx, NewTrack{ID: "song", Title: "original title", OriginalPath: "media/old.mp3", PlayablePath: "media/old.mp3", CoverPath: "media/old.jpg", SizeBytes: 15, DurationSeconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	files := []PendingFile{{Path: old.OriginalPath, SizeBytes: 10}, {Path: old.CoverPath, SizeBytes: 5}}
	candidate := NewTrack{Title: "new embedded title", OriginalPath: "media/new.mp3", PlayablePath: "media/new.mp3", CoverPath: "media/missing.jpg", SizeBytes: 23, DurationSeconds: 12}
	if _, err = s.ReplaceTrack(ctx, old.ID, candidate, files); err == nil {
		t.Fatal("missing candidate cover accepted")
	}
	unchanged, err := s.Track(ctx, old.ID)
	if err != nil || unchanged.AudioVersion != 1 || unchanged.CoverPath != old.CoverPath {
		t.Fatalf("failed replacement changed song: %+v %v", unchanged, err)
	}
	pending, err := s.PendingFiles(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("failed transaction leaked cleanup: %+v %v", pending, err)
	}
	candidate.CoverPath = "media/new.jpg"
	got, err := s.ReplaceTrack(ctx, old.ID, candidate, files)
	if err != nil || got.Title != old.Title || got.CoverPath != old.CoverPath || got.SizeBytes != 25 {
		t.Fatalf("replacement: %+v %v", got, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, db, WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.Track(ctx, old.ID)
	if err != nil || got.Title != old.Title || got.CoverPath != old.CoverPath || got.SizeBytes != 25 {
		t.Fatalf("restart: %+v %v", got, err)
	}
	pending, err = s.PendingFiles(ctx)
	if err != nil || len(pending) != 2 {
		t.Fatalf("cleanup: %+v %v", pending, err)
	}
	for _, file := range pending {
		if file.Path == old.CoverPath {
			t.Fatal("retained cover queued for deletion")
		}
	}
	used, err := s.Usage(ctx)
	if err != nil || used != 38 {
		t.Fatalf("quota: %d %v", used, err)
	}
}
