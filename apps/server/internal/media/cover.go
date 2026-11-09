package media

import (
	"bytes"
	"context"
	"encoding/json"
	"image/jpeg"
	"os"
	"os/exec"
	"strconv"
)

// Missing or unreadable artwork is optional. Unremoved partial files are fatal
// so their reservation and durable job remain available for cleanup retries.
func (p *Processor) ExtractCover(ctx context.Context, source, output string, reserve func(int64) error) (string, int64, error) {
	var probe struct {
		Streams []struct {
			Index       int `json:"index"`
			Disposition struct {
				Attached int `json:"attached_pic"`
			} `json:"disposition"`
		} `json:"streams"`
	}
	raw, err := exec.CommandContext(ctx, p.FFprobe, "-v", "error", "-show_entries", "stream=index:stream_disposition=attached_pic", "-of", "json", source).Output()
	if err != nil || json.Unmarshal(raw, &probe) != nil {
		return "", 0, nil
	}
	index := -1
	for _, stream := range probe.Streams {
		if stream.Disposition.Attached == 1 {
			index = stream.Index
			break
		}
	}
	if index < 0 {
		return "", 0, nil
	}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", 0, err
	}
	writer := &reservedWriter{writer: file, reserve: reserve}
	cmd := exec.CommandContext(ctx, p.FFmpeg, "-v", "error", "-nostdin", "-i", source, "-map", "0:"+strconv.Itoa(index), "-frames:v", "1", "-vf", "scale=512:512:force_original_aspect_ratio=decrease", "-c:v", "mjpeg", "-q:v", "4", "-threads", "1", "-f", "image2pipe", "pipe:1")
	cmd.Stdout = writer
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr == nil {
		runErr = file.Sync()
	}
	closeErr := file.Close()
	valid := runErr == nil && closeErr == nil && writer.err == nil
	var size int64
	if valid {
		image, openErr := os.Open(output)
		if openErr != nil {
			valid = false
		} else {
			config, decodeErr := jpeg.DecodeConfig(image)
			image.Close()
			valid = decodeErr == nil && config.Width > 0 && config.Height > 0 && config.Width <= 512 && config.Height <= 512
		}
		if info, statErr := os.Stat(output); statErr == nil {
			size = info.Size()
		} else {
			valid = false
		}
	}
	if !valid {
		if err = os.Remove(output); err != nil {
			return "", 0, err
		}
		return "", 0, nil
	}
	return output, size, nil
}
