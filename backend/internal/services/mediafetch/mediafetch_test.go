package mediafetch

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/storage"
)

func TestValidateRemoteURLRejectsUnsafeDestinations(t *testing.T) {
	unsafe := []string{
		"file:///etc/passwd",
		"http://127.0.0.1/private",
		"http://localhost/private",
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.1/internal",
		"http://[::1]/private",
		"ftp://example.com/file",
		"https://user:pass@example.com/file",
	}
	for _, rawURL := range unsafe {
		if err := ValidateRemoteURL(context.Background(), rawURL); err == nil {
			t.Errorf("ValidateRemoteURL(%q) succeeded, want rejection", rawURL)
		}
	}
}

func TestValidateImageUpload(t *testing.T) {
	var valid bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	img.Set(0, 0, color.White)
	if err := png.Encode(&valid, img); err != nil {
		t.Fatal(err)
	}

	info, err := ValidateImageUpload(bytes.NewReader(valid.Bytes()), int64(valid.Len()))
	if err != nil {
		t.Fatalf("valid PNG rejected: %v", err)
	}
	if info.MIME != "image/png" || info.Extension != ".png" || info.Width != 32 || info.Height != 24 {
		t.Fatalf("unexpected image info: %+v", info)
	}

	if _, err := ValidateImageUpload(strings.NewReader("<script>alert(1)</script>"), 25); err == nil {
		t.Fatal("HTML payload accepted as image")
	}
	if _, err := ValidateImageUpload(bytes.NewReader(valid.Bytes()), MaxImageUploadBytes+1); err == nil {
		t.Fatal("oversized upload accepted")
	}
}

func TestImportLocalMockFileIsRestricted(t *testing.T) {
	store := storage.NewLocal(t.TempDir())
	allowedDir := filepath.Join(os.TempDir(), "flyaimovie-mock")
	if err := os.MkdirAll(allowedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(allowedDir, "mediafetch-test.png")
	if err := os.WriteFile(allowed, []byte("mock"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(allowed) })

	rel, err := ImportLocalMockFile(store, "file://"+allowed, "images", ".png", MaxImageDownloadBytes)
	if err != nil {
		t.Fatalf("mock file rejected: %v", err)
	}
	if _, err := os.Stat(store.Abs(rel)); err != nil {
		t.Fatalf("imported file missing: %v", err)
	}
	if _, err := ImportLocalMockFile(store, "file:///etc/hosts", "images", ".png", MaxImageDownloadBytes); err == nil {
		t.Fatal("arbitrary local file import accepted")
	}
}

func TestDownloadLocalMockVideoUsesPlayableExtension(t *testing.T) {
	store := storage.NewLocal(t.TempDir())
	allowedDir := filepath.Join(os.TempDir(), "flyaimovie-mock")
	if err := os.MkdirAll(allowedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(allowedDir, "mediafetch-test.mp4")
	if err := os.WriteFile(video, []byte("mock mp4 bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(video) })

	rel, err := Download(context.Background(), store, "file://"+video, "videos", "video")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(rel) != ".mp4" {
		t.Fatalf("mock video extension=%q, want .mp4", filepath.Ext(rel))
	}
}

func TestMediaHelpersAndLimitReader(t *testing.T) {
	for _, tc := range []struct {
		kind, mime, ext string
		ok              bool
	}{
		{"image", "image/png", ".png", true}, {"image", "text/plain", ".bin", false},
		{"video", "video/mp4", ".mp4", true}, {"video", "application/octet-stream", ".mp4", true},
	} {
		if allowedMIME(tc.kind, tc.mime) != tc.ok {
			t.Errorf("allowedMIME(%q,%q)", tc.kind, tc.mime)
		}
		if got := canonicalExtension(tc.kind, tc.mime); got != tc.ext {
			t.Errorf("extension=%q want %q", got, tc.ext)
		}
	}
	reader := &limitReader{reader: strings.NewReader("abcdef"), remaining: 3}
	b := make([]byte, 8)
	n, err := reader.Read(b)
	if n != 3 || err != nil || string(b[:n]) != "abc" {
		t.Fatalf("first read n=%d err=%v data=%q", n, err, b[:n])
	}
	if _, err = reader.Read(b); err != errSizeLimit {
		t.Fatalf("overflow err=%v", err)
	}
}
