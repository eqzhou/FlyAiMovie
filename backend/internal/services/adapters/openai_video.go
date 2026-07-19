package adapters

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type OpenAIVideoAdapter struct{}

func (a *OpenAIVideoAdapter) Name() string { return "openai" }

func (a *OpenAIVideoAdapter) Generate(ctx context.Context, cfg AIConfig, in VideoGenInput) (*VideoGenResult, error) {
	if strings.TrimSpace(in.LastFrameURL) != "" {
		return nil, fmt.Errorf("openai video does not support a last frame reference")
	}
	if len(in.ReferenceImageURLs) > 1 {
		return nil, fmt.Errorf("openai video supports at most one input reference")
	}
	referenceURL := firstNonEmptyStr(in.FirstFrameURL, in.ImageURL)
	if referenceURL == "" && len(in.ReferenceImageURLs) == 1 {
		referenceURL = in.ReferenceImageURLs[0]
	}
	endpoint := joinV1(cfg.BaseURL, "/videos")
	model := defaultString(cfg.Model, "sora-2")
	size := openAIVideoSize(in.Size, in.AspectRatio)
	seconds := in.Duration
	if seconds <= 0 {
		seconds = 8
	}

	var data map[string]any
	var err error
	if strings.HasPrefix(referenceURL, "data:image/") {
		data, err = submitOpenAIMultipart(ctx, endpoint, cfg.APIKey, model, in.Prompt, size, seconds, referenceURL)
	} else {
		body := map[string]any{"model": model, "prompt": in.Prompt, "size": size, "seconds": strconv.Itoa(seconds)}
		if referenceURL != "" {
			body["input_reference"] = map[string]any{"image_url": referenceURL}
		}
		data, err = providerJSON(ctx, http.MethodPost, endpoint, cfg.APIKey, nil, body)
	}
	if err != nil {
		return nil, fmt.Errorf("openai video submit: %w", err)
	}
	taskID := firstString(data, "id")
	if taskID == "" {
		return nil, fmt.Errorf("openai video response missing id")
	}
	return &VideoGenResult{IsAsync: true, TaskID: taskID}, nil
}

func (a *OpenAIVideoAdapter) Poll(ctx context.Context, cfg AIConfig, taskID string) (*VideoPollResult, error) {
	endpoint := joinV1(cfg.BaseURL, "/videos/"+url.PathEscape(taskID))
	data, err := providerJSON(ctx, http.MethodGet, endpoint, cfg.APIKey, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("openai video poll: %w", err)
	}
	status := normalizeVideoStatus(firstString(data, "status"))
	if status == "failed" {
		return &VideoPollResult{Status: status, Error: "openai reported video generation failure"}, nil
	}
	result := &VideoPollResult{Status: status}
	if status == "completed" {
		result.VideoURL = endpoint + "/content"
		result.BearerToken = strings.TrimPrefix(cfg.APIKey, "Bearer ")
	}
	return result, nil
}

func submitOpenAIMultipart(ctx context.Context, endpoint, apiKey, model, prompt, size string, seconds int, dataURI string) (map[string]any, error) {
	mimeType, imageData, err := prepareOpenAIReference(dataURI, size)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{"model": model, "prompt": prompt, "size": size, "seconds": strconv.Itoa(seconds)} {
		if err := writer.WriteField(key, value); err != nil {
			return nil, err
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="input_reference"; filename="reference`+imageExtension(mimeType)+`"`)
	header.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(imageData); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(apiKey, "Bearer "))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := providerHTTPClient(120 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider request failed with HTTP %d", resp.StatusCode)
	}
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}
	return data, nil
}

func prepareOpenAIReference(value, size string) (string, []byte, error) {
	mimeType, data, err := decodeImageDataURI(value)
	if err != nil {
		return mimeType, data, err
	}
	if mimeType == "image/webp" {
		return "", nil, fmt.Errorf("OpenAI video input reference does not support local WebP conversion; use PNG or JPEG")
	}
	width, height, ok := parseVideoSize(size)
	if !ok {
		return "", nil, fmt.Errorf("invalid OpenAI video size %q", size)
	}
	imageValue, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", nil, fmt.Errorf("decode OpenAI input reference: %w", err)
	}
	if imageValue.Bounds().Dx() == width && imageValue.Bounds().Dy() == height {
		return mimeType, data, nil
	}
	resized := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := imageValue.Bounds()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sourceX := bounds.Min.X + x*bounds.Dx()/width
			sourceY := bounds.Min.Y + y*bounds.Dy()/height
			resized.Set(x, y, imageValue.At(sourceX, sourceY))
		}
	}
	var output bytes.Buffer
	if mimeType == "image/jpeg" {
		err = jpeg.Encode(&output, resized, &jpeg.Options{Quality: 92})
	} else {
		err = png.Encode(&output, resized)
	}
	if err != nil {
		return "", nil, err
	}
	return mimeType, output.Bytes(), nil
}

func parseVideoSize(value string) (int, int, bool) {
	widthText, heightText, ok := strings.Cut(value, "x")
	if !ok {
		return 0, 0, false
	}
	width, widthErr := strconv.Atoi(widthText)
	height, heightErr := strconv.Atoi(heightText)
	return width, height, widthErr == nil && heightErr == nil && width > 0 && height > 0
}

func decodeImageDataURI(value string) (string, []byte, error) {
	header, encoded, ok := strings.Cut(value, ",")
	if !ok || !strings.HasPrefix(header, "data:image/") || !strings.HasSuffix(header, ";base64") {
		return "", nil, fmt.Errorf("invalid input reference data URI")
	}
	mimeType := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	if mimeType != "image/png" && mimeType != "image/jpeg" && mimeType != "image/webp" {
		return "", nil, fmt.Errorf("unsupported OpenAI input reference type %q", mimeType)
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return "", nil, fmt.Errorf("invalid input reference base64")
	}
	return mimeType, data, nil
}

func imageExtension(mimeType string) string {
	if mimeType == "image/jpeg" {
		return ".jpg"
	}
	if mimeType == "image/webp" {
		return ".webp"
	}
	return ".png"
}

func openAIVideoSize(size, aspectRatio string) string {
	if strings.TrimSpace(size) != "" {
		return size
	}
	if strings.TrimSpace(aspectRatio) == "9:16" {
		return "720x1280"
	}
	return "1280x720"
}
