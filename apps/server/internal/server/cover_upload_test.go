package server

import (
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

func TestCoverUploadAndReplacementFlow(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
	ctx := testContext()
	root := t.TempDir()
	s, err := store.Open(ctx, filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	api := &API{Store: s, Media: media.New(root)}
	art := filepath.Join(root, "embedded.jpg")
	f, err := os.Create(art)
	if err != nil {
		t.Fatal(err)
	}
	if err = jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 100, 100)), nil); err != nil {
		t.Fatal(err)
	}
	f.Close()
	var old store.Track
	for _, embedded := range []bool{true, false} {
		id := "initial"
		if !embedded {
			id = "replacement"
		}
		source := filepath.Join(root, id+".mp3")
		args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=duration=1"}
		if embedded {
			args = append(args, "-i", art, "-map", "0:a", "-map", "1:v", "-c:v", "copy", "-disposition:v", "attached_pic")
		}
		args = append(args, "-c:a", "libmp3lame", source)
		if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
			t.Fatalf("fixture: %v %s", err, out)
		}
		job := store.UploadJob{ID: id, FileName: id + ".mp3", SourcePath: source, OutputPath: filepath.Join(root, id+"-playback.mp3")}
		if !embedded {
			job.TargetTrackID = old.ID
		}
		if err = s.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(source)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.ReserveJob(ctx, job.ID, info.Size(), QuotaBytes); err != nil {
			t.Fatal(err)
		}
		if err = s.SetJobState(ctx, job.ID, "receiving", "processing", ""); err != nil {
			t.Fatal(err)
		}
		api.processJob(ctx, job)
		jobs, err := s.Jobs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, result := range jobs {
			if result.ID == job.ID && result.State != "succeeded" {
				t.Fatalf("job=%+v", result)
			}
		}
		trackID := job.ID
		if !embedded {
			trackID = old.ID
		}
		got, err := s.Track(ctx, trackID)
		if err != nil {
			t.Fatal(err)
		}
		if embedded {
			if got.CoverPath == "" || got.CoverURL == "" {
				t.Fatalf("cover not activated: %+v", got)
			}
			coverInfo, err := os.Stat(got.CoverPath)
			if err != nil {
				t.Fatal(err)
			}
			if got.SizeBytes != info.Size()+coverInfo.Size() {
				t.Fatal("cover omitted from quota")
			}
			old = got
		} else {
			if got.CoverPath != "" || got.CoverURL != "" {
				t.Fatal("old cover survived replacement")
			}
			if _, err = os.Stat(old.CoverPath); !os.IsNotExist(err) {
				t.Fatal("old cover file retained", err)
			}
		}
	}
}
