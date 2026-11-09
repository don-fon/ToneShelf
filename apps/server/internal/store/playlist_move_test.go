package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"music-player/server/internal/currentuser"
)

func TestMoveTrackRelationsAndRestart(t *testing.T) {
	for _, mode := range []string{"from-default", "to-default", "ordinary", "already-in-target"} {
		t.Run(mode, func(t *testing.T) {
			ctx := testContext()
			path := filepath.Join(t.TempDir(), "test.db")
			s, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close() }()
			for _, id := range []string{"source", "target", "other"} {
				if _, err = s.CreatePlaylist(ctx, id, id); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = s.InsertTrack(ctx, NewTrack{ID: "song", Title: "song", OriginalPath: "original", PlayablePath: "playable", SizeBytes: 10, DurationSeconds: 20}); err != nil {
				t.Fatal(err)
			}
			if _, err = s.UpdateTrack(ctx, "song", 4, 1, nil, .5, "lyrics"); err != nil {
				t.Fatal(err)
			}
			source, target := "source", "target"
			if mode == "from-default" {
				source = DefaultPlaylistID
			}
			if mode == "to-default" {
				target = DefaultPlaylistID
			}
			if source != DefaultPlaylistID {
				if err = s.AddTrack(ctx, source, "song", "source-item"); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.AddTrack(ctx, "other", "song", "other-item"); err != nil {
				t.Fatal(err)
			}
			if mode == "already-in-target" {
				if err = s.AddTrack(ctx, target, "song", "target-existing"); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.MoveTrack(ctx, source, target, "song", "moved"); err != nil {
				t.Fatal(err)
			}
			if err = s.MoveTrack(ctx, source, target, "song", "retry"); err != nil {
				t.Fatalf("retry was not idempotent %v", err)
			}
			s.Close()
			s, err = Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			lists, err := s.Playlists(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, list := range lists {
				count := 0
				for _, song := range list.Tracks {
					if song.ID == "song" {
						count++
					}
				}
				want := 0
				if list.ID == target || list.ID == "other" || (list.ID == DefaultPlaylistID && source != DefaultPlaylistID) {
					want = 1
				}
				if count != want {
					t.Fatalf("mode=%s playlist=%s count=%d want=%d", mode, list.ID, count, want)
				}
			}
			song, err := s.Track(ctx, "song")
			if err != nil || song.BaseRandomWeight != 4 || song.VolumeRatio != .5 || song.PlaybackStartSeconds != 1 || song.AudioVersion != 1 || song.Lyrics != "[offset:0]\nlyrics" {
				t.Fatalf("song changed %#v %v", song, err)
			}
			trash, err := s.Trash(ctx)
			if err != nil || len(trash) != 0 {
				t.Fatalf("move trashed song %#v %v", trash, err)
			}
		})
	}
}

func TestMoveTrackFailureRollsBackDestination(t *testing.T) {
	s := openTestStore(t)
	ctx := testContext()
	if _, err := s.CreatePlaylist(ctx, "target", "target"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertTrack(ctx, NewTrack{ID: "song", Title: "song", OriginalPath: "original", PlayablePath: "playable", SizeBytes: 10, DurationSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	// Fault injection in the temporary test DB: fail after the target was added.
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER fail_source_remove BEFORE DELETE ON playlist_items BEGIN SELECT RAISE(ABORT,'source remove failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveTrack(ctx, DefaultPlaylistID, "target", "song", "moved"); err == nil {
		t.Fatal("move succeeded despite source failure")
	}
	lists, err := s.Playlists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, list := range lists {
		want := 0
		if list.ID == DefaultPlaylistID {
			want = 1
		}
		if len(list.Tracks) != want {
			t.Fatalf("partial move committed %#v", list)
		}
	}
}

func TestMoveTrackRejectsInvalidOrForeignResources(t *testing.T) {
	s := openTestStore(t)
	ctx := testContext()
	b := currentuser.WithID(context.Background(), "user-b")
	if err := s.EnsureUser(context.Background(), "user-b", "B", "default-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePlaylist(ctx, "target", "target"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		ctx context.Context
		id  string
	}{{ctx, "song"}, {b, "foreign"}} {
		if _, err := s.InsertTrack(item.ctx, NewTrack{ID: item.id, Title: item.id, OriginalPath: item.id, PlayablePath: item.id, SizeBytes: 1, DurationSeconds: 10}); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct{ source, target, song string }{{DefaultPlaylistID, DefaultPlaylistID, "song"}, {DefaultPlaylistID, "missing", "song"}, {DefaultPlaylistID, "default-b", "song"}, {"default-b", "target", "foreign"}, {DefaultPlaylistID, "target", "foreign"}, {"target", DefaultPlaylistID, "absent"}} {
		if err := s.MoveTrack(ctx, item.source, item.target, item.song, "bad-move"); err == nil {
			t.Fatalf("invalid move accepted %#v", item)
		}
	}
	if err := s.MoveTrack(context.Background(), DefaultPlaylistID, "target", "song", "no-user"); !errors.Is(err, currentuser.ErrMissing) {
		t.Fatalf("missing user %v", err)
	}
	lists, err := s.Playlists(ctx)
	if err != nil || len(lists[0].Tracks) != 1 || len(lists[1].Tracks) != 0 {
		t.Fatalf("invalid move changed source %#v %v", lists, err)
	}
}
