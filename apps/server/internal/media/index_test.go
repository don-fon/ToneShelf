package media

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestProcessCreatesAccurateSeekableMP3(t *testing.T) {
	for _, format := range []struct{ ext, codec string }{{"flac", "flac"}, {"aac", "aac"}} {
		t.Run(format.ext, func(t *testing.T) {
			if _, err := exec.LookPath("ffmpeg"); err != nil {
				t.Skip("ffmpeg unavailable")
			}
			if _, err := exec.LookPath("ffprobe"); err != nil {
				t.Skip("ffprobe unavailable")
			}
			root := t.TempDir()
			source, output := filepath.Join(root, "source."+format.ext), filepath.Join(root, "output.mp3")
			// Silence, a tone, and noise produce dramatically different VBR bitrates.
			cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "aevalsrc='if(lt(t,1),0,if(lt(t,2),sin(2*PI*440*t),random(0)*0.3))':s=48000:d=4", "-c:a", format.codec, source)
			if stderr, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("fixture: %v %s", err, stderr)
			}
			if format.ext == "aac" {
				data, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "json", source).Output()
				if err != nil {
					t.Fatal(err)
				}
				var estimated probeOutput
				if err = json.Unmarshal(data, &estimated); err != nil {
					t.Fatal(err)
				}
				seconds, err := strconv.ParseFloat(estimated.Format.Duration, 64)
				if err != nil || math.Abs(seconds-4) < .15 {
					t.Fatalf("fixture must reproduce inaccurate ADTS estimate: %f %v", seconds, err)
				}
			}
			processor := New(root)
			var reserved int64
			result, err := processor.Process(context.Background(), "id", "song."+format.ext, source, output, func(bytes int64) error { reserved += bytes; return nil })
			if err != nil {
				t.Fatal(err)
			}
			meta, err := processor.probe(context.Background(), output)
			if err != nil || math.Abs(meta.duration-4) > .15 {
				t.Fatalf("indexed duration=%v err=%v", meta.duration, err)
			}
			if result.OriginalContentType != "audio/"+format.ext || result.PlayablePath != output {
				t.Fatalf("result=%#v", result)
			}
			if _, err = os.Stat(output + ".stream"); !os.IsNotExist(err) {
				t.Fatalf("stream candidate leaked: %v", err)
			}
			info, _ := os.Stat(output)
			if reserved < info.Size() {
				t.Fatal("output was not reserved")
			}
		})
	}
}

func TestOriginalMIMEUsesProbedContainerInsteadOfFilename(t *testing.T) {
	for _, test := range []struct{ format, codec, want string }{
		{"flac", "flac", "audio/flac"}, {"aac", "aac", "audio/aac"},
		{"mov,mp4,m4a,3gp,3g2,mj2", "aac", "audio/mp4"},
		{"ogg", "opus", "audio/ogg; codecs=opus"}, {"asf", "wmav2", "audio/x-ms-wma"},
	} {
		if got := originalMIME(test.format, test.codec); got != test.want {
			t.Fatalf("format=%s MIME=%s want=%s", test.format, got, test.want)
		}
	}
}
