package media

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEmbeddedCoverExtractionAndQuotaFallback(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
	root := t.TempDir()
	artwork := filepath.Join(root, "art.jpg")
	img := image.NewRGBA(image.Rect(0, 0, 800, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{220, 100, 60, 255})
		}
	}
	file, err := os.Create(artwork)
	if err != nil {
		t.Fatal(err)
	}
	if err = jpeg.Encode(file, img, nil); err != nil {
		t.Fatal(err)
	}
	file.Close()
	for _, format := range []struct{ ext, codec string }{{"mp3", "libmp3lame"}, {"flac", "flac"}, {"m4a", "aac"}} {
		t.Run(format.ext, func(t *testing.T) {
			source := filepath.Join(root, "song."+format.ext)
			cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=1", "-i", artwork, "-map", "0:a", "-map", "1:v", "-c:a", format.codec, "-c:v", "copy", "-disposition:v", "attached_pic", source)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("fixture: %v %s", err, out)
			}
			var reserved int64
			result, err := New(root).Process(context.Background(), "song", "song."+format.ext, source, filepath.Join(root, format.ext+".mp3"), func(n int64) error { reserved += n; return nil })
			if err != nil || result.CoverPath == "" {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			cover, err := os.Open(result.CoverPath)
			if err != nil {
				t.Fatal(err)
			}
			config, err := jpeg.DecodeConfig(cover)
			cover.Close()
			if err != nil || config.Width != 512 || config.Height != 256 {
				t.Fatalf("cover=%+v error=%v", config, err)
			}
			var actual int64
			paths := map[string]bool{source: true, result.PlayablePath: true, result.CoverPath: true}
			for path := range paths {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				actual += info.Size()
			}
			if result.SizeBytes != actual || reserved < actual-resultSize(t, source) {
				t.Fatalf("size=%d actual=%d reserved=%d", result.SizeBytes, actual, reserved)
			}
			denied := filepath.Join(root, format.ext+"-denied.jpg")
			path, size, err := New(root).ExtractCover(context.Background(), source, denied, func(int64) error { return errors.New("quota") })
			if err != nil || path != "" || size != 0 {
				t.Fatalf("optional cover quota: %q %d %v", path, size, err)
			}
			if _, err = os.Stat(denied); !os.IsNotExist(err) {
				t.Fatalf("partial cover retained: %v", err)
			}
		})
	}
	plain := filepath.Join(root, "plain.wav")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=1", plain).CombinedOutput(); err != nil {
		t.Fatalf("plain: %v %s", err, out)
	}
	path, size, err := New(root).ExtractCover(context.Background(), plain, filepath.Join(root, "missing.jpg"), func(int64) error { t.Fatal("no artwork must not reserve"); return nil })
	if err != nil || path != "" || size != 0 {
		t.Fatalf("no artwork: %q %d %v", path, size, err)
	}
}

func resultSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
