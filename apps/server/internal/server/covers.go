package server

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"music-player/server/internal/store"
	"net/http"
	"os"
	"path/filepath"
)

func (a *API) cover(w http.ResponseWriter, r *http.Request) {
	track, err := a.Store.Track(r.Context(), chi.URLParam(r, "trackID"))
	if err != nil || track.CoverPath == "" {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(track.CoverPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, "cover.jpg", info.ModTime(), file)
}

func (a *API) scheduleUserCovers(ctx context.Context) {
	tracks, err := a.Store.CoverMaintenanceTracks(ctx)
	if err != nil {
		a.logMaintenance("list missing covers", err)
		return
	}
	for _, track := range tracks {
		if ctx.Err() != nil {
			return
		}
		id, err := newID()
		if err != nil {
			a.logMaintenance("create cover ID", err)
			continue
		}
		output := filepath.Join(a.Media.Root, "media", "playable", id+".cover.jpg")
		if err = os.MkdirAll(filepath.Dir(output), 0700); err == nil {
			err = a.Store.ScheduleCover(ctx, track, id, output)
		}
		if err != nil {
			a.logMaintenance("schedule cover", err)
		}
	}
}

func (a *API) processCover(ctx context.Context, job store.UploadJob) {
	track, err := a.Store.Track(ctx, job.TargetTrackID)
	var path string
	var size int64
	if err == nil && track.OriginalPath != job.SourcePath {
		err = errors.New("audio version changed")
	}
	if err == nil {
		path, size, err = a.Media.ExtractCover(ctx, job.SourcePath, job.OutputPath, func(bytes int64) error { return a.Store.ReserveJob(ctx, job.ID, bytes, a.quotaLimit()) })
	}
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		err = a.Store.CompleteCover(ctx, job, path, size)
	}
	if err != nil {
		if stateErr := a.Store.SetJobState(ctx, job.ID, "processing", "failed", err.Error()); stateErr != nil {
			a.logMaintenance("record cover failure", stateErr)
			return
		}
		a.cleanFailedJob(ctx, job)
	}
	a.Maintain(ctx)
}
