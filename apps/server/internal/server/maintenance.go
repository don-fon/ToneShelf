package server

import (
	"context"
	"errors"
	"os"
	"time"
)

// Maintain retries replacement cleanup and permanently removes trash that has
// reached its server-enforced retention boundary. It is safe to call from
// startup, periodic work, and request handlers.
func (a *API) Maintain(ctx context.Context) {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()

	jobs, jobErr := a.Store.Jobs(ctx)
	if jobErr != nil {
		a.logMaintenance("list failed uploads", jobErr)
	}
	for _, job := range jobs {
		if job.Kind == "preflight" && job.State == "queued" {
			created, parseErr := time.Parse(time.RFC3339Nano, job.CreatedAt)
			if parseErr == nil && time.Since(created) >= time.Hour {
				if stateErr := a.Store.SetJobState(ctx, job.ID, "queued", "failed", "预检查已过期"); stateErr == nil {
					job.State = "failed"
				}
			}
		}
		if job.State == "failed" && job.ReservedBytes > 0 {
			a.cleanFailedJob(ctx, job)
		}
	}
	pending, err := a.Store.PendingFiles(ctx)
	if err != nil {
		a.logMaintenance("list pending files", err)
		return
	}
	for _, file := range pending {
		if err = os.Remove(file.Path); err == nil || errors.Is(err, os.ErrNotExist) {
			if completeErr := a.Store.CompletePendingFile(ctx, file.Path); completeErr != nil {
				a.logMaintenance("complete pending file", completeErr)
			}
		} else {
			_ = a.Store.FailPendingFile(ctx, file.Path, err)
			a.logMaintenance("remove old audio", err)
		}
	}

	now := time.Now()
	candidates, err := a.Store.PurgeCandidates(ctx, now)
	if err != nil {
		a.logMaintenance("list purge candidates", err)
		return
	}
	for _, track := range candidates {
		var removeErr error
		seen := make(map[string]bool)
		for _, path := range []string{track.OriginalPath, track.PlayablePath} {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				removeErr = err
				break
			}
		}
		if removeErr != nil {
			_ = a.Store.RecordPurgeError(ctx, track.ID, removeErr)
			a.logMaintenance("purge audio", removeErr)
			continue
		}
		if err = a.Store.PurgeTrack(ctx, track.ID, now); err != nil {
			a.logMaintenance("purge track", err)
		}
	}
}

func (a *API) logMaintenance(operation string, err error) {
	if a.Log != nil {
		a.Log.Warn("maintenance failed", "operation", operation, "error", err)
	}
}
