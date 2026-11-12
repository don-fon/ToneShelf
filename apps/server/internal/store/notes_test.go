package store

import (
	"github.com/pressly/goose/v3"
	"music-player/server/internal/currentuser"
	"path/filepath"
	"testing"
	"time"
)

func TestNotesMigrationPersistenceAndLifecycle(t *testing.T) {
	ctx := testContext()
	db := filepath.Join(t.TempDir(), "notes.db")
	s, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	_, err = s.InsertTrack(ctx, NewTrack{ID: "song", Title: "song", OriginalPath: "old", PlayablePath: "old", SizeBytes: 1, DurationSeconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err = goose.DownTo(s.db, ".", 17); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Track(ctx, "song")
	if err != nil || old.Note != "" {
		t.Fatalf("legacy: %+v %v", old, err)
	}
	note := "第一行\n<script>alert('text')</script> 🎵"
	updated, err := s.UpdateTrack(ctx, "song", 1, 0, nil, .8, "", nil, nil, nil, nil, nil, &note)
	if err != nil || updated.Note != note {
		t.Fatalf("save: %+v %v", updated, err)
	}
	if _, err = s.UpdateTrack(ctx, "song", 0, 0, nil, .8, "", nil, nil, nil, nil, nil, new(string)); err == nil {
		t.Fatal("invalid update accepted")
	}
	if _, err = s.UpdateTrack(currentuser.WithID(ctx, "other"), "song", 1, 0, nil, .8, "", nil, nil, nil, nil, nil, new(string)); err == nil {
		t.Fatal("foreign update accepted")
	}
	if _, err = s.UpdateTrack(ctx, "song", 1, 0, nil, .8, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReplaceTrack(ctx, "song", NewTrack{Title: "replacement", OriginalPath: "new", PlayablePath: "new", DurationSeconds: 12, SizeBytes: 2}, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.SoftDeleteTrack(ctx, "song", time.Now()); err != nil {
		t.Fatal(err)
	}
	trash, err := s.Trash(ctx)
	if err != nil || len(trash) != 1 || trash[0].Note != note {
		t.Fatalf("trash: %+v %v", trash, err)
	}
	if _, err = s.RestoreTrack(ctx, "song", "restore"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	library, err := s.Library(ctx)
	if err != nil || len(library.Tracks) != 1 || library.Tracks[0].Note != note {
		t.Fatalf("restart: %+v %v", library, err)
	}
	empty := ""
	cleared, err := s.UpdateTrack(ctx, "song", 1, 0, nil, .8, "", nil, nil, nil, nil, nil, &empty)
	if err != nil || cleared.Note != "" {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
}
