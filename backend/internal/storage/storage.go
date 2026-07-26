package storage

import (
	"bytes"
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
	_ = os.MkdirAll(root, 0o700)
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
	localPath := filepath.Join(filepath.FromSlash(subdir), name)
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return "", "", err
	}
	defer root.Close()
	if err := root.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
		return "", "", fmt.Errorf("create storage directory: %w", err)
	}
	f, err := root.OpenFile(localPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("create storage file: %w", err)
	}
	completed := false
	defer func() {
		if closeErr := f.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close storage file: %w", closeErr)
		}
		if !completed || err != nil {
			_ = root.Remove(localPath)
			relPath, absPath = "", ""
		}
	}()
	if _, err := io.Copy(f, r); err != nil {
		return "", "", err
	}
	completed = true
	relPath = filepath.ToSlash(localPath)
	absPath = filepath.Join(s.Root, localPath)
	return relPath, absPath, nil
}

func (s *LocalStorage) SaveBytes(subdir, filename string, data []byte) (relPath string, absPath string, err error) {
	return s.Save(subdir, filename, bytes.NewReader(data))
}

func (s *LocalStorage) Abs(rel string) string {
	rel = strings.TrimPrefix(rel, "/")
	rel = strings.TrimPrefix(rel, "static/")
	if strings.Contains(rel, "\\") {
		return ""
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == "" || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return ""
	}
	candidate := filepath.Join(s.Root, clean)
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return ""
	}
	defer root.Close()
	parts := strings.Split(filepath.ToSlash(clean), "/")
	for index := range parts {
		prefix := filepath.Join(parts[:index+1]...)
		info, statErr := root.Lstat(prefix)
		if os.IsNotExist(statErr) {
			return candidate
		}
		if statErr != nil {
			return ""
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ""
		}
	}
	return candidate
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
