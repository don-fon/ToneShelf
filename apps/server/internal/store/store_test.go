package store

import (
	"context"
	"path/filepath"
	"testing"
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
	if track.BaseRandomWeight != 2.5 || track.PlaybackEndSeconds == nil || *track.PlaybackEndSeconds != 9 || track.Lyrics != "歌词" {
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
