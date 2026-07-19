package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type OpenAIImageAdapter struct{}

func (a *OpenAIImageAdapter) Name() string { return "openai" }

func (a *OpenAIImageAdapter) Generate(ctx context.Context, cfg AIConfig, in ImageGenInput) (*ImageGenResult, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	url := base + "/images/generations"
	model := cfg.Model
	if model == "" {
		model = "dall-e-3"
	}
	size := in.Size
	if size == "" {
		size = "1024x1024"
	}
	body := map[string]any{
		"model":  model,
		"prompt": in.Prompt,
		"n":      1,
		"size":   size,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := providerHTTPClient(120 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai image request failed (HTTP %d)", resp.StatusCode)
	}
	data, err := readProviderResponse(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("empty image response")
	}
	if parsed.Data[0].URL != "" {
		return &ImageGenResult{IsAsync: false, ImageURL: parsed.Data[0].URL}, nil
	}
	return &ImageGenResult{IsAsync: false, Base64: parsed.Data[0].B64JSON, MimeType: "image/png"}, nil
}

func (a *OpenAIImageAdapter) Poll(ctx context.Context, cfg AIConfig, taskID string) (*ImagePollResult, error) {
	return &ImagePollResult{Status: "failed", Error: "openai image is sync only"}, nil
}
