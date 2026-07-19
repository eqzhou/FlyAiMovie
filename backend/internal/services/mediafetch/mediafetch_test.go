package mediafetch

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/storage"
)

func TestDownloadAuthorizedSendsBearerAndStoresVideo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p'}, bytes.Repeat([]byte{1}, 600)...))
	}))
	defer server.Close()

	store := storage.NewLocal(t.TempDir())
	client := server.Client()
	rel, err := downloadAuthorizedWithClient(context.Background(), client, store, server.URL+"/content", "videos", "video", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(rel) != ".mp4" {
		t.Fatalf("extension=%q", filepath.Ext(rel))
	}
}

func TestDownloadAuthorizedRejectsCrossAuthorityRedirect(t *testing.T) {
	var targetAuthorization string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p'}, bytes.Repeat([]byte{1}, 600)...))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/content", http.StatusFound)
	}))
	defer source.Close()

	store := storage.NewLocal(t.TempDir())
	_, err := downloadAuthorizedWithClient(context.Background(), source.Client(), store, source.URL+"/content", "videos", "video", "secret")
	if err == nil || !strings.Contains(err.Error(), "redirect changed host") {
		t.Fatalf("cross-authority redirect was not rejected: %v", err)
	}
	if targetAuthorization != "" {
		t.Fatalf("bearer token reached redirect target: %q", targetAuthorization)
	}
}

func TestDownloadRejectsStatusTypeAndDeclaredSize(t *testing.T) {
	store := storage.NewLocal(t.TempDir())
	for name, handler := range map[string]http.HandlerFunc{
		"status": func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusBadGateway) },
		"mime":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("plain text")) },
		"size": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", "40000000")
			w.WriteHeader(http.StatusOK)
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			if _, err := downloadAuthorizedWithClient(context.Background(), server.Client(), store, server.URL, "images", "image", ""); err == nil {
				t.Fatal("invalid download accepted")
			}
		})
	}
	if _, err := DownloadAuthorized(context.Background(), store, "http://127.0.0.1/media", "videos", "video", "token"); err == nil {
		t.Fatal("private authorized download accepted")
	}
}

func TestMediaHTTPClientDoesNotUseEnvironmentProxy(t *testing.T) {
	client := mediaHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type=%T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("media client inherited a process proxy")
	}
}

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
	if _, err := ValidateImageUpload(bytes.NewReader(valid.Bytes()), 0); err == nil {
		t.Fatal("empty upload accepted")
	}
	if _, err := ValidateImageUpload(bytes.NewReader([]byte("\x89PNG\r\n\x1a\ninvalid")), 15); err == nil {
		t.Fatal("malformed PNG accepted")
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
		{"image", "image/jpeg", ".jpg", true}, {"image", "image/gif", ".gif", true}, {"image", "image/webp", ".webp", true},
		{"video", "video/mp4", ".mp4", true}, {"video", "video/webm", ".webm", true}, {"video", "application/octet-stream", ".mp4", true},
		{"audio", "audio/mpeg", ".mp3", true}, {"audio", "audio/wav", ".wav", true}, {"audio", "audio/ogg", ".ogg", true},
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

func TestSafeDialAndInvalidLocalMockInputs(t *testing.T) {
	if _, err := safeDialContext(context.Background(), "tcp", "127.0.0.1:80"); err == nil {
		t.Fatal("safe dial accepted loopback")
	}
	store := storage.NewLocal(t.TempDir())
	for _, raw := range []string{"https://example.test/file", "file://host/tmp/file", "not-a-url"} {
		if _, err := ImportLocalMockFile(store, raw, "images", ".png", 10); err == nil {
			t.Fatalf("invalid local mock URL %q accepted", raw)
		}
	}
}
