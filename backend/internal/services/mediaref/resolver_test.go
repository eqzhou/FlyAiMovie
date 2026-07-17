package mediaref

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/storage"
)

func TestResolveLocalImageAsDataURI(t *testing.T) {
	store := storage.NewLocal(t.TempDir())
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	rel, _, err := store.Save("uploads", "pixel.png", &data)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := (&Resolver{Store: store}).ResolveImage(context.Background(), "minimax", "/static/"+rel)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resolved, "data:image/png;base64,") {
		t.Fatalf("unexpected reference %q", resolved)
	}
}

func TestResolveRejectsPrivateRemoteReference(t *testing.T) {
	_, err := (&Resolver{Store: storage.NewLocal(t.TempDir())}).ResolveImage(context.Background(), "minimax", "http://127.0.0.1/a.png")
	if err == nil || !strings.Contains(err.Error(), "publicly reachable") {
		t.Fatalf("error = %v", err)
	}
}

func TestMockReferenceIsNotRewritten(t *testing.T) {
	got, err := (&Resolver{}).ResolveImage(context.Background(), "mock", "/static/test.png")
	if err != nil || got != "/static/test.png" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}
