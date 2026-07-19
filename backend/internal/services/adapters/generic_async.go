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

// GenericVideoAdapter covers OpenAI-compatible async video APIs (volcengine/minimax-like).
type GenericVideoAdapter struct {
	ProviderName string
}

func (a *GenericVideoAdapter) Name() string { return a.ProviderName }

func (a *GenericVideoAdapter) Generate(ctx context.Context, cfg AIConfig, in VideoGenInput) (*VideoGenResult, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	url := base + "/api/v3/contents/generations/tasks"
	if a.ProviderName == "minimax" {
		url = joinV1(base, "/video_generation")
	}
	model := cfg.Model
	body := map[string]any{
		"model":  model,
		"prompt": in.Prompt,
	}
	if in.ImageURL != "" {
		body["image_url"] = in.ImageURL
		body["first_frame_image"] = in.ImageURL
	}
	if in.FirstFrameURL != "" {
		body["first_frame_image"] = in.FirstFrameURL
	}
	if in.LastFrameURL != "" {
		body["last_frame_image"] = in.LastFrameURL
	}
	if in.Duration > 0 {
		body["duration"] = in.Duration
	}
	if in.AspectRatio != "" {
		body["aspect_ratio"] = in.AspectRatio
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
		return nil, fmt.Errorf("%s video request failed (HTTP %d)", a.ProviderName, resp.StatusCode)
	}
	data, err := readProviderResponse(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)
	taskID := firstString(parsed, "id", "task_id", "taskId")
	if taskID == "" {
		if d, ok := parsed["data"].(map[string]any); ok {
			taskID = firstString(d, "id", "task_id", "taskId")
		}
	}
	videoURL := firstString(parsed, "video_url", "url")
	if taskID != "" {
		return &VideoGenResult{IsAsync: true, TaskID: taskID}, nil
	}
	if videoURL != "" {
		return &VideoGenResult{IsAsync: false, VideoURL: videoURL}, nil
	}
	return nil, fmt.Errorf("unable to parse video response")
}

func (a *GenericVideoAdapter) Poll(ctx context.Context, cfg AIConfig, taskID string) (*VideoPollResult, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	url := base + "/api/v3/contents/generations/tasks/" + taskID
	if a.ProviderName == "minimax" {
		url = joinV1(base, "/query/video_generation?task_id="+taskID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	client := providerHTTPClient(60 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &VideoPollResult{Status: "failed", Error: fmt.Sprintf("video poll request failed (HTTP %d)", resp.StatusCode)}, nil
	}
	data, err := readProviderResponse(resp.Body)
	if err != nil {
		return nil, err
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
	case "running", "processing", "in_progress":
		status = "processing"
	default:
		if status == "" {
			status = "processing"
		}
	}
	videoURL := firstString(parsed, "video_url", "url")
	if videoURL == "" {
		if d, ok := parsed["data"].(map[string]any); ok {
			videoURL = firstString(d, "video_url", "url", "file_id")
			if c, ok := d["content"].(map[string]any); ok {
				videoURL = firstString(c, "video_url", "url")
			}
		}
	}
	return &VideoPollResult{Status: status, VideoURL: videoURL}, nil
}

func joinV1(base, path string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + path
	}
	return base + "/v1" + path
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if t != "" {
					return t
				}
			case float64:
				return fmt.Sprintf("%.0f", t)
			}
		}
	}
	return ""
}
