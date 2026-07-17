package mediaref

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/services/mediafetch"
	"github.com/eqzhou/flyaimovie/internal/storage"
)

const (
	maxSourceBytes int64 = 30 << 20
	maxDataBytes         = 10 << 20
	maxDimension         = 2048
)

type Resolver struct {
	Store *storage.LocalStorage
}

func (r *Resolver) ResolveImages(ctx context.Context, provider string, refs []string) ([]string, error) {
	resolved := make([]string, 0, len(refs))
	for _, ref := range refs {
		value, err := r.ResolveImage(ctx, provider, ref)
		if err != nil {
			return nil, err
		}
		if value != "" {
			resolved = append(resolved, value)
		}
	}
	return resolved, nil
}

func (r *Resolver) ResolveImage(ctx context.Context, provider, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	if provider == "mock" || strings.HasPrefix(ref, "data:image/") {
		return ref, nil
	}
	parsed, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("invalid reference image")
	}
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		if err := mediafetch.ValidateRemoteURL(ctx, ref); err != nil {
			return "", fmt.Errorf("reference image is not publicly reachable: %w", err)
		}
		return ref, nil
	}
	if parsed.Scheme != "" || parsed.Host != "" || r == nil || r.Store == nil {
		return "", fmt.Errorf("unsupported reference image")
	}
	path, err := r.Store.Resolve(ref)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return encodeDataURI(file)
}

func encodeDataURI(reader io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxSourceBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxSourceBytes {
		return "", fmt.Errorf("reference image exceeds %d bytes", maxSourceBytes)
	}
	mime := http.DetectContentType(data)
	if mime != "image/png" && mime != "image/jpeg" {
		return "", fmt.Errorf("unsupported reference image type %q", mime)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode reference image: %w", err)
	}
	if img.Bounds().Dx() > maxDimension || img.Bounds().Dy() > maxDimension || len(data) > maxDataBytes {
		img = fit(img, maxDimension)
		var output bytes.Buffer
		if err := jpeg.Encode(&output, img, &jpeg.Options{Quality: 88}); err != nil {
			return "", err
		}
		data, mime = output.Bytes(), "image/jpeg"
	}
	if len(data) > maxDataBytes {
		return "", fmt.Errorf("encoded reference image exceeds %d bytes", maxDataBytes)
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func fit(source image.Image, max int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= max && height <= max {
		return source
	}
	if width >= height {
		height = height * max / width
		width = max
	} else {
		width = width * max / height
		height = max
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sx := bounds.Min.X + x*bounds.Dx()/width
			sy := bounds.Min.Y + y*bounds.Dy()/height
			target.Set(x, y, source.At(sx, sy))
		}
	}
	return target
}
