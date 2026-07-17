package mediaref

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
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

func TestResolveImagesSkipsEmptyReferencesAndPreservesOrder(t *testing.T) {
	resolver := &Resolver{}
	got, err := resolver.ResolveImages(context.Background(), "mock", []string{" first.png ", "", "second.png"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "first.png" || got[1] != "second.png" {
		t.Fatalf("resolved=%v", got)
	}
}

func TestResolveImagesStopsAtInvalidReference(t *testing.T) {
	_, err := (&Resolver{}).ResolveImages(context.Background(), "minimax", []string{"data:image/png;base64,AA==", "file:///tmp/private.png"})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error=%v", err)
	}
}

func TestResolveImageRejectsUnsupportedAndMissingLocalReferences(t *testing.T) {
	resolver := &Resolver{Store: storage.NewLocal(t.TempDir())}
	for _, ref := range []string{"ftp://example.com/image.png", "//example.com/image.png"} {
		if _, err := resolver.ResolveImage(context.Background(), "minimax", ref); err == nil {
			t.Fatalf("expected %q to be rejected", ref)
		}
	}
	if _, err := resolver.ResolveImage(context.Background(), "minimax", "/static/uploads/missing.png"); err == nil {
		t.Fatalf("missing file error=%v", err)
	}
}

func TestEncodeDataURIRejectsNonImageAndMalformedImage(t *testing.T) {
	if _, err := encodeDataURI(strings.NewReader("plain text")); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("non-image error=%v", err)
	}
	malformedPNG := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	if _, err := encodeDataURI(bytes.NewReader(malformedPNG)); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("malformed image error=%v", err)
	}
}

func TestFitKeepsBothDimensionsPositive(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 3000, 1))
	source.Set(0, 0, color.White)
	resized := fit(source, 2048)
	if resized.Bounds().Dx() != 2048 || resized.Bounds().Dy() != 1 {
		t.Fatalf("bounds=%v", resized.Bounds())
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, resized, nil); err != nil {
		t.Fatalf("encode resized image: %v", err)
	}
}

func TestFitReturnsOriginalImageWhenAlreadyWithinLimit(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 20, 10))
	if got := fit(source, 2048); got != source {
		t.Fatal("fit copied an image that did not require resizing")
	}
}
