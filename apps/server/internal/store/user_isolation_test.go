package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"music-player/server/internal/currentuser"
)

func TestUserScopedResourcesAndDefaultPlaylists(t *testing.T) {
	s := openTestStore(t)
	a := testContext()
	b := currentuser.WithID(context.Background(), "user-b")
	if err := s.EnsureUser(context.Background(), "user-b", "B", "default-b"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		ctx context.Context
		id  string
	}{{a, "a"}, {b, "b"}} {
		if _, err := s.InsertTrack(item.ctx, NewTrack{ID: item.id, Title: "same title", OriginalPath: item.id, PlayablePath: item.id, SizeBytes: 10, DurationSeconds: 20}); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		ctx          context.Context
		id, playlist string
	}{{a, "a", DefaultPlaylistID}, {b, "b", "default-b"}} {
		lists, err := s.Playlists(item.ctx)
		if err != nil || len(lists) != 1 || lists[0].ID != item.playlist || len(lists[0].Tracks) != 1 || lists[0].Tracks[0].ID != item.id {
			t.Fatalf("isolated list %#v %v", lists, err)
		}
	}
	for name, action := range map[string]func() error{
		"read":                 func() error { _, err := s.Track(a, "b"); return err },
		"update":               func() error { _, err := s.UpdateTrack(a, "b", 1, 0, nil, .8, "changed"); return err },
		"replace":              func() error { _, err := s.ReplaceTrack(a, "b", NewTrack{DurationSeconds: 20}, nil); return err },
		"rename playlist":      func() error { return s.RenamePlaylist(a, "default-b", "changed") },
		"delete playlist":      func() error { return s.DeletePlaylist(a, "default-b") },
		"add foreign track":    func() error { return s.AddTrack(a, DefaultPlaylistID, "b", "cross-track") },
		"add foreign playlist": func() error { return s.AddTrack(a, "default-b", "a", "cross-list") },
		"remove":               func() error { _, err := s.RemoveTrackFromPlaylist(a, "default-b", "b", true, time.Now()); return err },
		"trash":                func() error { return s.SoftDeleteTrack(a, "b", time.Now()) },
		"purge error":          func() error { return s.RecordPurgeError(a, "b", errors.New("changed")) },
		"upload target": func() error {
			return s.CreateJob(a, UploadJob{ID: "bad", TargetTrackID: "b", SourcePath: "bad", OutputPath: "bad-output"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(); err == nil {
				t.Fatal("cross-user access accepted")
			}
		})
	}
	if _, err := s.CreatePlaylist(b, "custom-b", "B list"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTrack(b, "custom-b", "b", "b-custom"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveTrackFromPlaylist(b, "default-b", "b", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePlaylist(b, "custom-b"); err != nil {
		t.Fatal(err)
	}
	lists, err := s.Playlists(b)
	if err != nil || len(lists) != 1 || len(lists[0].Tracks) != 1 || lists[0].ID != "default-b" {
		t.Fatalf("wrong fallback %#v %v", lists, err)
	}
	if err = s.SoftDeleteTrack(b, "b", time.Now().Add(-73*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if trash, err := s.Trash(a); err != nil || len(trash) != 0 {
		t.Fatalf("foreign trash leaked %#v %v", trash, err)
	}
	if _, err = s.RestoreTrack(a, "b", "foreign-restore"); err == nil {
		t.Fatal("restored foreign track")
	}
	if err = s.PurgeTrack(a, "b", time.Now()); err == nil {
		t.Fatal("purged foreign track")
	}
	if _, err = s.RestoreTrack(b, "b", "restore-b"); err != nil {
		t.Fatal(err)
	}
	if track, err := s.Track(b, "b"); err != nil || track.Lyrics != "" || track.AudioVersion != 1 {
		t.Fatalf("foreign mutations changed track %#v %v", track, err)
	}
	if _, err := s.CreatePlaylist(b, "trash-list-b", "Trash list"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTrack(b, "trash-list-b", "b", "b-trash-list"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveTrackFromPlaylist(b, "default-b", "b", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDeleteTrack(b, "b", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePlaylist(b, "trash-list-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreTrack(b, "b", "restored-without-old-list"); err != nil {
		t.Fatal(err)
	}
	lists, err = s.Playlists(b)
	if err != nil || len(lists) != 1 || lists[0].ID != "default-b" || len(lists[0].Tracks) != 1 {
		t.Fatalf("trash fallback %#v %v", lists, err)
	}
}

func TestUserScopedJobsAndDuplicateChecks(t *testing.T) {
	s := openTestStore(t)
	a := testContext()
	b := currentuser.WithID(context.Background(), "user-b")
	if err := s.EnsureUser(context.Background(), "user-b", "B", "default-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertTrack(b, NewTrack{ID: "b", Title: "same", OriginalPath: "b", PlayablePath: "b", SizeBytes: 10, DurationSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	for _, ctx := range []context.Context{a, b} {
		id := currentuser.ID(ctx)
		if err := s.CreateJob(ctx, UploadJob{ID: id, Kind: "preflight", SourcePath: id, OutputPath: id + ".mp3"}); err != nil {
			t.Fatal(err)
		}
		if err := s.ReserveJob(ctx, id, 20, 100); err != nil {
			t.Fatal(err)
		}
		if err := s.PrepareUpload(ctx, id, "same", 20); err != nil {
			t.Fatal(err)
		}
	}
	ajobs, err := s.Jobs(a)
	if err != nil || len(ajobs) != 1 || ajobs[0].RequiresConfirmation {
		t.Fatalf("foreign duplicate match %#v %v", ajobs, err)
	}
	bjobs, err := s.Jobs(b)
	if err != nil || len(bjobs) != 1 || !bjobs[0].RequiresConfirmation || bjobs[0].TargetTrackID != "b" {
		t.Fatalf("own duplicate missing %#v %v", bjobs, err)
	}
	for name, action := range map[string]func() error{
		"reserve":           func() error { return s.ReserveJob(a, "user-b", 1, 100) },
		"reservation":       func() error { return s.SetJobReservation(a, "user-b", 0) },
		"transition":        func() error { return s.SetJobState(a, "user-b", "queued", "failed", "changed") },
		"preflight confirm": func() error { return s.StartPreparedUpload(a, "user-b", "changed") },
		"complete":          func() error { _, err := s.CompleteJob(a, bjobs[0], NewTrack{ID: "forged"}, nil); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(); err == nil {
				t.Fatal("cross-user job mutation accepted")
			}
		})
	}
	after, err := s.Jobs(b)
	if err != nil || after[0].State != "queued" || after[0].ReservedBytes != 20 {
		t.Fatalf("foreign job changed %#v %v", after, err)
	}
	if used, reserved, err := s.Quota(context.Background()); err != nil || used != 10 || reserved != 40 {
		t.Fatalf("quota must remain shared %d %d %v", used, reserved, err)
	}
}

func TestUserContextIsRequired(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.Playlists(ctx); !errors.Is(err, currentuser.ErrMissing) {
		t.Fatalf("missing user read %v", err)
	}
	if _, err := s.Jobs(ctx); !errors.Is(err, currentuser.ErrMissing) {
		t.Fatalf("missing user jobs %v", err)
	}
	if _, err := s.InsertTrack(ctx, NewTrack{ID: "bad"}); !errors.Is(err, currentuser.ErrMissing) {
		t.Fatalf("missing user insert %v", err)
	}
	if err := s.SoftDeleteTrack(ctx, "any", time.Now()); !errors.Is(err, currentuser.ErrMissing) {
		t.Fatalf("missing user delete %v", err)
	}
	if _, err := s.Track(currentuser.WithID(ctx, "unknown"), "any"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown user read %v", err)
	}
}

func TestRepairCompletionRequiresJobAndTrackToShareCurrentOwner(t *testing.T) {
	s := openTestStore(t)
	a := testContext()
	b := currentuser.WithID(context.Background(), "user-b")
	if err := s.EnsureUser(context.Background(), "user-b", "B", "default-b"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		ctx context.Context
		id  string
	}{{a, "a"}, {b, "b"}} {
		if _, err := s.InsertTrack(item.ctx, NewTrack{ID: item.id, Title: item.id, OriginalPath: item.id, PlayablePath: "shared-test-path", SizeBytes: 10, DurationSeconds: 10}); err != nil {
			t.Fatal(err)
		}
	}
	job := UploadJob{ID: "repair-b", Kind: "repair", TargetTrackID: "b", SourcePath: "shared-test-path", OutputPath: "new-path"}
	if err := s.CreateJob(b, job); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveJob(b, job.ID, 20, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.SetJobState(b, job.ID, "receiving", "processing", ""); err != nil {
		t.Fatal(err)
	}
	job.TargetTrackID = "a"
	if err := s.CompleteIndexRepair(a, job, 5, 5); err == nil {
		t.Fatal("consumed another owner's repair job")
	}
	own, err := s.Track(a, "a")
	if err != nil || own.AudioVersion != 1 {
		t.Fatalf("track changed %#v %v", own, err)
	}
	foreign, err := s.Jobs(b)
	if err != nil || foreign[0].State != "processing" || foreign[0].ReservedBytes != 20 {
		t.Fatalf("job changed %#v %v", foreign, err)
	}
	pending, err := s.PendingFiles(context.Background())
	if err != nil || len(pending) != 0 {
		t.Fatalf("cross-owner file deletion queued %#v %v", pending, err)
	}
}
