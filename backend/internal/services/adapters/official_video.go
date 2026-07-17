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

type MiniMaxVideoAdapter struct{}
type VolcengineVideoAdapter struct{}
type AliyunVideoAdapter struct{}

func (a *MiniMaxVideoAdapter) Name() string    { return "minimax" }
func (a *VolcengineVideoAdapter) Name() string { return "volcengine" }
func (a *AliyunVideoAdapter) Name() string     { return "ali" }

func (a *MiniMaxVideoAdapter) Generate(ctx context.Context, cfg AIConfig, in VideoGenInput) (*VideoGenResult, error) {
	body := map[string]any{"model": defaultString(cfg.Model, "video-01"), "prompt": in.Prompt}
	if first := firstNonEmptyStr(in.FirstFrameURL, in.ImageURL); first != "" {
		body["first_frame_image"] = first
	}
	if in.LastFrameURL != "" {
		body["last_frame_image"] = in.LastFrameURL
	}
	data, err := providerJSON(ctx, http.MethodPost, joinV1(cfg.BaseURL, "/video_generation"), cfg.APIKey, nil, body)
	if err != nil {
		return nil, fmt.Errorf("minimax video submit: %w", err)
	}
	if err := providerBaseError("minimax", data); err != nil {
		return nil, err
	}
	taskID := firstString(data, "task_id", "id")
	if taskID == "" {
		return nil, fmt.Errorf("minimax video response missing task_id")
	}
	return &VideoGenResult{IsAsync: true, TaskID: taskID}, nil
}

func (a *MiniMaxVideoAdapter) Poll(ctx context.Context, cfg AIConfig, taskID string) (*VideoPollResult, error) {
	queryURL := joinV1(cfg.BaseURL, "/query/video_generation") + "?task_id=" + url.QueryEscape(taskID)
	data, err := providerJSON(ctx, http.MethodGet, queryURL, cfg.APIKey, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("minimax video poll: %w", err)
	}
	if err := providerBaseError("minimax", data); err != nil {
		return &VideoPollResult{Status: "failed", Error: err.Error()}, nil
	}
	status := normalizeVideoStatus(firstString(data, "status", "task_status"))
	if status != "completed" {
		return &VideoPollResult{Status: status, Error: firstString(data, "error", "message")}, nil
	}
	if direct := firstString(data, "video_url", "download_url"); direct != "" {
		return &VideoPollResult{Status: "completed", VideoURL: direct}, nil
	}
	fileID := firstString(data, "file_id")
	if fileID == "" {
		return &VideoPollResult{Status: "failed", Error: "minimax completed task missing file_id"}, nil
	}
	retrieveURL := joinV1(cfg.BaseURL, "/files/retrieve") + "?file_id=" + url.QueryEscape(fileID)
	fileData, err := providerJSON(ctx, http.MethodGet, retrieveURL, cfg.APIKey, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("minimax video retrieve: %w", err)
	}
	if err := providerBaseError("minimax", fileData); err != nil {
		return &VideoPollResult{Status: "failed", Error: err.Error()}, nil
	}
	file, _ := fileData["file"].(map[string]any)
	downloadURL := firstString(file, "download_url", "url")
	if downloadURL == "" {
		return &VideoPollResult{Status: "failed", Error: "minimax file response missing download_url"}, nil
	}
	return &VideoPollResult{Status: "completed", VideoURL: downloadURL}, nil
}

func (a *VolcengineVideoAdapter) Generate(ctx context.Context, cfg AIConfig, in VideoGenInput) (*VideoGenResult, error) {
	content := []map[string]any{{"type": "text", "text": in.Prompt}}
	if first := firstNonEmptyStr(in.FirstFrameURL, in.ImageURL); first != "" {
		content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": first}, "role": "first_frame"})
	}
	if in.LastFrameURL != "" {
		content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": in.LastFrameURL}, "role": "last_frame"})
	}
	body := map[string]any{"model": cfg.Model, "content": content}
	if in.Duration > 0 {
		body["duration"] = in.Duration
	}
	if in.AspectRatio != "" {
		body["ratio"] = in.AspectRatio
	}
	endpoint := joinPath(cfg.BaseURL, "/api/v3/contents/generations/tasks")
	data, err := providerJSON(ctx, http.MethodPost, endpoint, cfg.APIKey, nil, body)
	if err != nil {
		return nil, fmt.Errorf("volcengine video submit: %w", err)
	}
	taskID := firstString(data, "id", "task_id")
	if taskID == "" {
		return nil, fmt.Errorf("volcengine video response missing id")
	}
	return &VideoGenResult{IsAsync: true, TaskID: taskID}, nil
}

func (a *VolcengineVideoAdapter) Poll(ctx context.Context, cfg AIConfig, taskID string) (*VideoPollResult, error) {
	endpoint := joinPath(cfg.BaseURL, "/api/v3/contents/generations/tasks/"+url.PathEscape(taskID))
	data, err := providerJSON(ctx, http.MethodGet, endpoint, cfg.APIKey, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("volcengine video poll: %w", err)
	}
	status := normalizeVideoStatus(firstString(data, "status"))
	content, _ := data["content"].(map[string]any)
	videoURL := firstString(content, "video_url", "url")
	if videoURL == "" {
		videoURL = firstString(data, "video_url", "url")
	}
	return &VideoPollResult{Status: status, VideoURL: videoURL, Error: firstString(data, "error", "message")}, nil
}

func (a *AliyunVideoAdapter) Generate(ctx context.Context, cfg AIConfig, in VideoGenInput) (*VideoGenResult, error) {
	input := map[string]any{"prompt": in.Prompt}
	if first := firstNonEmptyStr(in.FirstFrameURL, in.ImageURL); first != "" {
		input["img_url"] = first
		input["first_frame_url"] = first
	}
	if in.LastFrameURL != "" {
		input["last_frame_url"] = in.LastFrameURL
	}
	parameters := map[string]any{}
	if in.Duration > 0 {
		parameters["duration"] = in.Duration
	}
	if in.AspectRatio != "" {
		parameters["ratio"] = in.AspectRatio
	}
	body := map[string]any{"model": cfg.Model, "input": input, "parameters": parameters}
	headers := map[string]string{"X-DashScope-Async": "enable"}
	endpoint := joinPath(cfg.BaseURL, "/api/v1/services/aigc/video-generation/video-synthesis")
	data, err := providerJSON(ctx, http.MethodPost, endpoint, cfg.APIKey, headers, body)
	if err != nil {
		return nil, fmt.Errorf("aliyun video submit: %w", err)
	}
	output, _ := data["output"].(map[string]any)
	taskID := firstString(output, "task_id")
	if taskID == "" {
		return nil, fmt.Errorf("aliyun video response missing output.task_id")
	}
	return &VideoGenResult{IsAsync: true, TaskID: taskID}, nil
}

func (a *AliyunVideoAdapter) Poll(ctx context.Context, cfg AIConfig, taskID string) (*VideoPollResult, error) {
	endpoint := joinPath(cfg.BaseURL, "/api/v1/tasks/"+url.PathEscape(taskID))
	data, err := providerJSON(ctx, http.MethodGet, endpoint, cfg.APIKey, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("aliyun video poll: %w", err)
	}
	output, _ := data["output"].(map[string]any)
	status := normalizeVideoStatus(firstString(output, "task_status", "status"))
	videoURL := firstString(output, "video_url", "url")
	if videoURL == "" {
		if results, ok := output["results"].([]any); ok && len(results) > 0 {
			result, _ := results[0].(map[string]any)
			videoURL = firstString(result, "video_url", "url")
		}
	}
	errorMessage := firstString(output, "message", "code")
	if errorMessage == "" {
		errorMessage = firstString(data, "message", "code")
	}
	return &VideoPollResult{Status: status, VideoURL: videoURL, Error: errorMessage}, nil
}

func providerJSON(ctx context.Context, method, endpoint, apiKey string, headers map[string]string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(apiKey, "Bearer "))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := providerHTTPClient(120 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}
	return data, nil
}

func providerBaseError(provider string, data map[string]any) error {
	base, _ := data["base_resp"].(map[string]any)
	code := firstString(base, "status_code")
	if code == "" || code == "0" {
		return nil
	}
	return fmt.Errorf("%s API error %s: %s", provider, code, firstString(base, "status_msg"))
}

func normalizeVideoStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "succeeded", "completed", "done":
		return "completed"
	case "fail", "failed", "error", "canceled", "cancelled", "expired", "unknown":
		return "failed"
	case "queueing", "queued", "pending", "submitted", "created":
		return "pending"
	default:
		return "processing"
	}
}

func joinPath(base, path string) string {
	base = strings.TrimRight(base, "/")
	for _, suffix := range []string{"/api/v3", "/api/v1"} {
		if strings.HasSuffix(base, suffix) && strings.HasPrefix(path, suffix+"/") {
			return base + strings.TrimPrefix(path, suffix)
		}
	}
	return base + path
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
