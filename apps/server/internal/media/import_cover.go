package media

import (
	"context"
	"encoding/json"
	"errors"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"os/exec"
)

const MaxCoverBytes int64 = 8 * 1024 * 1024

func (p *Processor) NormalizeCover(ctx context.Context, raw, output string, reserve func(int64) error) (int64, error) {
	// Reject playlists and other non-image formats before invoking a media parser.
	header, err := os.Open(raw)
	if err != nil {
		return 0, err
	}
	prefix := make([]byte, 512)
	count, readErr := header.Read(prefix)
	header.Close()
	if readErr != nil && readErr != io.EOF {
		return 0, readErr
	}
	switch http.DetectContentType(prefix[:count]) {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp":
	default:
		return 0, errors.New("封面不是受支持的图片")
	}
	data, err := exec.CommandContext(ctx, p.FFprobe, "-v", "error", "-protocol_whitelist", "file,pipe", "-show_entries", "stream=codec_name,width,height", "-of", "json", raw).Output()
	if err != nil {
		return 0, errors.New("封面不是可读取的图片")
	}
	var probe struct {
		Streams []struct {
			Codec         string `json:"codec_name"`
			Width, Height int
		} `json:"streams"`
	}
	if json.Unmarshal(data, &probe) != nil || len(probe.Streams) != 1 {
		return 0, errors.New("封面格式无效")
	}
	image := probe.Streams[0]
	if image.Width <= 0 || image.Height <= 0 || int64(image.Width) > (32*1024*1024)/int64(image.Height) {
		return 0, errors.New("封面分辨率过大或无效")
	}
	switch image.Codec {
	case "mjpeg", "png", "webp", "gif", "bmp":
	default:
		return 0, errors.New("封面格式不受支持")
	}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}
	writer := &reservedWriter{writer: file, reserve: reserve}
	command := exec.CommandContext(ctx, p.FFmpeg, "-v", "error", "-nostdin", "-protocol_whitelist", "file,pipe", "-i", raw, "-map", "0:v:0", "-frames:v", "1", "-vf", "scale=512:512:force_original_aspect_ratio=decrease", "-c:v", "mjpeg", "-q:v", "4", "-threads", "1", "-f", "image2pipe", "pipe:1")
	command.Stdout = writer
	err = command.Run()
	if writer.err != nil {
		err = writer.err
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return 0, err
	}
	if closeErr != nil {
		return 0, closeErr
	}
	result, err := os.Open(output)
	if err != nil {
		return 0, err
	}
	config, err := jpeg.DecodeConfig(result)
	result.Close()
	if err != nil || config.Width > 512 || config.Height > 512 {
		return 0, errors.New("封面图片转换失败")
	}
	info, err := os.Stat(output)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
