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
