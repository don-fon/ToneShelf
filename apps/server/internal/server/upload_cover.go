package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

func (a *API) uploadCover(w http.ResponseWriter, r *http.Request) {
	track, err := a.Store.Track(r.Context(), chi.URLParam(r, "trackID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if a.Media == nil {
		writeError(w, 503, errors.New("封面处理服务未启用"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxCoverBytes+64*1024)
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, 400, errors.New("请选择封面图片上传"))
		return
	}
	part, err := filePart(reader)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	defer part.Close()
	id, err := newID()
	if err != nil {
		a.fail(w, err)
		return
	}
	output := filepath.Join(a.Media.Root, "media", "playable", id+".cover.jpg")
	input := filepath.Join(a.Media.Root, "media", "staging", id+".image")
	for _, dir := range []string{filepath.Dir(input), filepath.Dir(output)} {
		if err = os.MkdirAll(dir, 0700); err != nil {
			a.fail(w, err)
			return
		}
	}
	job := store.UploadJob{ID: id, Kind: "cover", CoverInputPath: input, SourcePath: track.OriginalPath, OutputPath: output, TargetTrackID: track.ID, FileName: part.FileName()}
	if err = a.Store.CreateJob(r.Context(), job); err != nil {
		a.fail(w, err)
		return
	}
	file, err := os.OpenFile(input, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		job.ReservedBytes, err = io.CopyBuffer(&uploadWriter{writer: file, reserve: func(minimum, preferred int64) (int64, error) {
			return a.Store.ReserveUploadBatch(r.Context(), id, minimum, preferred, a.quotaLimit())
		}}, io.LimitReader(part, media.MaxCoverBytes+1), make([]byte, 64*1024))
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err == nil && (job.ReservedBytes == 0 || job.ReservedBytes > media.MaxCoverBytes) {
		err = errors.New("请选择不超过 8 MiB 的非空图片")
	}
	if err == nil {
		err = a.Store.QueueUpload(r.Context(), id, job.ReservedBytes)
	}
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
		defer cancel()
		if stateErr := a.Store.SetJobState(cleanupCtx, id, "receiving", "failed", err.Error()); stateErr == nil {
			a.cleanFailedJob(cleanupCtx, job)
		} else {
			a.logMaintenance("record cover upload failure", stateErr)
		}
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrQuotaExceeded) {
			status = http.StatusInsufficientStorage
		}
		writeError(w, status, err)
		return
	}
	job.State = "queued"
	writeJSON(w, 202, job)
}

func (a *API) processUploadedCover(ctx context.Context, job store.UploadJob) {
	track, err := a.Store.Track(ctx, job.TargetTrackID)
	var old store.PendingFile
	var size, inputSize int64
	if err == nil && track.OriginalPath != job.SourcePath {
		err = errors.New("音频版本已改变，请重新上传封面")
	}
	if err == nil {
		info, statErr := os.Stat(job.CoverInputPath)
		if statErr != nil {
			err = statErr
		} else {
			inputSize = info.Size()
		}
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
		size, err = a.Media.NormalizeCover(ctx, job.CoverInputPath, job.OutputPath, func(n int64) error { return a.Store.ReserveJob(ctx, job.ID, n, a.quotaLimit()) })
	}
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		err = a.Store.CompleteUploadedCover(ctx, job, old, inputSize, size)
	}
	if err != nil {
		if stateErr := a.Store.SetJobState(ctx, job.ID, "processing", "failed", err.Error()); stateErr != nil {
			a.logMaintenance("record cover processing failure", stateErr)
			return
		}
		a.cleanFailedJob(ctx, job)
	}
	a.Maintain(ctx)
}
