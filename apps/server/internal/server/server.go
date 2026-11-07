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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"music-player/server/internal/lyrics"
	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

const QuotaBytes int64 = 10 * 1024 * 1024 * 1024

type API struct {
	Store  *store.Store
	Media  *media.Processor
	Lyrics *lyrics.Client
	Log    *slog.Logger
	WebDir string
}

func (a *API) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, middleware.Timeout(30*time.Minute))
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/api/playlists", a.playlists)
	r.Post("/api/playlists", a.createPlaylist)
	r.Post("/api/playlists/{playlistID}/tracks/{trackID}", a.addTrack)
	r.Post("/api/tracks", a.upload)
	r.Patch("/api/tracks/{trackID}", a.updateTrack)
	r.Get("/api/tracks/{trackID}/lyrics/search", a.searchLyrics)
	r.Get("/api/tracks/{trackID}/audio", a.audio)
	r.Get("/api/quota", a.quota)
	if a.WebDir != "" {
		r.Handle("/*", spaHandler(a.WebDir))
	}
	return r
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

func (a *API) upload(w http.ResponseWriter, r *http.Request) {
	used, err := a.Store.Usage(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	if used >= QuotaBytes {
		writeError(w, http.StatusInsufficientStorage, errors.New("存储空间已满"))
		return
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
	result, err := a.Media.Prepare(r.Context(), part.FileName(), io.LimitReader(part, QuotaBytes-used+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if result.SizeBytes > QuotaBytes-used {
		media.Remove(result)
		writeError(w, http.StatusInsufficientStorage, errors.New("文件超过剩余空间"))
		return
	}
	t, err := a.Store.InsertTrack(r.Context(), store.NewTrack{ID: result.ID, Title: result.Title, Artist: result.Artist, Album: result.Album, OriginalPath: result.OriginalPath, PlayablePath: result.PlayablePath, ContentType: result.ContentType, SizeBytes: result.SizeBytes, DurationSeconds: result.DurationSeconds})
	if err != nil {
		media.Remove(result)
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (a *API) updateTrack(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BaseRandomWeight     float64  `json:"baseRandomWeight"`
		PlaybackStartSeconds float64  `json:"playbackStartSeconds"`
		PlaybackEndSeconds   *float64 `json:"playbackEndSeconds"`
		VolumeRatio          float64  `json:"volumeRatio"`
		Lyrics               string   `json:"lyrics"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	t, err := a.Store.UpdateTrack(r.Context(), chi.URLParam(r, "trackID"), in.BaseRandomWeight, in.PlaybackStartSeconds, in.PlaybackEndSeconds, in.VolumeRatio, in.Lyrics)
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
	f, err := os.Open(t.PlayablePath)
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
	w.Header().Set("Content-Type", t.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, filepath.Base(t.PlayablePath), info.ModTime(), f)
}

func (a *API) quota(w http.ResponseWriter, r *http.Request) {
	used, err := a.Store.Usage(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"usedBytes": used, "limitBytes": QuotaBytes, "remainingBytes": QuotaBytes - used})
}
func (a *API) fail(w http.ResponseWriter, err error) {
	a.Log.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, errors.New("服务器内部错误"))
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
