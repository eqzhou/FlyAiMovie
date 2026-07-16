package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompatImageAdapter supports OpenAI images + common async gateways (task_id responses).
type OpenAICompatImageAdapter struct {
	ProviderName string
}

func (a *OpenAICompatImageAdapter) Name() string {
	if a.ProviderName != "" {
		return a.ProviderName
	}
	return "openai"
}

func (a *OpenAICompatImageAdapter) Generate(ctx context.Context, cfg AIConfig, in ImageGenInput) (*ImageGenResult, error) {
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
	if len(in.ReferenceImages) > 0 {
		body["reference_images"] = in.ReferenceImages
		body["image"] = in.ReferenceImages[0]
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := providerHTTPClient(180 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s image error %d: %s", a.Name(), resp.StatusCode, string(data))
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	// async task style
	taskID := firstString(parsed, "id", "task_id", "taskId")
	if taskID == "" {
		if d, ok := parsed["data"].(map[string]any); ok {
			taskID = firstString(d, "id", "task_id", "taskId")
		}
	}
	// OpenAI style data array
	if arr, ok := parsed["data"].([]any); ok && len(arr) > 0 {
		if m, ok := arr[0].(map[string]any); ok {
			if u := firstString(m, "url"); u != "" {
				return &ImageGenResult{IsAsync: false, ImageURL: u}, nil
			}
			if b64 := firstString(m, "b64_json"); b64 != "" {
				return &ImageGenResult{IsAsync: false, Base64: b64, MimeType: "image/png"}, nil
			}
		}
	}
	if u := firstString(parsed, "image_url", "url"); u != "" {
		return &ImageGenResult{IsAsync: false, ImageURL: u}, nil
	}
	if taskID != "" {
		return &ImageGenResult{IsAsync: true, TaskID: taskID}, nil
	}
	return nil, fmt.Errorf("unable to parse image response: %s", string(data))
}

func (a *OpenAICompatImageAdapter) Poll(ctx context.Context, cfg AIConfig, taskID string) (*ImagePollResult, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	url := base + "/images/tasks/" + taskID
	// try common query paths
	paths := []string{
		url,
		base + "/tasks/" + taskID,
		strings.TrimRight(cfg.BaseURL, "/") + "/api/v3/images/generations/" + taskID,
	}
	client := providerHTTPClient(60 * time.Second)
	var lastErr error
	for _, u := range paths {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("%d %s", resp.StatusCode, string(data))
			continue
		}
		var parsed map[string]any
		_ = json.Unmarshal(data, &parsed)
		status := strings.ToLower(firstString(parsed, "status", "task_status"))
		if status == "" {
			if d, ok := parsed["data"].(map[string]any); ok {
				status = strings.ToLower(firstString(d, "status", "task_status"))
			}
		}
		switch status {
		case "succeeded", "success", "completed", "done":
			status = "completed"
		case "failed", "error", "canceled":
			status = "failed"
		case "queued", "pending", "submitted":
			status = "pending"
		default:
			if status == "" {
				status = "processing"
			} else {
				status = "processing"
			}
		}
		imageURL := firstString(parsed, "image_url", "url")
		if imageURL == "" {
			if d, ok := parsed["data"].(map[string]any); ok {
				imageURL = firstString(d, "image_url", "url")
				if arr, ok := d["images"].([]any); ok && len(arr) > 0 {
					if m, ok := arr[0].(map[string]any); ok {
						imageURL = firstString(m, "url", "image_url")
					} else if s, ok := arr[0].(string); ok {
						imageURL = s
					}
				}
			}
			if arr, ok := parsed["data"].([]any); ok && len(arr) > 0 {
				if m, ok := arr[0].(map[string]any); ok {
					imageURL = firstString(m, "url", "image_url")
				}
			}
		}
		return &ImagePollResult{Status: status, ImageURL: imageURL}, nil
	}
	if lastErr != nil {
		return &ImagePollResult{Status: "processing", Error: lastErr.Error()}, nil
	}
	return &ImagePollResult{Status: "processing"}, nil
}
