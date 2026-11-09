package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Process operates on a durable uploaded file. Output is streamed through a
// caller-owned writer that reserves quota before allowing bytes onto disk.
func (p *Processor) Process(ctx context.Context, id, name, source, output string, reserve func(int64) error) (Result, error) {
	meta, err := p.probe(ctx, source)
	if err != nil {
		return Result{}, fmt.Errorf("无法读取音频: %w", err)
	}
	info, err := os.Stat(source)
	if err != nil {
		return Result{}, err
	}
	result := Result{ID: id, Title: first(meta.tags, "title", strings.TrimSuffix(name, extension(name))), Artist: first(meta.tags, "artist", ""), Album: first(meta.tags, "album", ""), OriginalPath: source, PlayablePath: source, ContentType: "audio/mpeg", OriginalContentType: meta.contentType, SizeBytes: info.Size(), DurationSeconds: meta.duration}
	if meta.codec == "mp3" {
		return p.withCover(ctx, result, source, output, reserve)
	}
	// Reserve an estimated upper bound first; expand atomically before writing
	// if actual output exceeds it. Unused bytes are settled on activation.
	estimate := int64(meta.duration*40000) + 65536
	if err = reserve(estimate); err != nil {
		return Result{}, err
	}
	stream := output + ".stream"
	file, err := os.OpenFile(stream, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Result{}, err
	}
	writer := &reservedWriter{writer: file, reserve: reserve, available: estimate}
	cmd := exec.CommandContext(ctx, p.FFmpeg, "-v", "error", "-nostdin", "-i", source, "-map", "0:a:0", "-vn", "-c:a", "libmp3lame", "-q:a", "2", "-ac", "2", "-ar", "48000", "-f", "mp3", "pipe:1")
	cmd.Stdout = writer
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr == nil {
		runErr = file.Sync()
	}
	closeErr := file.Close()
	if writer.err != nil {
		return Result{}, writer.err
	}
	if runErr != nil {
		return Result{}, fmt.Errorf("转码失败: %w: %.2000s", runErr, stderr.String())
	}
	if closeErr != nil {
		return Result{}, closeErr
	}
	size, err := p.RemuxIndexed(ctx, stream, output, meta.duration, reserve)
	if err != nil {
		return Result{}, err
	}
	if err = os.Remove(stream); err != nil {
		return Result{}, err
	}
	result.PlayablePath = output
	result.SizeBytes += size
	return p.withCover(ctx, result, source, output, reserve)
}

type reservedWriter struct {
	writer    io.Writer
	reserve   func(int64) error
	available int64
	err       error
}

func (w *reservedWriter) Write(data []byte) (int, error) {
	if extra := int64(len(data)) - w.available; extra > 0 {
		if w.err = w.reserve(extra); w.err != nil {
			return 0, w.err
		}
		w.available += extra
	}
	n, err := w.writer.Write(data)
	w.available -= int64(n)
	return n, err
}

func extension(name string) string {
	if index := strings.LastIndex(name, "."); index >= 0 {
		return name[index:]
	}
	return ""
}

func (p *Processor) withCover(ctx context.Context, result Result, source, output string, reserve func(int64) error) (Result, error) {
	path, size, err := p.ExtractCover(ctx, source, output+".cover.jpg", reserve)
	if err != nil {
		return Result{}, err
	}
	result.CoverPath = path
	result.SizeBytes += size
	return result, nil
}
