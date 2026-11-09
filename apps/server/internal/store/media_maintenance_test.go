package store

import (
	"testing"

	"github.com/pressly/goose/v3"
)

func TestOriginalProbeCorrectsBoundsAndRejectsStaleVersion(t *testing.T) {
	for _, test := range []struct {
		name                  string
		start, end, wantStart float64
		wantEnd               *float64
	}{
		{"valid", 1, 3, 1, floatPointer(3)},
		{"end beyond duration", 1, 9, 1, floatPointer(4)},
		{"start beyond duration", 5, 9, 0, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := testContext()
			s := openTestStore(t)
			old, err := s.InsertTrack(ctx, NewTrack{ID: "aac", Title: "kept", OriginalPath: "source.aac", PlayablePath: "copy.mp3", OriginalContentType: "audio/aac", DurationSeconds: 10})
			if err != nil {
				t.Fatal(err)
			}
			old, err = s.UpdateTrack(ctx, old.ID, 2, test.start, &test.end, .6, "lyrics")
			if err != nil {
				t.Fatal(err)
			}
			if err = s.SetOriginalProbe(ctx, old, "audio/aac", 4); err != nil {
				t.Fatal(err)
			}
			got, err := s.Track(ctx, old.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.DurationSeconds != 4 || got.DurationProbeVersion != 1 || got.AudioVersion != old.AudioVersion+1 || got.PlaybackStartSeconds != test.wantStart || got.OriginalAudioURL != "" {
				t.Fatalf("corrected=%#v", got)
			}
			if (got.PlaybackEndSeconds == nil) != (test.wantEnd == nil) || (test.wantEnd != nil && *got.PlaybackEndSeconds != *test.wantEnd) {
				t.Fatalf("end=%v want=%v", got.PlaybackEndSeconds, test.wantEnd)
			}
			if got.Title != old.Title || got.BaseRandomWeight != 2 || got.VolumeRatio != .6 || got.Lyrics != "[offset:0]\nlyrics" {
				t.Fatalf("settings changed: %#v", got)
			}
			if err = s.SetOriginalProbe(ctx, old, "audio/aac", 9); err == nil {
				t.Fatal("stale probe overwrote newer audio version")
			}
			got, _ = s.Track(ctx, old.ID)
			if got.DurationSeconds != 4 {
				t.Fatal("failed probe changed duration")
			}
		})
	}
}

func floatPointer(value float64) *float64 { return &value }

func TestDurationMigrationSchedulesKnownLegacyAAC(t *testing.T) {
	ctx := testContext()
	s := openTestStore(t)
	if err := goose.DownTo(s.db, ".", 5); err != nil {
		t.Fatal(err)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO tracks(id,owner_user_id,title,original_path,playable_path,size_bytes,duration_seconds,original_content_type,playback_index_version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		"legacy-aac", DefaultUserID, "kept", "source.aac", "copy.mp3", 1, 653.718407, "audio/aac", 1, nowText(), nowText())
	if err != nil {
		t.Fatal(err)
	}
	if err = goose.Up(s.db, "."); err != nil {
		t.Fatal(err)
	}
	tracks, err := s.MediaMaintenanceTracks(ctx)
	if err != nil || len(tracks) != 1 || tracks[0].DurationProbeVersion != 0 || tracks[0].PlaybackIndexVersion != 1 {
		t.Fatalf("migration skipped existing indexed AAC: %#v %v", tracks, err)
	}
	if err = s.SetOriginalProbe(ctx, tracks[0], "audio/aac", 229.568); err != nil {
		t.Fatal(err)
	}
	tracks, err = s.MediaMaintenanceTracks(ctx)
	if err != nil || len(tracks) != 0 {
		t.Fatalf("completed correction repeated: %#v %v", tracks, err)
	}
}
