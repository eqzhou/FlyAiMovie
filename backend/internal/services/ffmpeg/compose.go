package ffmpeg

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ComposeShot muxes video + optional audio into one file.
func ComposeShot(videoPath, audioPath, subtitlePath, outPath string) error {
	return ComposeShotContext(context.Background(), videoPath, audioPath, subtitlePath, outPath)
}

// ComposeShotContext muxes media with an optional subtitle track and honors cancellation.
func ComposeShotContext(ctx context.Context, videoPath, audioPath, subtitlePath, outPath string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	args := []string{"-y", "-i", videoPath}
	if audioPath != "" {
		args = append(args, "-i", audioPath)
	}
	if subtitlePath != "" {
		args = append(args, "-i", subtitlePath)
	}
	// All inputs must be declared before output options. Map streams explicitly
	// so an external voice track replaces source audio while subtitles remain.
	args = append(args, "-map", "0:v:0")
	if audioPath != "" {
		args = append(args, "-map", "1:a:0", "-c:v", "copy", "-c:a", "aac", "-shortest")
	} else {
		args = append(args, "-map", "0:a?", "-c:v", "copy", "-c:a", "copy")
	}
	if subtitlePath != "" {
		subtitleInput := "1:s:0"
		if audioPath != "" {
			subtitleInput = "2:s:0"
		}
		args = append(args, "-map", subtitleInput, "-c:s", "mov_text")
	}
	tmp, err := tempOutput(outPath)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	args = append(args, tmp)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg compose: %w: %s", err, string(out))
	}
	if err := os.Rename(tmp, outPath); err != nil {
		return fmt.Errorf("publish composed output: %w", err)
	}
	return nil
}

// MergeEpisode concatenates shot videos with concat demuxer.
func MergeEpisode(paths []string, outPath string) error {
	return MergeEpisodeContext(context.Background(), paths, outPath)
}

func MergeEpisodeContext(ctx context.Context, paths []string, outPath string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no input videos")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	listFile := filepath.Join(filepath.Dir(outPath), ".concat-"+uuid.NewString()+".txt")
	var b strings.Builder
	for _, p := range paths {
		abs, _ := filepath.Abs(p)
		b.WriteString("file '")
		b.WriteString(strings.ReplaceAll(abs, "'", "'\\''"))
		b.WriteString("'\n")
	}
	if err := os.WriteFile(listFile, []byte(b.String()), 0o644); err != nil {
		return err
	}
	defer os.Remove(listFile)
	tmp, err := tempOutput(outPath)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", listFile, "-c", "copy", tmp)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// fallback re-encode
		cmd = exec.CommandContext(ctx, "ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", listFile, "-c:v", "libx264", "-c:a", "aac", tmp)
		out, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ffmpeg merge: %w: %s", err, string(out))
		}
	}
	if err := os.Rename(tmp, outPath); err != nil {
		return fmt.Errorf("publish merged output: %w", err)
	}
	return nil
}

func tempOutput(outPath string) (string, error) {
	if outPath == "" {
		return "", fmt.Errorf("empty output path")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(outPath), ".tmp-"+uuid.NewString()+filepath.Ext(outPath)), nil
}

// SplitGrid crops an N x M grid image into individual cells using ffmpeg/crop.
func SplitGrid(input string, rows, cols int, outDir string) ([]string, error) {
	if rows <= 0 || cols <= 0 {
		return nil, fmt.Errorf("invalid grid size")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	// Probe size
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height", "-of", "csv=p=0:s=x", input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w: %s", err, string(out))
	}
	var w, h int
	fmt.Sscanf(strings.TrimSpace(string(out)), "%dx%d", &w, &h)
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("invalid image size")
	}
	cw, ch := w/cols, h/rows
	paths := make([]string, 0, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			outPath := filepath.Join(outDir, fmt.Sprintf("cell_%02d.png", r*cols+c+1))
			x, y := c*cw, r*ch
			filter := fmt.Sprintf("crop=%d:%d:%d:%d", cw, ch, x, y)
			cmd := exec.Command("ffmpeg", "-y", "-i", input, "-vf", filter, outPath)
			if o, err := cmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("split cell: %w: %s", err, string(o))
			}
			paths = append(paths, outPath)
		}
	}
	return paths, nil
}
