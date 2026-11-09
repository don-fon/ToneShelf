package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"music-player/server/internal/artwork"
	"music-player/server/internal/store"
)

func (a *API) searchCovers(w http.ResponseWriter, r *http.Request) {
	track, err := a.Store.Track(r.Context(), chi.URLParam(r, "trackID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if a.Artwork == nil {
		writeError(w, 503, errors.New("封面服务未启用"))
		return
	}
	var input artwork.Input
	if err = decodeJSON(r, &input); err != nil {
		writeError(w, 400, err)
		return
	}
	if len(input.Title) > 2000 || len(input.Artist) > 500 || len(input.Album) > 500 {
		writeError(w, 400, errors.New("搜索内容过长"))
		return
	}
	input.DurationSeconds = track.DurationSeconds
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	result, err := a.Artwork.Search(ctx, input)
	if err != nil {
		writeError(w, 502, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, result)
}
func (a *API) previewCover(w http.ResponseWriter, r *http.Request) {
	if _, err := a.Store.Track(r.Context(), chi.URLParam(r, "trackID")); err != nil {
		http.NotFound(w, r)
		return
	}
	id := chi.URLParam(r, "releaseID")
	if !artwork.ValidID(id) {
		writeError(w, 400, errors.New("无效的发行版本 ID"))
		return
	}
	if a.Artwork == nil {
		writeError(w, 503, errors.New("封面服务未启用"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	var data bytes.Buffer
	if err := a.Artwork.Download(ctx, id, &data); err != nil {
		writeError(w, 502, err)
		return
	}
	mime := http.DetectContentType(data.Bytes())
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp":
	default:
		writeError(w, 502, errors.New("封面不是受支持的图片"))
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Write(data.Bytes())
}
func (a *API) selectCover(w http.ResponseWriter, r *http.Request) {
	track, err := a.Store.Track(r.Context(), chi.URLParam(r, "trackID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if a.Artwork == nil || a.Media == nil {
		writeError(w, 503, errors.New("封面服务未启用"))
		return
	}
	var input struct {
		ReleaseID string `json:"releaseId"`
	}
	if err = decodeJSON(r, &input); err != nil || !artwork.ValidID(input.ReleaseID) {
		writeError(w, 400, errors.New("无效的发行版本 ID"))
		return
	}
	id, err := newID()
	if err != nil {
		a.fail(w, err)
		return
	}
	output := filepath.Join(a.Media.Root, "media", "playable", id+".cover.jpg")
	if err = os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		a.fail(w, err)
		return
	}
	job := store.UploadJob{ID: id, Kind: "cover", CoverReleaseID: input.ReleaseID, TargetTrackID: track.ID, FileName: track.FileName, SourcePath: track.OriginalPath, OutputPath: output}
	if err = a.Store.CreateJob(r.Context(), job); err == nil {
		err = a.Store.SetJobState(r.Context(), id, "receiving", "queued", "")
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	job.State = "queued"
	writeJSON(w, 202, job)
}
func (a *API) processOnlineCover(ctx context.Context, job store.UploadJob) {
	track, err := a.Store.Track(ctx, job.TargetTrackID)
	var old store.PendingFile
	var size int64
	if err == nil && track.OriginalPath != job.SourcePath {
		err = errors.New("音频版本已改变，请重新选择封面")
	}
	if err == nil && a.Artwork == nil {
		err = errors.New("封面服务未启用")
	}
	if err == nil && track.CoverPath != "" {
		old.Path = track.CoverPath
		info, statErr := os.Stat(old.Path)
		if statErr != nil {
			err = statErr
		} else {
			old.SizeBytes = info.Size()
		}
	}
	if err == nil {
		size, err = a.Media.ImportCover(ctx, job.OutputPath, func(writer io.Writer) error { return a.Artwork.Download(ctx, job.CoverReleaseID, writer) }, func(n int64) error { return a.Store.ReserveJob(ctx, job.ID, n, a.quotaLimit()) })
	}
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		err = a.Store.CompleteOnlineCover(ctx, job, old, size)
	}
	if err != nil {
		if stateErr := a.Store.SetJobState(ctx, job.ID, "processing", "failed", err.Error()); stateErr != nil {
			a.logMaintenance("record online cover failure", stateErr)
			return
		}
		a.cleanFailedJob(ctx, job)
	}
	a.Maintain(ctx)
}
