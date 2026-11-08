package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

type Result struct {
	ID, Title, Artist, Album, OriginalPath, PlayablePath, ContentType, OriginalContentType string
	SizeBytes                                                                              int64
	DurationSeconds                                                                        float64
}

type Processor struct {
	Root, FFmpeg, FFprobe string
}

type probeOutput struct {
	Format struct {
		Duration string            `json:"duration"`
		Name     string            `json:"format_name"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecName string `json:"codec_name"`
		CodecType string `json:"codec_type"`
		Channels  int    `json:"channels"`
	} `json:"streams"`
}

func New(root string) *Processor { return &Processor{Root: root, FFmpeg: "ffmpeg", FFprobe: "ffprobe"} }

func (p *Processor) InspectTitle(ctx context.Context, path, name string) (string, error) {
	output, err := exec.CommandContext(ctx, p.FFprobe, "-v", "error", "-show_entries", "format_tags=title:stream=codec_type,channels", "-of", "json", path).Output()
	if err != nil {
		return "", fmt.Errorf("无法读取音频标题: %w", err)
	}
	var probe probeOutput
	if err = json.Unmarshal(output, &probe); err != nil {
		return "", err
	}
	for _, stream := range probe.Streams {
		if stream.CodecType == "audio" && stream.Channels > 0 {
			return first(probe.Format.Tags, "title", strings.TrimSuffix(name, extension(name))), nil
		}
	}
	return "", errors.New("未找到音频轨道")
}

type metadata struct {
	duration    float64
	tags        map[string]string
	codec       string
	contentType string
}

func (p *Processor) probe(ctx context.Context, path string) (metadata, error) {
	cmd := exec.CommandContext(ctx, p.FFprobe, "-v", "error", "-show_entries", "format=format_name,duration:format_tags=title,artist,album:stream=codec_name,codec_type", "-of", "json", path)
	b, err := cmd.Output()
	if err != nil {
		return metadata{}, err
	}
	var out probeOutput
	if err = json.Unmarshal(b, &out); err != nil {
		return metadata{}, err
	}
	var d float64
	if out.Format.Name == "aac" {
		d, err = p.decodedDuration(ctx, path)
	} else {
		_, err = fmt.Sscanf(out.Format.Duration, "%f", &d)
	}
	if err != nil {
		return metadata{}, err
	}
	if d <= 0 || math.IsNaN(d) || math.IsInf(d, 0) {
		return metadata{}, errors.New("音频时长无效")
	}
	for _, stream := range out.Streams {
		if stream.CodecType == "audio" && stream.CodecName != "" {
			return metadata{duration: d, tags: out.Format.Tags, codec: strings.ToLower(stream.CodecName), contentType: originalMIME(out.Format.Name, stream.CodecName)}, nil
		}
	}
	return metadata{}, errors.New("未找到音频轨道")
}

func (p *Processor) InspectOriginal(ctx context.Context, path string) (string, float64, error) {
	meta, err := p.probe(ctx, path)
	return meta.contentType, meta.duration, err
}

// Raw ADTS has no authoritative duration; bitrate estimates can be wildly wrong.
func (p *Processor) decodedDuration(ctx context.Context, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, p.FFmpeg, "-v", "error", "-nostdin", "-xerror", "-i", path, "-map", "0:a:0", "-vn", "-f", "null", "-", "-progress", "pipe:1", "-nostats")
	var progress, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &progress, &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("解码音频时长失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var duration float64
	for _, line := range strings.Split(progress.String(), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "out_time_us="); ok {
			if micros, err := strconv.ParseInt(value, 10, 64); err == nil && micros > 0 {
				duration = float64(micros) / 1e6
			}
		}
	}
	if duration <= 0 {
		return 0, errors.New("未能读取解码音频时长")
	}
	return duration, nil
}

func originalMIME(format, codec string) string {
	switch {
	case format == "flac":
		return "audio/flac"
	case format == "mp3":
		return "audio/mpeg"
	case format == "ogg":
		if codec == "opus" {
			return "audio/ogg; codecs=opus"
		}
		return "audio/ogg"
	case format == "wav":
		return "audio/wav"
	case format == "aac":
		return "audio/aac"
	case strings.Contains(format, "mp4"):
		return "audio/mp4"
	case format == "asf":
		return "audio/x-ms-wma"
	default:
		return "application/octet-stream"
	}
}

func first(tags map[string]string, key, fallback string) string {
	for k, v := range tags {
		if strings.EqualFold(k, key) && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return fallback
}
