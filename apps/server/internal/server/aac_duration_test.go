package server

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"music-player/server/internal/store"
)

func TestLegacyAACDurationCorrectedAcrossRestart(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		name := "unindexed"
		if indexed {
			name = "already indexed"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			api, old, db := legacyAudioFixture(t, "aac")
			// Simulate the old bitrate-derived duration and an already known MIME.
			index := 0
			if indexed {
				index = 1
			}
			var err error
			old, err = api.Store.ReplaceTrack(ctx, old.ID, store.NewTrack{
				Title: old.Title, Artist: old.Artist, OriginalPath: old.OriginalPath, PlayablePath: old.PlayablePath,
				ContentType: old.ContentType, OriginalContentType: "audio/aac", PlaybackIndexVersion: index,
				SizeBytes: old.SizeBytes, DurationSeconds: 653.718407,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !indexed {
				if err = api.Store.ScheduleIndexRepair(ctx, old, "previous-failure", filepath.Join(api.Media.Root, "media", "playable", "previous-failure.mp3")); err != nil {
					t.Fatal(err)
				}
				if err = api.Store.SetJobState(ctx, "previous-failure", "queued", "processing", ""); err != nil {
					t.Fatal(err)
				}
				if err = api.Store.SetJobState(ctx, "previous-failure", "processing", "failed", "MP3 时间索引校验失败"); err != nil {
					t.Fatal(err)
				}
			}
			original, err := os.ReadFile(old.OriginalPath)
			if err != nil {
				t.Fatal(err)
			}
			if err = api.Store.Close(); err != nil {
				t.Fatal(err)
			}
			api.Store, err = store.Open(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			if err = api.RecoverJobs(ctx); err != nil {
				t.Fatal(err)
			}
			fixed, err := api.Store.Track(ctx, old.ID)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(fixed.DurationSeconds-4) > .05 || fixed.DurationProbeVersion != 1 || fixed.AudioVersion != old.AudioVersion+1 || fixed.OriginalAudioURL != "" {
				t.Fatalf("duration correction=%#v", fixed)
			}
			if fixed.Lyrics != old.Lyrics || fixed.BaseRandomWeight != old.BaseRandomWeight || fixed.VolumeRatio != old.VolumeRatio || fixed.PlaybackStartSeconds != 1 || fixed.PlaybackEndSeconds == nil || *fixed.PlaybackEndSeconds != 3 {
				t.Fatalf("settings changed: %#v", fixed)
			}
			jobs, _ := api.Store.Jobs(ctx)
			if indexed {
				if len(jobs) != 0 {
					t.Fatalf("indexed copy unnecessarily repaired: %#v", jobs)
				}
			} else {
				if len(jobs) != 2 {
					t.Fatalf("failed repair was not retried: %#v", jobs)
				}
				var queued string
				for _, job := range jobs {
					if job.State == "queued" {
						queued = job.ID
					}
				}
				if queued == "" {
					t.Fatalf("no retry queued: %#v", jobs)
				}
				finishTestJob(t, api, queued, "succeeded")
			}
			beforeRestart, _ := api.Store.Track(ctx, old.ID)
			if err = api.Store.Close(); err != nil {
				t.Fatal(err)
			}
			api.Store, err = store.Open(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			if err = api.RecoverJobs(ctx); err != nil {
				t.Fatal(err)
			}
			afterRestart, _ := api.Store.Track(ctx, old.ID)
			if afterRestart.AudioVersion != beforeRestart.AudioVersion || afterRestart.DurationSeconds != fixed.DurationSeconds {
				t.Fatal("correction repeated on restart")
			}
			after, err := os.ReadFile(old.OriginalPath)
			if err != nil || string(original) != string(after) {
				t.Fatalf("original changed: %v", err)
			}
		})
	}
}

func TestAACProbeFailureLeavesRecordRetryable(t *testing.T) {
	ctx := context.Background()
	api, old, _ := legacyAudioFixture(t, "aac")
	api.Media.FFmpeg = "missing-ffmpeg-executable"
	if err := api.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := api.Store.Track(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.DurationProbeVersion != 0 || current.AudioVersion != old.AudioVersion || current.DurationSeconds != old.DurationSeconds {
		t.Fatal("failed probe activated inaccurate metadata")
	}
	jobs, _ := api.Store.Jobs(ctx)
	if len(jobs) != 0 {
		t.Fatal("repair scheduled without accurate source duration")
	}
	api.Media.FFmpeg = "ffmpeg"
	api.ScheduleMediaMaintenance(ctx)
	current, err = api.Store.Track(ctx, old.ID)
	if err != nil || current.DurationProbeVersion != 1 {
		t.Fatalf("retry failed: %#v %v", current, err)
	}
}
