package ffmpeg

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, binary := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skip(binary + " is required")
		}
	}
}

func runMediaCommand(t *testing.T, args ...string) {
	t.Helper()
	output, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg %v: %v: %s", args, err, output)
	}
}

func testVideo(t *testing.T, path, shade string) {
	t.Helper()
	runMediaCommand(t, "-y", "-f", "lavfi", "-i", "color=c="+shade+":s=160x90:d=0.3", "-c:v", "libx264", "-pix_fmt", "yuv420p", path)
}

func TestComposeShotAndMergeEpisode(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	videoA, videoB := filepath.Join(dir, "a.mp4"), filepath.Join(dir, "b.mp4")
	testVideo(t, videoA, "blue")
	testVideo(t, videoB, "red")
	audio := filepath.Join(dir, "voice.wav")
	runMediaCommand(t, "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.3", audio)
	subtitle := filepath.Join(dir, "subtitle.srt")
	if err := os.WriteFile(subtitle, []byte("1\n00:00:00,000 --> 00:00:00,250\n独立测试字幕\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	shotA := filepath.Join(dir, "out", "shot-a.mp4")
	if err := ComposeShot(videoA, audio, subtitle, shotA); err != nil {
		t.Fatal(err)
	}
	if stat, err := os.Stat(shotA); err != nil || stat.Size() == 0 {
		t.Fatalf("composed output missing: %v", err)
	}
	probe, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_type", "-of", "csv=p=0", shotA).CombinedOutput()
	if err != nil {
		t.Fatalf("probe: %v: %s", err, probe)
	}
	streams := string(probe)
	if !strings.Contains(streams, "video") || !strings.Contains(streams, "audio") || !strings.Contains(streams, "subtitle") {
		t.Fatalf("unexpected streams: %s", streams)
	}

	shotB := filepath.Join(dir, "out", "shot-b.mp4")
	if err := ComposeShot(videoB, "", "", shotB); err != nil {
		t.Fatal(err)
	}
	merged := filepath.Join(dir, "merged", "episode.mp4")
	if err := MergeEpisode([]string{shotA, shotB}, merged); err != nil {
		t.Fatal(err)
	}
	if stat, err := os.Stat(merged); err != nil || stat.Size() == 0 {
		t.Fatalf("merged output missing: %v", err)
	}
}

func TestComposeAndMergeValidationAndCancellation(t *testing.T) {
	requireFFmpeg(t)
	if err := MergeEpisode(nil, filepath.Join(t.TempDir(), "out.mp4")); err == nil {
		t.Fatal("expected empty input error")
	}
	if _, err := tempOutput(""); err == nil {
		t.Fatal("expected empty output error")
	}
	video := filepath.Join(t.TempDir(), "input.mp4")
	testVideo(t, video, "black")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := filepath.Join(t.TempDir(), "canceled.mp4")
	if err := ComposeShotContext(ctx, video, "", "", out); err == nil {
		t.Fatal("expected cancellation error")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("canceled output was published: %v", err)
	}
}

func TestSplitGrid(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	input := filepath.Join(dir, "grid.png")
	img := image.NewRGBA(image.Rect(0, 0, 80, 40))
	colors := []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 0, 255}}
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, colors[(y/20)*2+x/40])
		}
	}
	file, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, img); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	paths, err := SplitGrid(input, 2, 2, filepath.Join(dir, "cells"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 4 {
		t.Fatalf("cells=%d want 4", len(paths))
	}
	for _, path := range paths {
		if stat, err := os.Stat(path); err != nil || stat.Size() == 0 {
			t.Fatalf("cell missing: %s %v", path, err)
		}
	}
	if _, err := SplitGrid(input, 0, 2, filepath.Join(dir, "invalid")); err == nil {
		t.Fatal("expected invalid grid error")
	}
}
