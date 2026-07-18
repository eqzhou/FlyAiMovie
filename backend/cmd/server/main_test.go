package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDurationOrAndEnvOr(t *testing.T) {
	if got := durationOr(0, 3*time.Second); got != 3*time.Second {
		t.Fatalf("fallback duration=%v", got)
	}
	if got := durationOr(2, 3*time.Second); got != 2*time.Second {
		t.Fatalf("configured duration=%v", got)
	}
	if got := envOr("FLYAIMOVIE_TEST_MISSING", "fallback"); got != "fallback" {
		t.Fatalf("missing env=%q", got)
	}
	t.Setenv("FLYAIMOVIE_TEST_VALUE", "configured")
	if got := envOr("FLYAIMOVIE_TEST_VALUE", "fallback"); got != "configured" {
		t.Fatalf("configured env=%q", got)
	}
}

func TestFindProjectRootUsesNearestConfigsDirectory(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "configs"), 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "backend", "cmd")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(child); err != nil {
		t.Fatal(err)
	}
	expected, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(findProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	if got != expected {
		t.Fatalf("root=%q want=%q", got, expected)
	}
}
