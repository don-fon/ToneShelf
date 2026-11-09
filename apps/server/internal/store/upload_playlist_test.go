package store

import (
	"path/filepath"
	"testing"
)

func TestUploadPlaylistActivationAndRestart(t *testing.T) {
	for _, mode := range []string{"ordinary", "default", "legacy", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			ctx := testContext()
			path := filepath.Join(t.TempDir(), "test.db")
			s, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close() }()
			if _, err = s.CreatePlaylist(ctx, "a", "A"); err != nil {
				t.Fatal(err)
			}
			playlistID := "a"
			if mode == "default" {
				playlistID = DefaultPlaylistID
			}
			if mode == "legacy" {
				playlistID = ""
			}
			job, candidate := prepareConflictJob(t, s, "new", "new song", playlistID)
			// Invalid activation must not partially create either relationship.
			invalid := candidate
			invalid.DurationSeconds = 0
			if _, err = s.CompleteJob(ctx, job, invalid, nil); err == nil {
				t.Fatal("invalid activation accepted")
			}
			if mode == "deleted" {
				if err = s.DeletePlaylist(ctx, "a"); err != nil {
					t.Fatal(err)
				}
			}
			s.Close()
			s, err = Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			jobs, err := s.Jobs(ctx)
			if err != nil || jobs[0].PlaylistID != playlistID {
				t.Fatalf("lost destination %#v %v", jobs, err)
			}
			if _, err = s.CompleteJob(ctx, jobs[0], candidate, nil); err != nil {
				t.Fatal(err)
			}
			playlists, err := s.Playlists(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, playlist := range playlists {
				want := 0
				if playlist.ID == DefaultPlaylistID || (mode == "ordinary" && playlist.ID == "a") {
					want = 1
				}
				if len(playlist.Tracks) != want {
					t.Fatalf("mode=%s playlist=%#v want=%d", mode, playlist, want)
				}
			}
			if _, err = s.CompleteJob(ctx, jobs[0], candidate, nil); err == nil {
				t.Fatal("activated twice")
			}
		})
	}
}

func TestUploadPlaylistOwnership(t *testing.T) {
	ctx := testContext()
	s := openTestStore(t)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO users(id,display_name,created_at) VALUES('other','Other',?)`, nowText()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO playlists(id,owner_user_id,name,is_default,created_at,updated_at) VALUES('foreign','other','Foreign',0,?,?)`, nowText(), nowText()); err != nil {
		t.Fatal(err)
	}
	exists, err := s.PlaylistExists(ctx, "foreign")
	if err != nil || exists {
		t.Fatalf("foreign visible %v %v", exists, err)
	}
	if err = s.CreateJob(ctx, UploadJob{ID: "new", PlaylistID: "foreign", FileName: "new", SourcePath: "source", OutputPath: "output"}); err == nil {
		t.Fatal("accepted foreign upload playlist")
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM playlist_items WHERE playlist_id='foreign'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cross-owner relationship %d %v", count, err)
	}
}
