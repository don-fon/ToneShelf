package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestDisplayMigrationBackfillsLegacyTracks(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = goose.DownTo(s.db, ".", 2); err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO tracks(id,owner_user_id,title,original_path,playable_path,size_bytes,duration_seconds,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		"legacy", DefaultUserID, "历史名称", "a", "a", 1, 10, "2026-11-01T00:00:00Z", "2026-11-01T00:00:00Z")
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	track, err := s.Track(ctx, "legacy")
	if err != nil || track.FileName != "历史名称" || track.UploadedAt != "2026-11-01T00:00:00Z" {
		t.Fatalf("migrated=%#v err=%v", track, err)
	}
}

func TestEditableIdentityPersistsAcrossReplacementAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "identity.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	original, err := s.InsertTrack(ctx, NewTrack{ID: "editable", Title: "embedded title", Artist: "embedded artist", FileName: "source.aac", OriginalPath: "a", PlayablePath: "b", SizeBytes: 1, DurationSeconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	title, artist := " 我的歌 ", " 我的歌手 "
	updated, err := s.UpdateTrack(ctx, original.ID, 1, 0, nil, .8, "lyrics", &title, &artist)
	if err != nil || updated.Title != "我的歌" || updated.Artist != "我的歌手" {
		t.Fatalf("identity=%#v err=%v", updated, err)
	}
	blank := "  "
	if _, err = s.UpdateTrack(ctx, original.ID, 1, 0, nil, .8, "", &blank, nil); err == nil {
		t.Fatal("blank title accepted")
	}
	replaced, err := s.ReplaceTrack(ctx, original.ID, NewTrack{Title: "new embedded title", Artist: "new embedded artist", FileName: "replacement.mp3", OriginalPath: "c", PlayablePath: "c", SizeBytes: 2, DurationSeconds: 12}, nil)
	if err != nil || replaced.Title != "我的歌" || replaced.Artist != "我的歌手" || replaced.FileName != "replacement.mp3" || replaced.UploadedAt != original.UploadedAt {
		t.Fatalf("replacement=%#v err=%v", replaced, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.Track(ctx, original.ID)
	if err != nil || reloaded.Title != replaced.Title || reloaded.Artist != replaced.Artist || reloaded.UploadedAt != original.UploadedAt {
		t.Fatalf("reloaded=%#v err=%v", reloaded, err)
	}
	artist = ""
	cleared, err := s.UpdateTrack(ctx, original.ID, 1, 0, nil, .8, "lyrics", nil, &artist)
	if err != nil || cleared.Artist != "" {
		t.Fatalf("clear artist=%#v err=%v", cleared, err)
	}
}

func TestPlaylistLifecycleAndDefaultRemovalConfirmation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	for _, id := range []string{"track-1", "track-2"} {
		if _, err := s.InsertTrack(ctx, NewTrack{ID: id, Title: id, OriginalPath: id + ".source", PlayablePath: id + ".mp3", ContentType: "audio/mpeg", SizeBytes: 1, DurationSeconds: 10}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreatePlaylist(ctx, "playlist-1", "旧名称"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenamePlaylist(ctx, "playlist-1", "新名称"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTrack(ctx, "playlist-1", "track-1", "item-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTrack(ctx, "playlist-1", "track-2", "item-2"); err != nil {
		t.Fatal(err)
	}
	playlists, err := s.Playlists(ctx)
	if err != nil || playlists[1].Name != "新名称" || playlists[1].Tracks[0].ID != "track-1" {
		t.Fatalf("unexpected playlist state: %#v, %v", playlists, err)
	}
	if _, err = s.RemoveTrackFromPlaylist(ctx, DefaultPlaylistID, "track-1", false, time.Now()); err != nil {
		t.Fatal("track with another membership should leave the default playlist without confirmation")
	}
	if _, err = s.RemoveTrackFromPlaylist(ctx, "playlist-1", "track-2", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RemoveTrackFromPlaylist(ctx, DefaultPlaylistID, "track-2", false, time.Now()); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("expected confirmation requirement, got %v", err)
	}
	deletedAt := time.Date(2026, 11, 8, 1, 2, 3, 0, time.UTC)
	result, err := s.RemoveTrackFromPlaylist(ctx, DefaultPlaylistID, "track-2", true, deletedAt)
	if err != nil || !result.TrackDeleted {
		t.Fatalf("confirmed removal failed: %#v, %v", result, err)
	}
	trash, err := s.Trash(ctx)
	if err != nil || len(trash) != 1 || trash[0].PurgeEligibleAt == nil || *trash[0].PurgeEligibleAt != deletedAt.Add(72*time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("unexpected trash: %#v, %v", trash, err)
	}
	if err = s.DeletePlaylist(ctx, "playlist-1"); err != nil {
		t.Fatal(err)
	}
	playlists, err = s.Playlists(ctx)
	if err != nil || len(playlists) != 1 || len(playlists[0].Tracks) != 1 || playlists[0].Tracks[0].ID != "track-1" {
		t.Fatalf("deleting playlist must preserve active tracks: %#v, %v", playlists, err)
	}
}

func TestTrashRestoreAndPurgeBoundary(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.InsertTrack(ctx, NewTrack{ID: "track-1", Title: "test", OriginalPath: "a", PlayablePath: "b", ContentType: "audio/mpeg", SizeBytes: 2, DurationSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePlaylist(ctx, "playlist-1", "收藏"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTrack(ctx, "playlist-1", "track-1", "item-1"); err != nil {
		t.Fatal(err)
	}
	deletedAt := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	if err := s.SoftDeleteTrack(ctx, "track-1", deletedAt); err != nil {
		t.Fatal(err)
	}
	if candidates, err := s.PurgeCandidates(ctx, deletedAt.Add(72*time.Hour-time.Nanosecond)); err != nil || len(candidates) != 0 {
		t.Fatalf("track became purgeable too early: %#v, %v", candidates, err)
	}
	if candidates, err := s.PurgeCandidates(ctx, deletedAt.Add(72*time.Hour)); err != nil || len(candidates) != 1 {
		t.Fatalf("track not purgeable at boundary: %#v, %v", candidates, err)
	}
	restored, err := s.RestoreTrack(ctx, "track-1", "fallback-item")
	if err != nil || restored.DeletedAt != nil {
		t.Fatalf("restore failed: %#v, %v", restored, err)
	}
	playlists, _ := s.Playlists(ctx)
	if len(playlists[0].Tracks) != 1 || len(playlists[1].Tracks) != 1 {
		t.Fatalf("restore did not preserve memberships: %#v", playlists)
	}
}

func TestReplaceTrackPreservesSettingsAndQueuesOldFiles(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	track, err := s.InsertTrack(ctx, NewTrack{ID: "track-1", Title: "old", OriginalPath: "old.src", PlayablePath: "old.mp3", ContentType: "audio/mpeg", SizeBytes: 12, DurationSeconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	end := 9.0
	if track, err = s.UpdateTrack(ctx, track.ID, 2.5, 8, &end, .6, "lyrics"); err != nil {
		t.Fatal(err)
	}
	replaced, err := s.ReplaceTrack(ctx, track.ID, NewTrack{Title: "new", Artist: "artist", OriginalPath: "new.src", PlayablePath: "new.mp3", ContentType: "audio/mpeg", SizeBytes: 20, DurationSeconds: 5}, []PendingFile{{Path: "old.src", SizeBytes: 5}, {Path: "old.mp3", SizeBytes: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.ID != track.ID || replaced.Title != "new" || replaced.AudioVersion != track.AudioVersion+1 || replaced.BaseRandomWeight != 2.5 || replaced.VolumeRatio != .6 || replaced.Lyrics != "[offset:0]\nlyrics" || replaced.PlaybackStartSeconds != 0 || replaced.PlaybackEndSeconds != nil {
		t.Fatalf("replacement lost track state: %#v", replaced)
	}
	pending, err := s.PendingFiles(ctx)
	if err != nil || len(pending) != 2 {
		t.Fatalf("old files not queued: %#v, %v", pending, err)
	}
	used, err := s.Usage(ctx)
	if err != nil || used != 32 {
		t.Fatalf("replacement quota must include pending files, used=%d err=%v", used, err)
	}
}

func TestDefaultPlaylistAndTrackSettings(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	playlists, err := s.Playlists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(playlists) != 1 || !playlists[0].IsDefault {
		t.Fatalf("unexpected defaults: %#v", playlists)
	}
	track, err := s.InsertTrack(ctx, NewTrack{ID: "track-1", Title: "测试音乐", OriginalPath: "original.mp3", PlayablePath: "playable.mp3", ContentType: "audio/mpeg", SizeBytes: 42, DurationSeconds: 12})
	if err != nil {
		t.Fatal(err)
	}
	if track.BaseRandomWeight != 1 || track.VolumeRatio != .8 {
		t.Fatalf("unexpected defaults: %#v", track)
	}
	end := 9.0
	track, err = s.UpdateTrack(ctx, track.ID, 2.5, 1, &end, .65, "歌词")
	if err != nil {
		t.Fatal(err)
	}
	if track.BaseRandomWeight != 2.5 || track.PlaybackEndSeconds == nil || *track.PlaybackEndSeconds != 9 || track.Lyrics != "[offset:0]\n歌词" {
		t.Fatalf("update failed: %#v", track)
	}
	if _, err = s.UpdateTrack(ctx, track.ID, 0, 0, nil, .8, ""); err == nil {
		t.Fatal("expected invalid weight error")
	}
	playlists, _ = s.Playlists(ctx)
	if len(playlists[0].Tracks) != 1 || playlists[0].Tracks[0].ID != track.ID {
		t.Fatalf("track not added to default playlist: %#v", playlists)
	}
	used, err := s.Usage(ctx)
	if err != nil || used != 42 {
		t.Fatalf("unexpected usage %d: %v", used, err)
	}
}

func TestCreatePlaylistAndPreventDuplicateTrack(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	_, err := s.InsertTrack(ctx, NewTrack{ID: "track-1", Title: "测试音乐", OriginalPath: "a", PlayablePath: "b", ContentType: "audio/mpeg", SizeBytes: 1, DurationSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreatePlaylist(ctx, "playlist-1", "通勤"); err != nil {
		t.Fatal(err)
	}
	if err = s.AddTrack(ctx, "playlist-1", "track-1", "item-1"); err != nil {
		t.Fatal(err)
	}
	if err = s.AddTrack(ctx, "playlist-1", "track-1", "item-2"); err == nil {
		t.Fatal("expected duplicate error")
	}
	playlists, err := s.Playlists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(playlists) != 2 || len(playlists[1].Tracks) != 1 {
		t.Fatalf("unexpected playlists: %#v", playlists)
	}
}
