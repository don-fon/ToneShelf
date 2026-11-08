package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

func TestMaintenancePrunesLegacyCancelledJobsAndPreservesFailures(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := store.Open(ctx, filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	oldPath := filepath.Join(root, "old.mp3")
	if err = os.WriteFile(oldPath, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.InsertTrack(ctx, store.NewTrack{ID: "old", Title: "old", OriginalPath: oldPath, PlayablePath: oldPath, SizeBytes: 3, DurationSeconds: 10, OriginalContentType: "audio/mpeg", PlaybackIndexVersion: 1, DurationProbeVersion: 1}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"cancelled", "failed"} {
		job := store.UploadJob{ID: id, FileName: id + ".mp3", TargetTrackID: "old", SourcePath: filepath.Join(root, id+".source"), OutputPath: filepath.Join(root, id+".output")}
		if err = s.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
		reason := "转码失败"
		if id == "cancelled" {
			reason = "已取消"
			for _, path := range []string{job.SourcePath, job.OutputPath, job.OutputPath + ".stream"} {
				if err = os.WriteFile(path, []byte("candidate"), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err = s.SetJobState(ctx, id, "receiving", "failed", reason); err != nil {
			t.Fatal(err)
		}
	}
	api := &API{Store: s, Media: media.New(root), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	if strings.Contains(response.Body.String(), "已取消") || !strings.Contains(response.Body.String(), "转码失败") {
		t.Fatalf("visible tasks %s", response.Body.String())
	}
	api.Maintain(ctx)
	jobs, err := s.Jobs(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].ID != "failed" {
		t.Fatalf("jobs %#v %v", jobs, err)
	}
	for _, path := range []string{"cancelled.source", "cancelled.output", "cancelled.output.stream"} {
		if _, err = os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("candidate remains %s %v", path, err)
		}
	}
	if data, err := os.ReadFile(oldPath); err != nil || string(data) != "old" {
		t.Fatalf("original changed %s %v", data, err)
	}
	api.Maintain(ctx)
	jobs, err = s.Jobs(ctx)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("repeated cleanup %#v %v", jobs, err)
	}
}
