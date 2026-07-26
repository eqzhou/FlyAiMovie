package mediafetch

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestDownloadAuthorizedRejectsHTTPSDowngradeOnSameAuthority(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(req, http.StatusFound, map[string]string{"Location": "http://8.8.8.8/content"}, nil), nil
		}
		body := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p'}, bytes.Repeat([]byte{1}, 600)...)
		return response(req, http.StatusOK, map[string]string{"Content-Type": "video/mp4"}, body), nil
	})}

	store := storage.NewLocal(t.TempDir())
	_, err := downloadAuthorizedWithClient(context.Background(), client, store, "https://8.8.8.8/content", "videos", "video", "secret")
	if err == nil || !strings.Contains(err.Error(), "redirect changed") {
		t.Fatalf("HTTPS downgrade redirect was not rejected: %v", err)
	}
	if calls != 1 {
		t.Fatalf("round trips=%d, want 1", calls)
	}
}

func TestDownloadDoesNotMutateCallerHTTPClient(t *testing.T) {
	originalRedirect := func(_ *http.Request, _ []*http.Request) error { return nil }
	client := &http.Client{
		CheckRedirect: originalRedirect,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p'}, bytes.Repeat([]byte{1}, 600)...)
			return response(req, http.StatusOK, map[string]string{"Content-Type": "video/mp4"}, body), nil
		}),
	}

	store := storage.NewLocal(t.TempDir())
	if _, err := downloadAuthorizedWithClient(context.Background(), client, store, "https://8.8.8.8/content", "videos", "video", ""); err != nil {
		t.Fatal(err)
	}
	if reflect.ValueOf(client.CheckRedirect).Pointer() != reflect.ValueOf(originalRedirect).Pointer() {
		t.Fatal("download mutated the caller's redirect policy")
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
	if transport.IdleConnTimeout <= 0 || transport.TLSHandshakeTimeout <= 0 {
		t.Fatal("media transport does not bound idle connections and TLS handshakes")
	}
	if other := mediaHTTPClient(); other.Transport != client.Transport {
		t.Fatal("media clients do not share a bounded connection pool")
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
		"http://8.8.8.8:0/file",
		"http://[::127.0.0.1]/private",
		"http://[64:ff9b::7f00:1]/private",
	}
	for _, rawURL := range unsafe {
		if err := ValidateRemoteURL(context.Background(), rawURL); err == nil {
			t.Errorf("ValidateRemoteURL(%q) succeeded, want rejection", rawURL)
		}
	}
}

func TestDownloadClosesResponseBodyAndPropagatesContext(t *testing.T) {
	type contextKey string
	const key contextKey = "request-id"
	body := &trackingBody{Reader: strings.NewReader("failure")}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Context().Value(key); got != "ctx-123" {
			t.Fatalf("request context value=%v", got)
		}
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     make(http.Header),
			Body:       body,
			Request:    req,
		}, nil
	})}

	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), key, "ctx-123"), time.Second)
	defer cancel()
	store := storage.NewLocal(t.TempDir())
	if _, err := downloadAuthorizedWithClient(ctx, client, store, "https://8.8.8.8/content", "videos", "video", ""); err == nil {
		t.Fatal("error response was accepted")
	}
	if !body.closed {
		t.Fatal("response body was not closed")
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

func TestSafeDialRevalidatesAndPinsDNSAnswers(t *testing.T) {
	t.Run("private rebinding answer", func(t *testing.T) {
		dialed := false
		lookup := func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}
		dial := func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, errors.New("unexpected dial")
		}
		if _, err := safeDialContextWith(context.Background(), "tcp", "media.example:443", lookup, dial); err == nil {
			t.Fatal("private rebound address was accepted")
		}
		if dialed {
			t.Fatal("dialer was called for a private rebound address")
		}
	})

	t.Run("public answer", func(t *testing.T) {
		type contextKey string
		const key contextKey = "trace"
		ctx := context.WithValue(context.Background(), key, "trace-1")
		lookup := func(got context.Context, host string) ([]net.IPAddr, error) {
			if got.Value(key) != "trace-1" || host != "media.example" {
				t.Fatalf("lookup context/host = %v/%q", got.Value(key), host)
			}
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		}
		wantErr := errors.New("dial stopped")
		dial := func(got context.Context, network, address string) (net.Conn, error) {
			if got.Value(key) != "trace-1" || network != "tcp" || address != "8.8.8.8:443" {
				t.Fatalf("dial context/network/address = %v/%q/%q", got.Value(key), network, address)
			}
			return nil, wantErr
		}
		if _, err := safeDialContextWith(ctx, "tcp", "media.example:443", lookup, dial); !errors.Is(err, wantErr) {
			t.Fatalf("dial error was not preserved: %v", err)
		}
	})

	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		lookup := func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		}
		dialed := false
		dial := func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, nil
		}
		if _, err := safeDialContextWith(ctx, "tcp", "media.example:443", lookup, dial); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation was not preserved: %v", err)
		}
		if dialed {
			t.Fatal("dialer was called after context cancellation")
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

func response(req *http.Request, status int, headers map[string]string, body []byte) *http.Response {
	header := make(http.Header, len(headers))
	for key, value := range headers {
		header.Set(key, value)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}
