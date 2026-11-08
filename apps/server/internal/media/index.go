package media

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
)

// Remux onto a seekable file so FFmpeg can finalize Xing's duration and TOC.
// Copying the audio packets does not re-encode or change the song's tempo.
func (p *Processor) RemuxIndexed(ctx context.Context, source, output string, duration float64, reserve func(int64) error) (int64, error) {
	info, err := os.Stat(source)
	if err != nil {
		return 0, err
	}
	// No metadata/pictures are copied. The only growth is a small MP3 index
	// frame plus an ID3 header; reserve additional packet/trailer headroom.
	budget := info.Size() + 4096
	if err = reserve(budget); err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, p.FFmpeg, "-v", "error", "-nostdin", "-n", "-i", source, "-map", "0:a:0", "-map_metadata", "-1", "-c:a", "copy", "-write_xing", "1", "-fs", strconv.FormatInt(info.Size()+2048, 10), "-f", "mp3", output)
	if stderr, runErr := cmd.CombinedOutput(); runErr != nil {
		return 0, fmt.Errorf("补写 MP3 索引失败: %w: %.2000s", runErr, stderr)
	}
	file, err := os.OpenFile(output, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil {
		return 0, syncErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	meta, err := p.probe(ctx, output)
	if err != nil {
		return 0, err
	}
	if meta.codec != "mp3" || math.Abs(meta.duration-duration) > .15 {
		return 0, fmt.Errorf("MP3 时间索引校验失败: 副本 %.3f 秒，原文件 %.3f 秒", meta.duration, duration)
	}
	info, err = os.Stat(output)
	if err != nil {
		return 0, err
	}
	if info.Size() > budget {
		return 0, fmt.Errorf("MP3 索引副本超过预留空间")
	}
	return info.Size(), nil
}
