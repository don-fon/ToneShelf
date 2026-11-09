package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"music-player/server/internal/currentuser"
	"music-player/server/internal/store"
)

// RunJobs is owned by main. Startup recovery finishes before requests are
// accepted; shutdown waits for all child FFmpeg processes to exit.
func (a *API) RunJobs(ctx context.Context) {
	var workers sync.WaitGroup
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	defer workers.Wait()
	for {
		users, err := a.Store.UserIDs(ctx)
		if err != nil && ctx.Err() == nil {
			a.logMaintenance("list job owners", err)
		}
		for _, id := range users {
			if ctx.Err() != nil {
				return
			}
			a.scanUserJobs(currentuser.WithID(ctx, id), &workers)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *API) scanUserJobs(ctx context.Context, workers *sync.WaitGroup) {
	jobs, err := a.Store.Jobs(ctx)
	if err != nil {
		if ctx.Err() == nil {
			a.logMaintenance("scan upload jobs", err)
		}
		return
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return
		}
		if job.State != "queued" || job.RequiresConfirmation || job.Kind == "preflight" {
			continue
		}
		if err = a.Store.SetJobState(ctx, job.ID, "queued", "processing", ""); err != nil {
			continue
		}
		workers.Add(1)
		go func(job store.UploadJob) { defer workers.Done(); a.processJob(ctx, job) }(job)
	}
}

func (a *API) RecoverJobs(ctx context.Context) error {
	users, err := a.Store.UserIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range users {
		if err = a.recoverUserJobs(currentuser.WithID(ctx, id)); err != nil {
			return err
		}
	}
	if err = a.reconcileCandidates(ctx); err != nil {
		return err
	}
	a.ScheduleMediaMaintenance(ctx)
	return nil
}

func (a *API) recoverUserJobs(ctx context.Context) error {
	jobs, err := a.Store.Jobs(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.Kind == "preflight" {
			if job.State != "failed" {
				if err = a.Store.SetJobState(ctx, job.ID, job.State, "failed", "预检查中断，请重新选择文件"); err != nil {
					return err
				}
			}
			a.cleanFailedJob(ctx, job)
			continue
		}
		switch job.State {
		case "receiving":
			if err = a.Store.SetJobState(ctx, job.ID, "receiving", "failed", "上传中断，请重新选择文件上传"); err != nil {
				return err
			}
			job.State = "failed"
			a.cleanFailedJob(ctx, job)
		case "processing":
			if job.Kind == "repair" || job.Kind == "cover" {
				if err = removeCandidate(job.OutputPath + ".download"); err != nil {
					return err
				}
				if err = removeCandidate(job.OutputPath); err != nil {
					if stateErr := a.Store.SetJobState(ctx, job.ID, "processing", "failed", err.Error()); stateErr != nil {
						return stateErr
					}
					continue
				}
				reservation := int64(0)
				if job.Kind == "cover" && job.CoverInputPath != "" {
					info, statErr := os.Stat(job.CoverInputPath)
					if statErr != nil {
						if err = a.Store.SetJobState(ctx, job.ID, "processing", "failed", "封面图片缺失，请重新上传"); err != nil {
							return err
						}
						a.cleanFailedJob(ctx, job)
						continue
					}
					reservation = info.Size()
				}
				if err = a.Store.SetJobReservation(ctx, job.ID, reservation); err != nil {
					return err
				}
				if err = a.Store.SetJobState(ctx, job.ID, "processing", "queued", ""); err != nil {
					return err
				}
				continue
			}
			// Remove only this job's deterministic candidate output; never
			// touch files belonging to succeeded jobs.
			if err = removeCandidate(job.OutputPath); err != nil {
				if stateErr := a.Store.SetJobState(ctx, job.ID, "processing", "failed", "无法清理未完成的播放副本："+err.Error()); stateErr != nil {
					return stateErr
				}
				a.cleanFailedJob(ctx, job)
				continue
			}
			if err = removeCandidate(job.OutputPath + ".stream"); err != nil {
				if stateErr := a.Store.SetJobState(ctx, job.ID, "processing", "failed", err.Error()); stateErr != nil {
					return stateErr
				}
				a.cleanFailedJob(ctx, job)
				continue
			}
			if err = removeCandidate(job.OutputPath + ".cover.jpg"); err != nil {
				return err
			}
			info, statErr := os.Stat(job.SourcePath)
			if statErr != nil {
				if err = a.Store.SetJobState(ctx, job.ID, "processing", "failed", "源文件缺失，请重新上传"); err != nil {
					return err
				}
				a.cleanFailedJob(ctx, job)
				continue
			}
			if err = a.Store.SetJobReservation(ctx, job.ID, info.Size()); err != nil {
				return err
			}
			if err = a.Store.SetJobState(ctx, job.ID, "processing", "queued", ""); err != nil {
				return err
			}
		case "failed":
			a.cleanFailedJob(ctx, job)
		}
	}
	return nil
}

// Runs before workers/HTTP start. Only regular files inside the application's
// managed media directories are considered; symlinks and subdirectories are
// never traversed. Persist cleanup first, including quota, before deleting.
func (a *API) reconcileCandidates(ctx context.Context) error {
	known, err := a.Store.KnownMediaPaths(ctx)
	if err != nil {
		return err
	}
	normalized := make(map[string]bool, len(known))
	for path := range known {
		normalized[canonicalPath(path)] = true
	}
	for _, dir := range []string{"staging", "originals", "playable"} {
		root := filepath.Join(a.Media.Root, "media", dir)
		entries, readErr := os.ReadDir(root)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return readErr
		}
		for _, entry := range entries {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			path := filepath.Join(root, entry.Name())
			if !info.Mode().IsRegular() || normalized[canonicalPath(path)] {
				continue
			}
			if err = a.Store.QueueOrphan(ctx, path, info.Size()); err != nil {
				return err
			}
		}
	}
	a.Maintain(ctx)
	return nil
}

func canonicalPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = filepath.Clean(path)
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(absolute)
	}
	return absolute
}

func (a *API) processJob(ctx context.Context, job store.UploadJob) {
	if job.Kind == "cover" {
		if job.CoverInputPath != "" {
			a.processUploadedCover(ctx, job)
			return
		}
		a.processCover(ctx, job)
		return
	}
	if job.Kind == "repair" {
		a.processRepair(ctx, job)
		return
	}
	result, err := a.Media.Process(ctx, job.ID, job.FileName, job.SourcePath, job.OutputPath, func(bytes int64) error { return a.Store.ReserveJob(ctx, job.ID, bytes, a.quotaLimit()) })
	if ctx.Err() != nil {
		return
	} // leave processing durable for startup recovery
	if err == nil {
		var oldFiles []store.PendingFile
		if job.TargetTrackID != "" {
			track, trackErr := a.Store.Track(ctx, job.TargetTrackID)
			err = trackErr
			if err == nil {
				oldFiles = storedFiles(track)
			}
		}
		if err == nil {
			_, err = a.Store.CompleteJob(ctx, job, store.NewTrack{ID: result.ID, CoverPath: result.CoverPath, Title: result.Title, Artist: result.Artist, Album: result.Album, FileName: job.FileName, OriginalPath: result.OriginalPath, PlayablePath: result.PlayablePath, ContentType: result.ContentType, OriginalContentType: result.OriginalContentType, PlaybackIndexVersion: 1, DurationProbeVersion: 1, SizeBytes: result.SizeBytes, DurationSeconds: result.DurationSeconds}, oldFiles)
		}
	}
	if errors.Is(err, store.ErrUploadConflict) {
		return
	}
	if err != nil {
		if stateErr := a.Store.SetJobState(ctx, job.ID, "processing", "failed", err.Error()); stateErr != nil {
			a.logMaintenance("fail upload job", stateErr)
			return
		}
		a.cleanFailedJob(ctx, job)
	}
	a.Maintain(ctx)
}

func (a *API) cleanFailedJob(ctx context.Context, job store.UploadJob) {
	paths := []string{job.CoverInputPath, job.OutputPath + ".download", job.OutputPath, job.OutputPath + ".stream", job.OutputPath + ".cover.jpg"}
	if job.Kind != "repair" && job.Kind != "cover" {
		paths = append(paths, job.SourcePath)
	}
	for _, path := range paths {
		if err := removeCandidate(path); err != nil {
			a.logMaintenance("clean failed upload", err)
			return
		}
	}
	if err := a.Store.SetJobReservation(ctx, job.ID, 0); err != nil {
		a.logMaintenance("release upload reservation", err)
		return
	}
	if err := a.Store.DeleteCancelledJob(ctx, job.ID); err != nil {
		a.logMaintenance("delete cancelled upload", err)
	}
}

func removeCandidate(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (a *API) quotaLimit() int64 {
	if a.QuotaLimit > 0 {
		return a.QuotaLimit
	}
	return QuotaBytes
}

func (a *API) jobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := a.Store.Jobs(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	visible := make([]store.UploadJob, 0)
	completed := 0
	for _, job := range jobs {
		if job.Kind == "preflight" || (job.State == "failed" && job.Error == "已取消") {
			continue
		}
		if job.State == "succeeded" || job.State == "failed" {
			completed++
			if completed > 100 {
				continue
			}
		}
		visible = append(visible, job)
	}
	writeJSON(w, http.StatusOK, visible)
}

func (a *API) resolveUpload(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Overwrite  bool   `json:"overwrite"`
		RequestKey string `json:"requestKey"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	jobs, err := a.Store.Jobs(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	for _, job := range jobs {
		if job.ID != chi.URLParam(r, "jobID") {
			continue
		}
		if job.Kind == "preflight" {
			if len(in.RequestKey) > 128 {
				writeError(w, http.StatusBadRequest, errors.New("上传标识过长"))
				return
			}
			if in.Overwrite {
				err = a.Store.StartPreparedUpload(r.Context(), job.ID, in.RequestKey)
			} else {
				err = a.Store.SetJobState(r.Context(), job.ID, "queued", "failed", "已取消")
				if err == nil {
					a.cleanFailedJob(r.Context(), job)
				}
			}
			if err != nil {
				writeError(w, http.StatusConflict, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !job.RequiresConfirmation {
			writeError(w, http.StatusConflict, errors.New("任务无需确认"))
			return
		}
		if in.Overwrite {
			track, trackErr := a.Store.Track(r.Context(), job.TargetTrackID)
			err = trackErr
			if err == nil {
				_, err = a.Store.ConfirmUpload(r.Context(), job, storedFiles(track))
			}
		} else {
			err = a.Store.SetJobState(r.Context(), job.ID, "queued", "failed", "已取消")
			if err == nil {
				a.cleanFailedJob(r.Context(), job)
			}
		}
		if err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		a.Maintain(r.Context())
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.NotFound(w, r)
}

// Each request receives one file; clients can issue any number in parallel.
// The HTTP lifetime only covers reception, not probing or transcoding.
func (a *API) receiveJob(w http.ResponseWriter, r *http.Request, target string, preflight ...bool) {
	playlistID := ""
	if target == "" {
		playlistID = r.URL.Query().Get("playlistId")
		if playlistID != "" {
			exists, err := a.Store.PlaylistExists(r.Context(), playlistID)
			if err != nil {
				a.fail(w, err)
				return
			}
			if !exists {
				writeError(w, http.StatusNotFound, errors.New("上传目标歌单不存在"))
				return
			}
		}
	}
	if target != "" {
		if _, err := a.Store.Track(r.Context(), target); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	part, err := filePart(mr)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer part.Close()
	id, err := newID()
	if err != nil {
		a.fail(w, err)
		return
	}
	ext := strings.ToLower(filepath.Ext(part.FileName()))
	if len(ext) > 10 || ext == "" {
		ext = ".bin"
	}
	job := store.UploadJob{ID: id, FileName: part.FileName(), TargetTrackID: target, SourcePath: filepath.Join(a.Media.Root, "media", "originals", id+ext), OutputPath: filepath.Join(a.Media.Root, "media", "playable", id+".mp3"), State: "receiving", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	job.Kind = "upload"
	job.PlaylistID = playlistID
	checking := len(preflight) > 0 && preflight[0]
	if checking {
		job.Kind = "preflight"
	}
	if key := r.Header.Get("X-Upload-Key"); len(key) <= 128 {
		job.RequestKey = key
	}
	for _, path := range []string{filepath.Dir(job.SourcePath), filepath.Dir(job.OutputPath)} {
		if err = os.MkdirAll(path, 0o700); err != nil {
			a.fail(w, err)
			return
		}
	}
	if err = a.Store.CreateJob(r.Context(), job); err != nil {
		a.fail(w, err)
		return
	}
	file, err := os.OpenFile(job.SourcePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		job.ReservedBytes, err = io.CopyBuffer(&uploadWriter{writer: file, reserve: func(minimum, preferred int64) (int64, error) {
			return a.Store.ReserveUploadBatch(r.Context(), id, minimum, preferred, a.quotaLimit())
		}}, part, make([]byte, 64*1024))
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err == nil {
		if checking {
			var title string
			title, err = a.Media.InspectTitle(r.Context(), job.SourcePath, job.FileName)
			if err == nil {
				err = a.Store.PrepareUpload(r.Context(), id, title, job.ReservedBytes)
			}
		} else {
			err = a.Store.QueueUpload(r.Context(), id, job.ReservedBytes)
		}
	}
	if err != nil {
		// A disconnected request cannot be used to persist failure/cleanup.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
		defer cancel()
		if stateErr := a.Store.SetJobState(cleanupCtx, id, "receiving", "failed", err.Error()); stateErr == nil {
			a.cleanFailedJob(cleanupCtx, job)
		} else {
			a.logMaintenance("record interrupted upload", stateErr)
		}
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrQuotaExceeded) {
			status = http.StatusInsufficientStorage
		}
		writeError(w, status, err)
		return
	}
	job.State = "queued"
	if checking {
		jobs, listErr := a.Store.Jobs(r.Context())
		if listErr != nil {
			a.fail(w, listErr)
			return
		}
		for _, prepared := range jobs {
			if prepared.ID == id {
				job = prepared
				break
			}
		}
	}
	writeJSON(w, http.StatusAccepted, job)
}

type uploadWriter struct {
	writer    io.Writer
	reserve   func(minimum, preferred int64) (int64, error)
	available int64
}

func (w *uploadWriter) Write(data []byte) (int, error) {
	if needed := int64(len(data)) - w.available; needed > 0 {
		grant, err := w.reserve(needed, max(4<<20, needed))
		if err != nil {
			return 0, err
		}
		if grant < needed {
			return 0, errors.New("insufficient upload reservation")
		}
		w.available += grant
	}
	n, err := w.writer.Write(data)
	w.available -= int64(n)
	return n, err
}

func (a *API) replaceAudio(w http.ResponseWriter, r *http.Request) {
	a.receiveJob(w, r, chi.URLParam(r, "trackID"))
}
func (a *API) upload(w http.ResponseWriter, r *http.Request) { a.receiveJob(w, r, "") }
