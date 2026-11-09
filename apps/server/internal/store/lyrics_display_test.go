package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

func TestLyricsDisplayMigrationAndPersistence(t *testing.T) {
	ctx := testContext()
	path := filepath.Join(t.TempDir(), "lyrics.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err = goose.DownTo(s.db, ".", 6); err != nil {
		t.Fatal(err)
	}
	lyrics := "[00:01]First\n[00:03]Uno"
	_, err = s.db.ExecContext(ctx, `INSERT INTO tracks(id,owner_user_id,title,original_path,playable_path,size_bytes,duration_seconds,lyrics,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"legacy", DefaultUserID, "kept", "a", "b", 1, 10, lyrics, nowText(), nowText())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Track(ctx, "legacy")
	if err != nil || old.LyricsLayout != "timestamp" || old.LyricsPrimaryLine != "first" || old.Lyrics != lyrics {
		t.Fatalf("migration=%#v %v", old, err)
	}
	lyrics = "[offset:0]\n" + lyrics
	layout, primary := "separate", "last"
	updated, err := s.UpdateTrack(ctx, old.ID, 1, 0, nil, .8, lyrics, nil, nil, &layout, &primary)
	if err != nil || updated.LyricsLayout != layout || updated.LyricsPrimaryLine != primary {
		t.Fatalf("save=%#v %v", updated, err)
	}
	if updated.AudioVersion != old.AudioVersion {
		t.Fatal("display setting invalidated playback session")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	updated, err = s.Track(ctx, old.ID)
	if err != nil || updated.LyricsLayout != layout || updated.LyricsPrimaryLine != primary || updated.Lyrics != lyrics {
		t.Fatalf("restart=%#v %v", updated, err)
	}
	// Clients omitting new optional fields must preserve the saved choice.
	updated, err = s.UpdateTrack(ctx, old.ID, 1, 0, nil, .8, lyrics)
	if err != nil || updated.LyricsLayout != layout || updated.LyricsPrimaryLine != primary {
		t.Fatalf("legacy client=%#v %v", updated, err)
	}
	updated, err = s.ReplaceTrack(ctx, old.ID, NewTrack{Title: "replacement", OriginalPath: "c", PlayablePath: "c", SizeBytes: 1, DurationSeconds: 10}, nil)
	if err != nil || updated.LyricsLayout != layout || updated.LyricsPrimaryLine != primary || updated.Lyrics != lyrics {
		t.Fatalf("replacement=%#v %v", updated, err)
	}
	if err = s.SoftDeleteTrack(ctx, old.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	trash, err := s.Trash(ctx)
	if err != nil || len(trash) != 1 || trash[0].LyricsLayout != layout {
		t.Fatalf("trash=%#v %v", trash, err)
	}
	updated, err = s.RestoreTrack(ctx, old.ID, "restored")
	if err != nil || updated.LyricsLayout != layout || updated.LyricsPrimaryLine != primary {
		t.Fatalf("restore=%#v %v", updated, err)
	}
	playlists, err := s.Playlists(ctx)
	if err != nil || len(playlists) != 1 || len(playlists[0].Tracks) != 1 || playlists[0].Tracks[0].LyricsLayout != layout {
		t.Fatalf("playlist=%#v %v", playlists, err)
	}
}

func TestInvalidLyricsDisplayLeavesTrackUnchanged(t *testing.T) {
	ctx := testContext()
	s := openTestStore(t)
	old, err := s.InsertTrack(ctx, NewTrack{ID: "track", Title: "kept", DurationSeconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range [][2]string{{"unknown", "first"}, {"end-timed", "first"}, {"timestamp", "unknown"}, {"", "first"}} {
		if _, err = s.UpdateTrack(ctx, old.ID, 2, 1, nil, .5, "changed", nil, nil, &values[0], &values[1]); err == nil {
			t.Fatal("invalid display settings accepted")
		}
		current, err := s.Track(ctx, old.ID)
		if err != nil || current.Lyrics != old.Lyrics || current.BaseRandomWeight != old.BaseRandomWeight || current.LyricsLayout != old.LyricsLayout {
			t.Fatalf("failed update changed track: %#v %v", current, err)
		}
	}
}

func TestRemoveLyricsInferenceMigrationPreservesTextAndOtherSettings(t *testing.T) {
	ctx := testContext()
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err = goose.DownTo(s.db, ".", 7); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"retired", "independent"} {
		if _, err = s.InsertTrack(ctx, NewTrack{ID: id, Title: id, DurationSeconds: 10}); err != nil {
			t.Fatal(err)
		}
	}
	lyrics := "[00:01]First\n[00:04]Uno\n[00:04]Second"
	_, err = s.db.ExecContext(ctx, `UPDATE tracks SET lyrics=?,lyrics_primary_line='last',base_random_weight=2,volume_ratio=0.6,lyrics_layout=CASE WHEN id='retired' THEN 'end-timed' ELSE 'separate' END`, lyrics)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		s, err = Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		for id, layout := range map[string]string{"retired": "timestamp", "independent": "separate"} {
			got, err := s.Track(ctx, id)
			if err != nil || got.LyricsLayout != layout || got.Lyrics != lyrics || got.LyricsPrimaryLine != "last" || got.BaseRandomWeight != 2 || got.VolumeRatio != .6 || got.AudioVersion != 1 {
				t.Fatalf("migration/restart=%#v %v", got, err)
			}
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
