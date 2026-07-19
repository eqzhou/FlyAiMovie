package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ViduVideoAdapter implements Vidu-style image-to-video APIs.
type ViduVideoAdapter struct{}

func (a *ViduVideoAdapter) Name() string { return "vidu" }

func (a *ViduVideoAdapter) Generate(ctx context.Context, cfg AIConfig, in VideoGenInput) (*VideoGenResult, error) {
	if err := validateViduConfig(cfg); err != nil {
		return nil, err
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	url := base + "/ent/v2/img2video"
	if strings.Contains(base, "/ent/") {
		url = base + "/img2video"
	}
	model := cfg.Model
	if model == "" {
		model = "vidu2.0"
	}
	body := map[string]any{
		"model":  model,
		"prompt": in.Prompt,
	}
	if in.Duration > 0 {
		body["duration"] = in.Duration
	}
	if in.AspectRatio != "" {
		body["aspect_ratio"] = in.AspectRatio
	}
	img := firstNonEmptyStr(in.FirstFrameURL, in.ImageURL)
	if img == "" && len(in.ReferenceImageURLs) == 0 {
		return nil, fmt.Errorf("vidu image-to-video requires at least one image")
	}
	if img != "" {
		body["images"] = []string{img}
		body["image"] = img
	}
	if in.LastFrameURL != "" {
		body["last_image"] = in.LastFrameURL
	}
	if len(in.ReferenceImageURLs) > 0 {
		body["images"] = in.ReferenceImageURLs
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cfg.APIKey), "Token "))
	req.Header.Set("Authorization", "Token "+token)
	req.Header.Set("Content-Type", "application/json")
	client := providerHTTPClient(120 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("vidu video request failed with HTTP %d", resp.StatusCode)
	}
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)
	taskID := firstString(parsed, "task_id", "id", "taskId")
	if taskID == "" {
		if d, ok := parsed["data"].(map[string]any); ok {
			taskID = firstString(d, "task_id", "id")
		}
	}
	if taskID == "" {
		return nil, fmt.Errorf("vidu response missing task id")
	}
	return &VideoGenResult{IsAsync: true, TaskID: taskID}, nil
}

func (a *ViduVideoAdapter) Poll(ctx context.Context, cfg AIConfig, taskID string) (*VideoPollResult, error) {
	if err := validateViduConfig(cfg); err != nil {
		return nil, err
	}
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("vidu task id is required")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	endpoint := base + "/ent/v2/tasks/" + url.PathEscape(taskID) + "/creations"
	if strings.Contains(base, "/ent/v2") {
		endpoint = base + "/tasks/" + url.PathEscape(taskID) + "/creations"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cfg.APIKey), "Token "))
	req.Header.Set("Authorization", "Token "+token)
	client := providerHTTPClient(60 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("vidu poll failed with HTTP %d", resp.StatusCode)
	}
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)
	status := strings.ToLower(firstString(parsed, "state", "status"))
	switch status {
	case "success", "succeeded", "completed", "done":
		status = "completed"
	case "failed", "error":
		status = "failed"
	case "created", "queueing", "pending":
		status = "pending"
	case "processing", "running":
		status = "processing"
	default:
		if status == "" {
			status = "processing"
		}
	}
	videoURL := firstString(parsed, "video_url", "url")
	if videoURL == "" {
		if creations, ok := parsed["creations"].([]any); ok && len(creations) > 0 {
			if m, ok := creations[0].(map[string]any); ok {
				videoURL = firstString(m, "url", "video_url")
			}
		}
	}
	return &VideoPollResult{Status: status, VideoURL: videoURL}, nil
}

func validateViduConfig(cfg AIConfig) error {
	parsed, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("vidu base URL must be http or https")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return fmt.Errorf("vidu API key is required")
	}
	return nil
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
