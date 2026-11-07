package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
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
	Streams []struct {
		CodecName string `json:"codec_name"`
		CodecType string `json:"codec_type"`
	} `json:"streams"`
}

func New(root string) *Processor { return &Processor{Root: root, FFmpeg: "ffmpeg", FFprobe: "ffprobe"} }

type metadata struct {
	duration float64
	tags     map[string]string
	codec    string
}

func (p *Processor) probe(ctx context.Context, path string) (metadata, error) {
	cmd := exec.CommandContext(ctx, p.FFprobe, "-v", "error", "-show_entries", "format=duration:format_tags=title,artist,album:stream=codec_name,codec_type", "-of", "json", path)
	b, err := cmd.Output()
	if err != nil {
		return metadata{}, err
	}
	var out probeOutput
	if err = json.Unmarshal(b, &out); err != nil {
		return metadata{}, err
	}
	var d float64
	if _, err = fmt.Sscanf(out.Format.Duration, "%f", &d); err != nil || d <= 0 || math.IsNaN(d) || math.IsInf(d, 0) {
		return metadata{}, errors.New("音频时长无效")
	}
	for _, stream := range out.Streams {
		if stream.CodecType == "audio" && stream.CodecName != "" {
			return metadata{duration: d, tags: out.Format.Tags, codec: strings.ToLower(stream.CodecName)}, nil
		}
	}
	return metadata{}, errors.New("未找到音频轨道")
}

func first(tags map[string]string, key, fallback string) string {
	for k, v := range tags {
		if strings.EqualFold(k, key) && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return fallback
}
