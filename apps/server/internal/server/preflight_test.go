package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

func TestUploadPreflightBeforeVisibleTasks(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	for _, mode := range []string{"cancel", "overwrite", "new", "restart", "renamed", "filename", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			db := filepath.Join(root, "test.db")
			s, err := store.Open(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close() }()
			api := &API{Store: s, Media: media.New(root), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			source := filepath.Join(root, "source.flac")
			args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=duration=0.2"}
			if mode != "filename" {
				args = append(args, "-metadata", "title= Same Song ")
			}
			args = append(args, source)
			if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
				t.Fatalf("generate %s %v", out, err)
			}
			oldPath := filepath.Join(root, "old.mp3")
			if err = os.WriteFile(oldPath, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			title := "same song"
			if mode == "new" {
				title = "other"
			}
			old, err := s.InsertTrack(ctx, store.NewTrack{ID: "old", Title: title, OriginalPath: oldPath, PlayablePath: oldPath, SizeBytes: 3, DurationSeconds: 10})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.CreatePlaylist(ctx, "source-playlist", "Source"); err != nil {
				t.Fatal(err)
			}
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			name := "different-file.flac"
			if mode == "filename" {
				name = "same song.flac"
			}
			part, err := form.CreateFormFile("file", name)
			if err != nil {
				t.Fatal(err)
			}
			audio, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "invalid" {
				audio = []byte("not audio")
			}
			if _, err = part.Write(audio); err != nil {
				t.Fatal(err)
			}
			form.Close()
			request := httptest.NewRequest(http.MethodPost, "/api/uploads/check?playlistId=source-playlist", &body)
			request.Header.Set("Content-Type", form.FormDataContentType())
			response := httptest.NewRecorder()
			api.Handler().ServeHTTP(response, request)
			if mode == "invalid" {
				if response.Code != http.StatusBadRequest {
					t.Fatalf("invalid accepted %d", response.Code)
				}
				_, reserved, err := s.Quota(ctx)
				if err != nil || reserved != 0 {
					t.Fatalf("invalid leaked quota %d %v", reserved, err)
				}
				jobs, err := s.Jobs(ctx)
				if err != nil || len(jobs) != 1 || jobs[0].State != "failed" {
					t.Fatalf("failed check %#v %v", jobs, err)
				}
				if _, err = os.Stat(jobs[0].SourcePath); !os.IsNotExist(err) {
					t.Fatalf("invalid file remains %v", err)
				}
				return
			}
			if response.Code != http.StatusAccepted {
				t.Fatalf("check %d %s", response.Code, response.Body.String())
			}
			var prepared store.UploadJob
			if err = json.Unmarshal(response.Body.Bytes(), &prepared); err != nil {
				t.Fatal(err)
			}
			if prepared.Kind != "preflight" || prepared.RequiresConfirmation != (mode != "new") || prepared.PlaylistID != "source-playlist" {
				t.Fatalf("prepared %#v", prepared)
			}
			jobs, err := s.Jobs(ctx)
			if err != nil || len(jobs) != 1 {
				t.Fatalf("jobs %#v %v", jobs, err)
			}
			hidden := jobs[0]
			if _, err = os.Stat(hidden.OutputPath); !os.IsNotExist(err) {
				t.Fatalf("preflight transcoded %v", err)
			}
			visible := httptest.NewRecorder()
			api.Handler().ServeHTTP(visible, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
			if strings.TrimSpace(visible.Body.String()) != "[]" {
				t.Fatalf("preflight leaked %s", visible.Body.String())
			}
			used, reserved, err := s.Quota(ctx)
			if err != nil || used != 3 || reserved != int64(len(audio)) {
				t.Fatalf("quota %d %d %v", used, reserved, err)
			}
			runCtx, cancel := context.WithTimeout(ctx, 550*time.Millisecond)
			api.RunJobs(runCtx)
			cancel()
			if _, err = os.Stat(hidden.OutputPath); !os.IsNotExist(err) {
				t.Fatal("worker processed preflight")
			}
			resolve := func(overwrite bool) *httptest.ResponseRecorder {
				response := httptest.NewRecorder()
				value := "false"
				if overwrite {
					value = "true"
				}
				api.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/jobs/"+prepared.ID+"/resolve", strings.NewReader(`{"overwrite":`+value+`,"requestKey":"session:1"}`)))
				return response
			}
			if mode == "renamed" {
				renamed := "renamed"
				if _, err = s.UpdateTrack(ctx, "old", 1, 0, nil, .8, "", &renamed); err != nil {
					t.Fatal(err)
				}
				if response = resolve(true); response.Code != http.StatusConflict {
					t.Fatalf("renamed overwrite %d", response.Code)
				}
			}
			if mode == "restart" {
				s.Close()
				s, err = store.Open(ctx, db)
				if err != nil {
					t.Fatal(err)
				}
				api.Store = s
				if err = api.RecoverJobs(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				overwrite := mode == "overwrite" || mode == "new" || mode == "filename"
				response = resolve(overwrite)
				if response.Code != http.StatusNoContent {
					t.Fatalf("resolve %d %s", response.Code, response.Body.String())
				}
				if overwrite {
					job := finishTestJob(t, api, prepared.ID, "succeeded")
					if job.RequestKey != "session:1" {
						t.Fatalf("lost request key %#v", job)
					}
					if mode != "new" {
						current, err := s.Track(ctx, "old")
						if err != nil || current.AudioVersion != old.AudioVersion+1 {
							t.Fatalf("replace %#v %v", current, err)
						}
						if _, err = os.Stat(oldPath); !os.IsNotExist(err) {
							t.Fatal("old version not cleaned")
						}
					}
					playlists, err := s.Playlists(ctx)
					if err != nil {
						t.Fatal(err)
					}
					for _, playlist := range playlists {
						if playlist.ID == "source-playlist" {
							want := 0
							if mode == "new" {
								want = 1
							}
							if len(playlist.Tracks) != want {
								t.Fatalf("source playlist %#v want=%d", playlist, want)
							}
						}
					}
					return
				}
			}
			_, reserved, err = s.Quota(ctx)
			if err != nil || reserved != 0 {
				t.Fatalf("cleanup quota %d %v", reserved, err)
			}
			if _, err = os.Stat(hidden.SourcePath); !os.IsNotExist(err) {
				t.Fatalf("source remains %v", err)
			}
			current, err := s.Track(ctx, "old")
			if err != nil || current.AudioVersion != old.AudioVersion {
				t.Fatalf("old changed %#v %v", current, err)
			}
		})
	}
}

func TestPreflightRejectsMissingPlaylistBeforeReceiving(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	api := &API{Store: s}
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/uploads/check?playlistId=missing", strings.NewReader("")))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
	jobs, err := s.Jobs(ctx)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("created jobs %#v %v", jobs, err)
	}
}
