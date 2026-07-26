package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStorageSaveAndURLs(t *testing.T) {
	store := NewLocal(t.TempDir())
	rel, abs, err := store.SaveBytes("images", "../cover.png", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(rel) != "images" || filepath.Ext(rel) != ".png" {
		t.Fatalf("rel=%q", rel)
	}
	if abs != store.Abs(rel) {
		t.Fatalf("abs mismatch: %q vs %q", abs, store.Abs(rel))
	}
	data, err := os.ReadFile(abs)
	if err != nil || !bytes.Equal(data, []byte("hello")) {
		t.Fatalf("saved data=%q err=%v", data, err)
	}
	if got := store.PublicURL(rel); got != "/static/"+rel {
		t.Fatalf("public URL=%q", got)
	}
	if got := store.PublicURL("/static/" + rel); got != "/static/"+rel {
		t.Fatalf("static URL=%q", got)
	}
}

func TestLocalStorageSaveRejectsSubdirectoryTraversal(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "storage")
	store := NewLocal(root)

	if _, _, err := store.SaveBytes("../escaped", "cover.png", []byte("secret")); err == nil {
		t.Fatal("subdirectory traversal was accepted")
	}
	if entries, err := os.ReadDir(filepath.Join(parent, "escaped")); !os.IsNotExist(err) {
		t.Fatalf("write escaped storage root: entries=%v err=%v", entries, err)
	}
}

func TestLocalStorageSaveRejectsSymlinkedSubdirectoryEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "uploads")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	store := NewLocal(root)

	if _, _, err := store.SaveBytes("uploads", "cover.png", []byte("secret")); err == nil {
		t.Fatal("symlinked subdirectory outside storage root was accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("write escaped through symlink: %v", entries)
	}
}

func TestLocalStorageCreatesPrivateDirectoriesAndFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "storage")
	store := NewLocal(root)
	_, absolute, err := store.SaveBytes("images", "cover.png", []byte("private"))
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{root, filepath.Join(root, "images"), absolute} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got&0o077 != 0 {
			t.Errorf("%s permissions = %#o, want no group/other access", path, got)
		}
	}
}

func TestLocalStorageAbsRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	store := NewLocal(root)
	for _, relative := range []string{"../secret", "images/../../secret", "/static/../../secret", `..\secret`} {
		if absolute := store.Abs(relative); absolute != "" {
			t.Errorf("Abs(%q) = %q, want rejection", relative, absolute)
		}
	}
	if got, want := store.Abs("new/nested/file.bin"), filepath.Join(root, "new", "nested", "file.bin"); got != want {
		t.Errorf("safe non-existing path = %q, want %q", got, want)
	}
}

func TestLocalStorageAbsRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	targets := map[string]string{
		"existing": outside,
		"dangling": filepath.Join(outside, "not-created"),
	}
	for name, target := range targets {
		link := filepath.Join(root, name)
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if absolute := NewLocal(root).Abs(name + "/secret"); absolute != "" {
			t.Errorf("Abs followed %s symlink outside storage root: %q", name, absolute)
		}
	}
}

func TestResolveConfinesPathsToStorageRoot(t *testing.T) {
	root := t.TempDir()
	store := NewLocal(root)
	inside := filepath.Join(root, "videos", "clip.mp4")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	inside, err := filepath.EvalSymlinks(inside)
	if err != nil {
		t.Fatal(err)
	}

	for _, input := range []string{"videos/clip.mp4", "/static/videos/clip.mp4"} {
		resolved, err := store.Resolve(input)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", input, err)
		}
		if resolved != inside {
			t.Fatalf("Resolve(%q) = %q, want %q", input, resolved, inside)
		}
	}

	unsafe := []string{
		"../../etc/passwd",
		"/static/../../etc/passwd",
		"https://evil.example/static/videos/clip.mp4",
		"file:///etc/passwd",
		"%2e%2e/%2e%2e/etc/passwd",
	}
	for _, input := range unsafe {
		if _, err := store.Resolve(input); err == nil {
			t.Errorf("Resolve(%q) succeeded, want rejection", input)
		}
	}
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	store := NewLocal(root)
	if _, err := store.Resolve("link"); err == nil {
		t.Fatal("symlink outside storage root was accepted")
	}
}
