package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Result struct {
	ID, Title, Artist, Album, OriginalPath, PlayablePath, ContentType string
	SizeBytes                                                         int64
	DurationSeconds                                                   float64
}

type Processor struct {
	Root, FFmpeg, FFprobe string
}

type probeOutput struct {
	Format struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

func New(root string) *Processor { return &Processor{Root: root, FFmpeg: "ffmpeg", FFprobe: "ffprobe"} }

func (p *Processor) Prepare(ctx context.Context, name string, src io.Reader) (result Result, err error) {
	id, err := randomID()
	if err != nil {
		return result, err
	}
	for _, dir := range []string{"staging", "originals", "playable"} {
		if err = os.MkdirAll(filepath.Join(p.Root, "media", dir), 0o755); err != nil {
			return result, err
		}
	}
	ext := strings.ToLower(filepath.Ext(filepath.Base(name)))
	if ext == "" || len(ext) > 10 {
		ext = ".bin"
	}
	staging := filepath.Join(p.Root, "media", "staging", id+ext)
	f, err := os.OpenFile(staging, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return result, err
	}
	_, copyErr := io.Copy(f, src)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(staging)
		return result, copyErr
	}
	if closeErr != nil {
		os.Remove(staging)
		return result, closeErr
	}
	defer func() {
		if err != nil {
			os.Remove(staging)
			if result.OriginalPath != "" {
				os.Remove(result.OriginalPath)
			}
			if result.PlayablePath != "" && result.PlayablePath != result.OriginalPath {
				os.Remove(result.PlayablePath)
			}
		}
	}()

	meta, err := p.probe(ctx, staging)
	if err != nil {
		return result, fmt.Errorf("无法读取音频: %w", err)
	}
	original := filepath.Join(p.Root, "media", "originals", id+ext)
	if err = os.Rename(staging, original); err != nil {
		return result, err
	}
	result = Result{ID: id, OriginalPath: original, PlayablePath: original, ContentType: "audio/mpeg", DurationSeconds: meta.duration}
	result.Title = first(meta.tags, "title", strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	result.Artist = first(meta.tags, "artist", "")
	result.Album = first(meta.tags, "album", "")
	info, err := os.Stat(original)
	if err != nil {
		return result, err
	}
	result.SizeBytes = info.Size()
	if !strings.EqualFold(ext, ".mp3") {
		playable := filepath.Join(p.Root, "media", "playable", id+".mp3")
		cmd := exec.CommandContext(ctx, p.FFmpeg, "-v", "error", "-y", "-i", original, "-map", "0:a:0", "-vn", "-c:a", "libmp3lame", "-q:a", "2", "-ac", "2", playable)
		if output, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
			return result, fmt.Errorf("转码失败: %w: %s", cmdErr, strings.TrimSpace(string(output)))
		}
		info, err = os.Stat(playable)
		if err != nil {
			return result, err
		}
		result.PlayablePath = playable
		result.SizeBytes += info.Size()
	}
	return result, nil
}

type metadata struct {
	duration float64
	tags     map[string]string
}

func (p *Processor) probe(ctx context.Context, path string) (metadata, error) {
	cmd := exec.CommandContext(ctx, p.FFprobe, "-v", "error", "-show_entries", "format=duration:format_tags=title,artist,album", "-of", "json", path)
	b, err := cmd.Output()
	if err != nil {
		return metadata{}, err
	}
	var out probeOutput
	if err = json.Unmarshal(b, &out); err != nil {
		return metadata{}, err
	}
	var d float64
	if _, err = fmt.Sscanf(out.Format.Duration, "%f", &d); err != nil || d <= 0 {
		return metadata{}, errors.New("音频时长无效")
	}
	return metadata{duration: d, tags: out.Format.Tags}, nil
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func first(tags map[string]string, key, fallback string) string {
	for k, v := range tags {
		if strings.EqualFold(k, key) && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return fallback
}
func Remove(result Result) {
	_ = os.Remove(result.OriginalPath)
	if result.PlayablePath != result.OriginalPath {
		_ = os.Remove(result.PlayablePath)
	}
}
