package storage

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type LocalStorage struct {
	Root string
}

func NewLocal(root string) *LocalStorage {
	_ = os.MkdirAll(root, 0o755)
	return &LocalStorage{Root: root}
}

func (s *LocalStorage) Save(subdir, filename string, r io.Reader) (relPath string, absPath string, err error) {
	safe := filepath.Base(filename)
	if safe == "." || safe == "/" || safe == "" {
		safe = "file.bin"
	}
	// preserve extension
	ext := filepath.Ext(safe)
	name := fmt.Sprintf("%s_%s%s", time.Now().Format("20060102150405"), uuid.NewString()[:8], ext)
	rel := filepath.ToSlash(filepath.Join(subdir, name))
	abs := filepath.Join(s.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", "", err
	}
	f, err := os.Create(abs)
	if err != nil {
		return "", "", err
	}
	completed := false
	defer func() {
		_ = f.Close()
		if !completed {
			_ = os.Remove(abs)
		}
	}()
	if _, err := io.Copy(f, r); err != nil {
		return "", "", err
	}
	completed = true
	return rel, abs, nil
}

func (s *LocalStorage) SaveBytes(subdir, filename string, data []byte) (relPath string, absPath string, err error) {
	return s.Save(subdir, filename, strings.NewReader(string(data)))
}

func (s *LocalStorage) Abs(rel string) string {
	rel = strings.TrimPrefix(rel, "/")
	rel = strings.TrimPrefix(rel, "static/")
	return filepath.Join(s.Root, filepath.FromSlash(rel))
}

// Resolve returns an existing file inside the storage root.
func (s *LocalStorage) Resolve(publicOrRel string) (string, error) {
	parsed, err := url.Parse(publicOrRel)
	if err != nil {
		return "", fmt.Errorf("invalid storage path")
	}
	if parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil {
		return "", fmt.Errorf("external storage URL is not allowed")
	}
	pathValue, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return "", fmt.Errorf("invalid escaped storage path")
	}
	if strings.Contains(pathValue, "\\") {
		return "", fmt.Errorf("invalid storage path separator")
	}
	pathValue = strings.TrimPrefix(pathValue, "/")
	pathValue = strings.TrimPrefix(pathValue, "static/")
	clean := filepath.Clean(filepath.FromSlash(pathValue))
	if clean == "." || clean == "" || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", fmt.Errorf("storage path escapes root")
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(root, clean)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("storage file not found")
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("storage path escapes root")
	}
	info, err := os.Stat(resolved)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("storage file not found")
	}
	return resolved, nil
}

func (s *LocalStorage) PublicURL(rel string) string {
	rel = strings.TrimPrefix(rel, "/")
	if strings.HasPrefix(rel, "static/") {
		return "/" + rel
	}
	return "/static/" + rel
}
