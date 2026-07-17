package mediainfo

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseFFProbeOutput(t *testing.T) {
	info, err := Parse([]byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"avg_frame_rate":"30000/1001"}],"format":{"duration":"3.500","format_name":"mov,mp4"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if info.Duration != 3.5 || info.Width != 1920 || info.Height != 1080 || info.Codec != "h264" || info.FrameRate < 29.9 {
		t.Fatalf("info=%+v", info)
	}
}

func TestParseAudioAndInvalidProbeOutput(t *testing.T) {
	info, err := Parse([]byte(`{"streams":[{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"1.25","format_name":"mov"}}`))
	if err != nil || info.Codec != "aac" || info.Duration != 1.25 {
		t.Fatalf("audio info=%+v err=%v", info, err)
	}
	for _, data := range [][]byte{[]byte(`{`), []byte(`{"streams":[],"format":{}}`)} {
		if _, err := Parse(data); err == nil {
			t.Fatalf("invalid probe output accepted: %s", data)
		}
	}
	if parseRate("25") != 25 || parseRate("25/0") != 0 || parseRate("bad") != 0 {
		t.Fatal("unexpected frame-rate parsing")
	}
}

func TestProbeReadsRealMediaAndReportsMissingFile(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is required")
	}
	path := filepath.Join(t.TempDir(), "probe.mp4")
	if output, err := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "color=c=black:s=64x32:d=0.2", "-c:v", "libx264", "-pix_fmt", "yuv420p", path).CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v: %s", err, output)
	}
	info, err := Probe(context.Background(), path)
	if err != nil || info.Width != 64 || info.Height != 32 || info.Duration <= 0 {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	if _, err := Probe(context.Background(), filepath.Join(t.TempDir(), "missing.mp4")); err == nil {
		t.Fatal("missing media accepted")
	}
}
