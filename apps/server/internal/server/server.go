package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"music-player/server/internal/lyrics"
	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

const QuotaBytes int64 = 10 * 1024 * 1024 * 1024

type API struct {
	Store       *store.Store
	Media       *media.Processor
	Lyrics      *lyrics.Client
	Log         *slog.Logger
	WebDir      string
	QuotaLimit  int64
	ResolveUser UserResolver

	maintenanceMu sync.Mutex
	authMu        sync.Mutex
	loginWindow   time.Time
	loginCount    int
	activeLogins  int
}

func (a *API) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, middleware.Timeout(30*time.Minute), sameOriginWrites)
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Post("/api/auth/login", a.login)
	r.Post("/api/auth/logout", a.logout)
	r.Group(func(r chi.Router) {
		r.Use(a.withCurrentUser)
		r.Get("/api/current-user", a.currentUser)
		r.Get("/api/library", a.library)
		r.Post("/api/tags", a.saveTag)
		r.Patch("/api/tags/{tagID}", a.saveTag)
		r.Delete("/api/tags/{tagID}", a.deleteTag)
		r.Post("/api/tag-groups", a.saveTagGroup)
		r.Put("/api/tag-groups/{groupID}", a.saveTagGroup)
		r.Delete("/api/tag-groups/{groupID}", a.deleteTagGroup)
		r.Patch("/api/track-tags", a.setTrackTags)
		r.Get("/api/playlists", a.playlists)
		r.Post("/api/playlists", a.createPlaylist)
		r.Patch("/api/playlists/{playlistID}", a.renamePlaylist)
		r.Delete("/api/playlists/{playlistID}", a.deletePlaylist)
		r.Post("/api/playlists/{playlistID}/tracks/{trackID}", a.addTrack)
		r.Post("/api/playlists/{playlistID}/tracks/{trackID}/move", a.moveTrack)
		r.Delete("/api/playlists/{playlistID}/tracks/{trackID}", a.removeTrack)
		r.Get("/api/jobs", a.jobs)
		r.Post("/api/jobs/{jobID}/resolve", a.resolveUpload)
		r.Post("/api/tracks", a.upload)
		r.Post("/api/uploads/check", func(w http.ResponseWriter, r *http.Request) { a.receiveJob(w, r, "", true) })
		r.Patch("/api/tracks/{trackID}", a.updateTrack)
		r.Delete("/api/tracks/{trackID}", a.deleteTrack)
		r.Put("/api/tracks/{trackID}/audio", a.replaceAudio)
		r.Post("/api/tracks/{trackID}/restore", a.restoreTrack)
		r.Get("/api/trash", a.trash)
		r.Get("/api/tracks/{trackID}/lyrics/search", a.searchLyrics)
		r.Get("/api/tracks/{trackID}/audio", a.audio)
		r.Get("/api/tracks/{trackID}/cover", a.cover)
		r.Post("/api/tracks/{trackID}/cover", a.uploadCover)
		r.Get("/api/quota", a.quota)
	})
	if a.WebDir != "" {
		r.Handle("/*", spaHandler(a.WebDir))
	}
	return r
}

func (a *API) moveTrack(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TargetPlaylistID string `json:"targetPlaylistId"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	id, err := newID()
	if err != nil {
		a.fail(w, err)
		return
	}
	if err = a.Store.MoveTrack(r.Context(), chi.URLParam(r, "playlistID"), in.TargetPlaylistID, chi.URLParam(r, "trackID"), id); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) renamePlaylist(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := a.Store.RenamePlaylist(r.Context(), chi.URLParam(r, "playlistID"), in.Name); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeletePlaylist(r.Context(), chi.URLParam(r, "playlistID")); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removeTrack(w http.ResponseWriter, r *http.Request) {
	confirmDelete, _ := strconv.ParseBool(r.URL.Query().Get("confirmDelete"))
	result, err := a.Store.RemoveTrackFromPlaylist(r.Context(), chi.URLParam(r, "playlistID"), chi.URLParam(r, "trackID"), confirmDelete, time.Now())
	if errors.Is(err, store.ErrConfirmationRequired) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "歌曲只属于默认歌单，移除后将进入回收站", "requiresConfirmation": true})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) searchLyrics(w http.ResponseWriter, r *http.Request) {
	if a.Lyrics == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("歌词搜索未启用"))
		return
	}
	track, err := a.Store.Track(r.Context(), chi.URLParam(r, "trackID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	result, err := a.Lyrics.Search(r.Context(), lyrics.SearchInput{
		Title: track.Title, Artist: track.Artist, Album: track.Album, DurationSeconds: track.DurationSeconds,
	})
	if err != nil {
		a.Log.Warn("lyrics search failed", "track", track.ID, "error", err)
		writeError(w, http.StatusBadGateway, errors.New("歌词服务暂时不可用"))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func spaHandler(root string) http.HandlerFunc {
	files := http.FileServer(http.Dir(root))
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		path := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	}
}

func (a *API) playlists(w http.ResponseWriter, r *http.Request) {
	v, err := a.Store.Playlists(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (a *API) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	id, err := newID()
	if err != nil {
		a.fail(w, err)
		return
	}
	p, err := a.Store.CreatePlaylist(r.Context(), id, strings.TrimSpace(in.Name))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (a *API) addTrack(w http.ResponseWriter, r *http.Request) {
	id, _ := newID()
	err := a.Store.AddTrack(r.Context(), chi.URLParam(r, "playlistID"), chi.URLParam(r, "trackID"), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteTrack(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.SoftDeleteTrack(r.Context(), chi.URLParam(r, "trackID"), time.Now()); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	a.Maintain(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) trash(w http.ResponseWriter, r *http.Request) {
	tracks, err := a.Store.Trash(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tracks)
}

func (a *API) restoreTrack(w http.ResponseWriter, r *http.Request) {
	itemID, _ := newID()
	track, err := a.Store.RestoreTrack(r.Context(), chi.URLParam(r, "trackID"), itemID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, track)
}

func (a *API) updateTrack(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Note                 *string   `json:"note"`
		Title                *string   `json:"title"`
		Artist               *string   `json:"artist"`
		BaseRandomWeight     float64   `json:"baseRandomWeight"`
		PlaybackStartSeconds float64   `json:"playbackStartSeconds"`
		PlaybackEndSeconds   *float64  `json:"playbackEndSeconds"`
		VolumeRatio          float64   `json:"volumeRatio"`
		Lyrics               string    `json:"lyrics"`
		LyricsLayout         *string   `json:"lyricsLayout"`
		LyricsPrimaryLine    *string   `json:"lyricsPrimaryLine"`
		TagIDs               *[]string `json:"tagIds"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var tagsJSON *string
	if in.TagIDs != nil {
		raw, _ := json.Marshal(*in.TagIDs)
		text := string(raw)
		tagsJSON = &text
	}
	t, err := a.Store.UpdateTrack(r.Context(), chi.URLParam(r, "trackID"), in.BaseRandomWeight, in.PlaybackStartSeconds, in.PlaybackEndSeconds, in.VolumeRatio, in.Lyrics, in.Title, in.Artist, in.LyricsLayout, in.LyricsPrimaryLine, tagsJSON, in.Note)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (a *API) audio(w http.ResponseWriter, r *http.Request) {
	t, err := a.Store.Track(r.Context(), chi.URLParam(r, "trackID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	path, contentType := t.PlayablePath, t.ContentType
	if r.URL.Query().Get("source") == "original" {
		path, contentType = t.OriginalPath, t.OriginalContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

func (a *API) quota(w http.ResponseWriter, r *http.Request) {
	used, reserved, err := a.Store.Quota(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"usedBytes": used, "reservedBytes": reserved, "limitBytes": a.quotaLimit(), "remainingBytes": a.quotaLimit() - used - reserved})
}

func (a *API) fail(w http.ResponseWriter, err error) {
	if a.Log != nil {
		a.Log.Error("request failed", "error", err)
	}
	writeError(w, http.StatusInternalServerError, errors.New("服务器内部错误"))
}

func storedFiles(track store.Track) []store.PendingFile {
	seen := make(map[string]bool)
	files := make([]store.PendingFile, 0, 2)
	for _, path := range []string{track.OriginalPath, track.PlayablePath, track.CoverPath} {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		var size int64
		if info, err := os.Stat(path); err == nil {
			size = info.Size()
		}
		files = append(files, store.PendingFile{Path: path, SizeBytes: size})
	}
	var accounted int64
	for _, file := range files {
		accounted += file.SizeBytes
	}
	if len(files) > 0 && accounted < track.SizeBytes {
		// Keep the old version fully charged until cleanup even when one file
		// cannot be stat'ed (for example because it is temporarily locked).
		files[0].SizeBytes += track.SizeBytes - accounted
	}
	return files
}

func filePart(mr *multipart.Reader) (*multipart.Part, error) {
	for {
		p, err := mr.NextPart()
		if err != nil {
			return nil, err
		}
		if p.FormName() == "file" && p.FileName() != "" {
			return p, nil
		}
		p.Close()
	}
}
func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	d := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func newID() (string, error) {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + fmt.Sprint(time.Now().Nanosecond()), nil
}
