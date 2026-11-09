package server

import (
	"context"
	"os"
	"path/filepath"

	"music-player/server/internal/currentuser"
	"music-player/server/internal/store"
)

func (a *API) ScheduleMediaMaintenance(ctx context.Context) {
	if a.Media == nil {
		return
	}
	users, err := a.Store.UserIDs(ctx)
	if err != nil {
		a.logMaintenance("list media owners", err)
		return
	}
	for _, id := range users {
		a.scheduleUserMediaMaintenance(currentuser.WithID(ctx, id))
	}
}

func (a *API) scheduleUserMediaMaintenance(ctx context.Context) {
	tracks, err := a.Store.MediaMaintenanceTracks(ctx)
	if err != nil {
		a.logMaintenance("list media maintenance", err)
		return
	}
	for _, track := range tracks {
		if ctx.Err() != nil {
			return
		}
		if track.OriginalContentType == "" || (track.OriginalContentType == "audio/aac" && track.DurationProbeVersion < 1) {
			mime, duration, probeErr := a.Media.InspectOriginal(ctx, track.OriginalPath)
			if probeErr != nil {
				a.logMaintenance("probe original audio", probeErr)
				continue
			}
			if err = a.Store.SetOriginalProbe(ctx, track, mime, duration); err != nil {
				a.logMaintenance("save original format", err)
				continue
			}
		}
		if track.OriginalPath != track.PlayablePath && track.PlaybackIndexVersion < 1 {
			id, idErr := newID()
			if idErr != nil {
				a.logMaintenance("create index repair ID", idErr)
				continue
			}
			output := filepath.Join(a.Media.Root, "media", "playable", id+".mp3")
			if err = os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
				a.logMaintenance("create repair directory", err)
				continue
			}
			if err = a.Store.ScheduleIndexRepair(ctx, track, id, output); err != nil {
				a.logMaintenance("schedule MP3 index repair", err)
			}
		}
	}
}

func (a *API) processRepair(ctx context.Context, job store.UploadJob) {
	track, err := a.Store.Track(ctx, job.TargetTrackID)
	var oldSize, newSize int64
	if err == nil {
		info, statErr := os.Stat(job.SourcePath)
		err = statErr
		if err == nil {
			oldSize = info.Size()
		}
	}
	if err == nil {
		newSize, err = a.Media.RemuxIndexed(ctx, job.SourcePath, job.OutputPath, track.DurationSeconds, func(bytes int64) error { return a.Store.ReserveJob(ctx, job.ID, bytes, a.quotaLimit()) })
	}
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		err = a.Store.CompleteIndexRepair(ctx, job, oldSize, newSize)
	}
	if err != nil {
		if stateErr := a.Store.SetJobState(ctx, job.ID, "processing", "failed", err.Error()); stateErr != nil {
			a.logMaintenance("record index repair failure", stateErr)
			return
		}
		a.cleanFailedJob(ctx, job)
	}
	a.Maintain(ctx)
}
