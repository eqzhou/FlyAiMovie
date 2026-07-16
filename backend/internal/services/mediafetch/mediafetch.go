package mediafetch

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/storage"
)

const (
	MaxImageUploadBytes   int64 = 20 << 20
	MaxImageDownloadBytes int64 = 30 << 20
	MaxVideoDownloadBytes int64 = 500 << 20
	MaxImagePixels              = 100_000_000
)

var errSizeLimit = errors.New("media exceeds size limit")

type ImageInfo struct {
	MIME      string
	Extension string
	Width     int
	Height    int
}

func ValidateRemoteURL(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("invalid media URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported media URL scheme")
	}
	if u.User != nil {
		return fmt.Errorf("media URL credentials are not allowed")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("local media host is not allowed")
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve media host: %w", err)
	}
	if len(addresses) == 0 {
		return fmt.Errorf("media host has no address")
	}
	for _, address := range addresses {
		if isUnsafeIP(address.IP) {
			return fmt.Errorf("private media address is not allowed")
		}
	}
	return nil
}

func Download(ctx context.Context, store *storage.LocalStorage, rawURL, subdir, kind string) (string, error) {
	if strings.HasPrefix(rawURL, "file://") {
		ext := ".mp4"
		limit := MaxVideoDownloadBytes
		if kind == "image" {
			ext = ".png"
			limit = MaxImageDownloadBytes
		}
		return ImportLocalMockFile(store, rawURL, subdir, ext, limit)
	}
	if err := ValidateRemoteURL(ctx, rawURL); err != nil {
		return "", err
	}
	limit := MaxVideoDownloadBytes
	if kind == "image" {
		limit = MaxImageDownloadBytes
	}
	client := &http.Client{
		Timeout: 5 * time.Minute,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 30 * time.Second,
			DialContext:           safeDialContext,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return ValidateRemoteURL(req.Context(), req.URL.String())
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("create media request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download media: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download media: unexpected status %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return "", errSizeLimit
	}
	reader := bufio.NewReader(resp.Body)
	header, _ := reader.Peek(512)
	mime := http.DetectContentType(header)
	if !allowedMIME(kind, mime) {
		return "", fmt.Errorf("unexpected %s content type %q", kind, mime)
	}
	ext := canonicalExtension(kind, mime)
	rel, _, err := store.Save(subdir, "download"+ext, &limitReader{reader: reader, remaining: limit})
	if err != nil {
		return "", err
	}
	return rel, nil
}

func ValidateImageUpload(r io.Reader, declaredSize int64) (ImageInfo, error) {
	if declaredSize < 1 || declaredSize > MaxImageUploadBytes {
		return ImageInfo{}, errSizeLimit
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxImageUploadBytes+1))
	if err != nil {
		return ImageInfo{}, fmt.Errorf("read image: %w", err)
	}
	if int64(len(data)) > MaxImageUploadBytes {
		return ImageInfo{}, errSizeLimit
	}
	mime := http.DetectContentType(data)
	ext := canonicalExtension("image", mime)
	if ext == ".bin" {
		return ImageInfo{}, fmt.Errorf("unsupported image content type %q", mime)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ImageInfo{}, fmt.Errorf("decode image: %w", err)
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 16_384 || config.Height > 16_384 || int64(config.Width)*int64(config.Height) > MaxImagePixels {
		return ImageInfo{}, fmt.Errorf("image dimensions exceed limit")
	}
	return ImageInfo{MIME: mime, Extension: ext, Width: config.Width, Height: config.Height}, nil
}

func ImportLocalMockFile(store *storage.LocalStorage, rawURL, subdir, extension string, maxBytes int64) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "file" || u.Host != "" {
		return "", fmt.Errorf("invalid local mock URL")
	}
	source, err := filepath.EvalSymlinks(filepath.Clean(u.Path))
	if err != nil {
		return "", fmt.Errorf("resolve local mock file: %w", err)
	}
	allowedRoot, err := filepath.EvalSymlinks(filepath.Join(os.TempDir(), "flyaimovie-mock"))
	if err != nil {
		return "", fmt.Errorf("resolve mock root: %w", err)
	}
	if !withinRoot(allowedRoot, source) {
		return "", fmt.Errorf("local file is outside mock directory")
	}
	file, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if stat, err := file.Stat(); err != nil || stat.Size() > maxBytes {
		return "", errSizeLimit
	}
	rel, _, err := store.Save(subdir, "mock"+extension, &limitReader{reader: file, remaining: maxBytes})
	return rel, err
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	for _, candidate := range addresses {
		if isUnsafeIP(candidate.IP) {
			continue
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("media host has no permitted address")
}

func isUnsafeIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

func allowedMIME(kind, mime string) bool {
	if kind == "image" {
		return mime == "image/png" || mime == "image/jpeg" || mime == "image/gif" || mime == "image/webp"
	}
	return strings.HasPrefix(mime, "video/") || mime == "application/octet-stream"
}

func canonicalExtension(kind, mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/webm":
		return ".webm"
	default:
		if kind == "video" && (strings.HasPrefix(mime, "video/") || mime == "application/octet-stream") {
			return ".mp4"
		}
		return ".bin"
	}
}

func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

type limitReader struct {
	reader    io.Reader
	remaining int64
	checked   bool
}

func (r *limitReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		if r.checked {
			return 0, io.EOF
		}
		r.checked = true
		var extra [1]byte
		n, err := r.reader.Read(extra[:])
		if n > 0 {
			return 0, errSizeLimit
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}
