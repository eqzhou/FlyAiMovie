package adapters

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MockImageAdapter generates a local placeholder PNG without external APIs.
type MockImageAdapter struct{}

func (a *MockImageAdapter) Name() string { return "mock" }

func (a *MockImageAdapter) Generate(ctx context.Context, cfg AIConfig, in ImageGenInput) (*ImageGenResult, error) {
	dir := filepath.Join(os.TempDir(), "flyaimovie-mock")
	_ = os.MkdirAll(dir, 0o755)
	name := fmt.Sprintf("img_%s.png", uuid.NewString()[:8])
	abs := filepath.Join(dir, name)

	img := image.NewRGBA(image.Rect(0, 0, 1024, 576))
	// simple gradient + bars so it looks intentional
	for y := 0; y < 576; y++ {
		for x := 0; x < 1024; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(30 + x%80),
				G: uint8(40 + y%60),
				B: uint8(70 + (x+y)%90),
				A: 255,
			})
		}
	}
	// panel-like divider for grid-ish look
	for x := 0; x < 1024; x += 256 {
		for y := 0; y < 576; y++ {
			img.Set(x, y, color.RGBA{R: 220, G: 220, B: 230, A: 255})
		}
	}
	for y := 0; y < 576; y += 288 {
		for x := 0; x < 1024; x++ {
			img.Set(x, y, color.RGBA{R: 220, G: 220, B: 230, A: 255})
		}
	}
	f, err := os.Create(abs)
	if err != nil {
		return nil, err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	return &ImageGenResult{IsAsync: false, ImageURL: "file://" + abs}, nil
}

func (a *MockImageAdapter) Poll(ctx context.Context, cfg AIConfig, taskID string) (*ImagePollResult, error) {
	return &ImagePollResult{Status: "failed", Error: "mock image is sync only"}, nil
}

// MockVideoAdapter creates a short silent mp4 via ffmpeg when available.
type MockVideoAdapter struct{}

func (a *MockVideoAdapter) Name() string { return "mock" }

func (a *MockVideoAdapter) Generate(ctx context.Context, cfg AIConfig, in VideoGenInput) (*VideoGenResult, error) {
	dir := filepath.Join(os.TempDir(), "flyaimovie-mock")
	_ = os.MkdirAll(dir, 0o755)
	name := fmt.Sprintf("vid_%s.mp4", uuid.NewString()[:8])
	abs := filepath.Join(dir, name)
	dur := in.Duration
	if dur <= 0 {
		dur = 4
	}
	// Prefer image-to-video if first frame local-ish; else color source
	args := []string{"-y", "-f", "lavfi", "-i", fmt.Sprintf("color=c=0x1a2740:s=1280x720:d=%d", dur),
		"-vf", "drawtext=text='FlyAiMovie mock':x=(w-text_w)/2:y=(h-text_h)/2:fontsize=36:fontcolor=white",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-t", fmt.Sprintf("%d", dur), abs}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		// fallback without drawtext
		args = []string{"-y", "-f", "lavfi", "-i", fmt.Sprintf("color=c=0x243b55:s=1280x720:d=%d", dur),
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-t", fmt.Sprintf("%d", dur), abs}
		cmd = exec.CommandContext(ctx, "ffmpeg", args...)
		if out2, err2 := cmd.CombinedOutput(); err2 != nil {
			return nil, fmt.Errorf("mock video ffmpeg: %w: %s / %s", err2, string(out), string(out2))
		}
	}
	return &VideoGenResult{IsAsync: false, VideoURL: "file://" + abs}, nil
}

func (a *MockVideoAdapter) Poll(ctx context.Context, cfg AIConfig, taskID string) (*VideoPollResult, error) {
	return &VideoPollResult{Status: "failed", Error: "mock video is sync only"}, nil
}

// MockTTSAdapter writes a tiny silent-ish mp3 via ffmpeg sine tone.
type MockTTSAdapter struct{}

func (a *MockTTSAdapter) Name() string { return "mock" }

func (a *MockTTSAdapter) Generate(ctx context.Context, cfg AIConfig, in TTSInput) (*TTSResult, error) {
	dir := filepath.Join(os.TempDir(), "flyaimovie-mock")
	_ = os.MkdirAll(dir, 0o755)
	abs := filepath.Join(dir, fmt.Sprintf("tts_%s.mp3", uuid.NewString()[:8]))
	// short tone as placeholder audio
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=1.2",
		"-c:a", "libmp3lame", abs)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("mock tts: %w: %s", err, string(out))
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	return &TTSResult{AudioBytes: b, Format: "mp3"}, nil
}

// Helper used by generation services to import file:// URLs into storage.
func IsFileURL(u string) bool { return strings.HasPrefix(u, "file://") }

func FileURLPath(u string) string {
	return strings.TrimPrefix(u, "file://")
}

// keep time import used if needed
var _ = time.Now
