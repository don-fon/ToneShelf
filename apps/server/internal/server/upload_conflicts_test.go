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

func TestDuplicateUploadDecisionAndRecovery(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	for _, overwrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "overwrite"}[overwrite], func(t *testing.T) {
			ctx := testContext()
			root := t.TempDir()
			db := filepath.Join(root, "test.db")
			s, err := store.Open(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close() }()
			api := &API{Store: s, Media: media.New(root), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			source := filepath.Join(root, "different-filename.flac")
			if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=0.2", "-metadata", "title=Same Song", source).CombinedOutput(); err != nil {
				t.Fatalf("generate %s %v", out, err)
			}
			oldPath := filepath.Join(root, "old.mp3")
			if err = os.WriteFile(oldPath, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			old, err := s.InsertTrack(ctx, store.NewTrack{ID: "old", Title: "same song", OriginalPath: oldPath, PlayablePath: oldPath, SizeBytes: 3, DurationSeconds: 10})
			if err != nil {
				t.Fatal(err)
			}
			playlist, err := s.CreatePlaylist(ctx, "custom", "custom")
			if err != nil {
				t.Fatal(err)
			}
			if err = s.AddTrack(ctx, playlist.ID, "old", "item"); err != nil {
				t.Fatal(err)
			}
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			part, err := form.CreateFormFile("file", "different-filename.flac")
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = part.Write(data); err != nil {
				t.Fatal(err)
			}
			form.Close()
			request := httptest.NewRequest(http.MethodPost, "/api/tracks", &body)
			request.Header.Set("Content-Type", form.FormDataContentType())
			response := httptest.NewRecorder()
			api.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusAccepted {
				t.Fatalf("upload %d %s", response.Code, response.Body.String())
			}
			jobs, err := s.Jobs(ctx)
			if err != nil || len(jobs) != 1 {
				t.Fatalf("jobs %#v %v", jobs, err)
			}
			job := jobs[0]
			if err = s.SetJobState(ctx, job.ID, "queued", "processing", ""); err != nil {
				t.Fatal(err)
			}
			api.processJob(ctx, job)
			jobs, err = s.Jobs(ctx)
			if err != nil || !jobs[0].RequiresConfirmation || jobs[0].ConflictTitle != "Same Song" {
				t.Fatalf("conflict %#v %v", jobs, err)
			}
			waiting := jobs[0]
			if got, err := os.ReadFile(oldPath); err != nil || string(got) != "old" {
				t.Fatalf("old audio changed %s %v", got, err)
			}
			s.Close()
			s, err = store.Open(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			api.Store = s
			if err = api.RecoverJobs(ctx); err != nil {
				t.Fatal(err)
			}
			jobs, err = s.Jobs(ctx)
			if err != nil || !jobs[0].RequiresConfirmation {
				t.Fatalf("recovery %#v %v", jobs, err)
			}
			// Even a running worker must leave a pending decision and its files alone.
			runCtx, cancel := context.WithTimeout(ctx, 600*time.Millisecond)
			api.RunJobs(runCtx)
			cancel()
			jobs, _ = s.Jobs(ctx)
			if !jobs[0].RequiresConfirmation {
				t.Fatal("worker restarted pending conversion")
			}
			response = httptest.NewRecorder()
			api.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
			if strings.Contains(response.Body.String(), root) || !strings.Contains(response.Body.String(), `"requiresConfirmation":true`) {
				t.Fatalf("public jobs %s", response.Body.String())
			}
			decision := "false"
			if overwrite {
				decision = "true"
			}
			resolve := func() *httptest.ResponseRecorder {
				r := httptest.NewRecorder()
				api.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/jobs/"+job.ID+"/resolve", strings.NewReader(`{"overwrite":`+decision+`}`)))
				return r
			}
			blocked := filepath.Join(waiting.OutputPath, "blocked")
			if !overwrite {
				// A nonempty directory makes deletion fail on Windows and Linux.
				if err = os.Remove(waiting.OutputPath); err != nil {
					t.Fatal(err)
				}
				if err = os.Mkdir(waiting.OutputPath, 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(blocked, []byte("blocked"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			response = resolve()
			if response.Code != http.StatusNoContent {
				t.Fatalf("resolve %d %s", response.Code, response.Body.String())
			}
			if !overwrite {
				jobs, _ = s.Jobs(ctx)
				if jobs[0].State != "failed" || jobs[0].ReservedBytes == 0 {
					t.Fatalf("released quota before cleanup %#v", jobs)
				}
				s.Close()
				s, err = store.Open(ctx, db)
				if err != nil {
					t.Fatal(err)
				}
				api.Store = s
				if err = os.Remove(blocked); err != nil {
					t.Fatal(err)
				}
				if err = api.RecoverJobs(ctx); err != nil {
					t.Fatal(err)
				}
			}
			jobs, _ = s.Jobs(ctx)
			if (!overwrite && len(jobs) != 0) || (overwrite && (len(jobs) != 1 || jobs[0].RequiresConfirmation || jobs[0].ReservedBytes != 0)) {
				t.Fatalf("unfinished %#v", jobs)
			}
			used, reserved, err := s.Quota(ctx)
			if err != nil || reserved != 0 {
				t.Fatalf("quota %d %d %v", used, reserved, err)
			}
			current, err := s.Track(ctx, old.ID)
			if err != nil {
				t.Fatal(err)
			}
			if overwrite {
				if current.AudioVersion != old.AudioVersion+1 || current.OriginalPath != waiting.SourcePath || used != current.SizeBytes {
					t.Fatalf("replacement %#v usage %d", current, used)
				}
				if _, err = os.Stat(oldPath); !os.IsNotExist(err) {
					t.Fatalf("old file remains %v", err)
				}
			} else {
				if current.AudioVersion != old.AudioVersion || used != 3 {
					t.Fatalf("cancel changed original %#v quota %d", current, used)
				}
				for _, path := range []string{waiting.SourcePath, waiting.OutputPath} {
					if _, err = os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("candidate remains %s %v", path, err)
					}
				}
			}
			response = resolve()
			wantStatus := http.StatusConflict
			if !overwrite {
				wantStatus = http.StatusNotFound
			}
			if response.Code != wantStatus {
				t.Fatalf("double decision %d", response.Code)
			}
			response = httptest.NewRecorder()
			api.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/playlists", nil))
			var playlists []store.Playlist
			if err = json.Unmarshal(response.Body.Bytes(), &playlists); err != nil {
				t.Fatal(err)
			}
			for _, p := range playlists {
				if len(p.Tracks) != 1 || p.Tracks[0].ID != "old" {
					t.Fatalf("playlist changed %#v", p)
				}
			}
		})
	}
}
