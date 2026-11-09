package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"music-player/server/internal/media"
	"music-player/server/internal/store"
)

func legacyIndexFixture(t *testing.T) (*API, store.Track, string) {
	return legacyAudioFixture(t, "flac")
}

func legacyAudioFixture(t *testing.T, format string) (*API, store.Track, string) {
	t.Helper()
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
	ctx := testContext()
	root := t.TempDir()
	for _, dir := range []string{"originals", "playable"} {
		if err := os.MkdirAll(filepath.Join(root, "media", dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	source, output := filepath.Join(root, "media", "originals", "legacy."+format), filepath.Join(root, "media", "playable", "legacy.mp3")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "aevalsrc='if(lt(t,1),0,if(lt(t,2),sin(2*PI*440*t),random(0)*0.3))':s=48000:d=4", "-c:a", format, source)
	if stderr, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("source: %v %s", err, stderr)
	}
	file, err := os.Create(output)
	if err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("ffmpeg", "-v", "error", "-i", source, "-vn", "-c:a", "libmp3lame", "-q:a", "2", "-ar", "48000", "-ac", "2", "-f", "mp3", "pipe:1")
	cmd.Stdout = file
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	closeErr := file.Close()
	if runErr != nil || closeErr != nil {
		t.Fatalf("legacy MP3: %v %v %s", runErr, closeErr, stderr.String())
	}
	sourceInfo, _ := os.Stat(source)
	outputInfo, _ := os.Stat(output)
	db := filepath.Join(root, "test.db")
	s, err := store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s, Media: media.New(root)}
	t.Cleanup(func() { _ = api.Store.Close() })
	track, err := s.InsertTrack(ctx, store.NewTrack{ID: "legacy", Title: "kept", Artist: "artist", FileName: "legacy.flac", OriginalPath: source, PlayablePath: output, ContentType: "audio/mpeg", DurationSeconds: 4, SizeBytes: sourceInfo.Size() + outputInfo.Size()})
	if err != nil {
		t.Fatal(err)
	}
	end := 3.0
	track, err = s.UpdateTrack(ctx, track.ID, 2, 1, &end, .6, "lyrics")
	if err != nil {
		t.Fatal(err)
	}
	return api, track, db
}

func TestLegacyIndexRepairRecoversAndPreservesOriginalAndSettings(t *testing.T) {
	ctx := testContext()
	api, old, db := legacyIndexFixture(t)
	original, err := os.ReadFile(old.OriginalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = api.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	jobs, _ := api.Store.Jobs(ctx)
	if len(jobs) != 1 || jobs[0].Kind != "repair" {
		t.Fatalf("repair jobs=%#v", jobs)
	}
	job := jobs[0]
	if err = api.Store.SetJobState(ctx, job.ID, "queued", "processing", ""); err != nil {
		t.Fatal(err)
	}
	if err = api.Store.ReserveJob(ctx, job.ID, 10, QuotaBytes); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(job.OutputPath, []byte("unfinished"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = api.Store.Close(); err != nil {
		t.Fatal(err)
	}
	api.Store, err = store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err = api.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if err = api.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	jobs, _ = api.Store.Jobs(ctx)
	if len(jobs) != 1 || jobs[0].State != "queued" || jobs[0].ReservedBytes != 0 {
		t.Fatalf("repair restart=%#v", jobs)
	}
	if _, err = os.Stat(job.SourcePath); err != nil {
		t.Fatalf("recovery deleted active audio: %v", err)
	}
	finishTestJob(t, api, job.ID, "succeeded")
	updated, err := api.Store.Track(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PlaybackIndexVersion != 1 || updated.AudioVersion != old.AudioVersion+2 || updated.OriginalContentType != "audio/flac" || updated.PlayablePath == old.PlayablePath {
		t.Fatalf("repair=%#v", updated)
	}
	if updated.Title != old.Title || updated.Artist != old.Artist || updated.Lyrics != old.Lyrics || updated.BaseRandomWeight != old.BaseRandomWeight || updated.VolumeRatio != old.VolumeRatio || updated.PlaybackStartSeconds != 1 || updated.PlaybackEndSeconds == nil || *updated.PlaybackEndSeconds != 3 {
		t.Fatalf("repair changed settings: %#v", updated)
	}
	after, err := os.ReadFile(old.OriginalPath)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("original changed: %v", err)
	}
	if _, err = os.Stat(old.PlayablePath); !os.IsNotExist(err) {
		t.Fatalf("old fallback not cleaned: %v", err)
	}
	output, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "json", updated.PlayablePath).Output()
	if err != nil {
		t.Fatal(err)
	}
	var probe struct{ Format struct{ Duration string } }
	if err = json.Unmarshal(output, &probe); err != nil {
		t.Fatal(err)
	}
	var duration float64
	if _, err = fmt.Sscanf(probe.Format.Duration, "%f", &duration); err != nil || math.Abs(duration-4) > .15 {
		t.Fatalf("indexed duration=%f err=%v", duration, err)
	}
	if err = api.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	jobs, _ = api.Store.Jobs(ctx)
	if len(jobs) != 1 {
		t.Fatal("completed repair was scheduled twice")
	}
	reserved, _ := api.Store.ReservedBytes(ctx)
	if reserved != 0 {
		t.Fatalf("repair reservation leaked: %d", reserved)
	}
}

func TestIndexRepairQuotaFailureLeavesCurrentAudioUsable(t *testing.T) {
	ctx := testContext()
	api, old, _ := legacyIndexFixture(t)
	used, err := api.Store.Usage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	api.QuotaLimit = used
	if err = api.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	jobs, _ := api.Store.Jobs(ctx)
	if len(jobs) != 1 {
		t.Fatalf("jobs=%#v", jobs)
	}
	failed := finishTestJob(t, api, jobs[0].ID, "failed")
	if failed.ReservedBytes != 0 {
		t.Fatal("failed repair kept unused reservation")
	}
	current, err := api.Store.Track(ctx, old.ID)
	if err != nil || current.PlayablePath != old.PlayablePath || current.AudioVersion != old.AudioVersion+1 {
		t.Fatalf("active audio changed: %#v %v", current, err)
	}
	for _, path := range []string{old.OriginalPath, old.PlayablePath} {
		if _, err = os.Stat(path); err != nil {
			t.Fatalf("failed repair removed active file: %s %v", path, err)
		}
	}
	if _, err = os.Stat(failed.OutputPath); !os.IsNotExist(err) {
		t.Fatalf("failed candidate leaked: %v", err)
	}
}

func TestIndexRepairDoesNotOverwriteConcurrentReplacement(t *testing.T) {
	ctx := testContext()
	api, old, _ := legacyIndexFixture(t)
	if err := api.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	jobs, _ := api.Store.Jobs(ctx)
	replacementPath := filepath.Join(api.Media.Root, "media", "originals", "replacement.mp3")
	if err := os.WriteFile(replacementPath, []byte("new audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := api.Store.Track(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	replaced, err := api.Store.ReplaceTrack(ctx, old.ID, store.NewTrack{Title: "new", Artist: "new artist", OriginalPath: replacementPath, PlayablePath: replacementPath, ContentType: "audio/mpeg", OriginalContentType: "audio/mpeg", PlaybackIndexVersion: 1, SizeBytes: 9, DurationSeconds: 4}, storedFiles(current))
	if err != nil {
		t.Fatal(err)
	}
	finishTestJob(t, api, jobs[0].ID, "failed")
	current, err = api.Store.Track(ctx, old.ID)
	if err != nil || current.PlayablePath != replacementPath || current.AudioVersion != replaced.AudioVersion {
		t.Fatalf("stale repair overwrote replacement: %#v %v", current, err)
	}
	if content, err := os.ReadFile(replacementPath); err != nil || string(content) != "new audio" {
		t.Fatalf("active replacement removed: %q %v", content, err)
	}
}
