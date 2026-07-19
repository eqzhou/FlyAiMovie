package adapters

// Official image adapters intentionally model each vendor's public contract
// instead of routing through an OpenAI-compatible shim. This keeps request
// semantics explicit and makes provider changes independently testable.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func officialHTTP(ctx context.Context, method, endpoint, key string, body any, headers map[string]string) ([]byte, int, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, r)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := providerHTTPClient(180 * time.Second).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return b, resp.StatusCode, err
}

func officialBase(base, suffix string) string { return strings.TrimRight(base, "/") + suffix }
func value(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if x, ok := m[k].(string); ok && x != "" {
			return x
		}
	}
	return ""
}
func nested(m map[string]any, key string) map[string]any {
	if x, ok := m[key].(map[string]any); ok {
		return x
	}
	return nil
}

// GeminiImageAdapter uses Gemini generateContent with image response modalities.
type GeminiImageAdapter struct{}

func (*GeminiImageAdapter) Name() string { return "gemini" }
func (a *GeminiImageAdapter) Generate(ctx context.Context, cfg AIConfig, in ImageGenInput) (*ImageGenResult, error) {
	model := cfg.Model
	if model == "" {
		model = "gemini-2.0-flash-preview-image-generation"
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if !strings.Contains(base, "/v1beta") {
		base += "/v1beta"
	}
	endpoint := officialBase(base, "/models/"+url.PathEscape(model)+":generateContent")
	body := map[string]any{"contents": []any{map[string]any{"parts": []any{map[string]any{"text": in.Prompt}}}}, "generationConfig": map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}}}
	data, code, err := officialHTTP(ctx, http.MethodPost, endpoint, "", body, map[string]string{"X-Goog-Api-Key": cfg.APIKey})
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return nil, fmt.Errorf("gemini image request failed with HTTP %d", code)
	}
	var p struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData struct {
						MIMEType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
					FileData struct {
						FileURI string `json:"fileUri"`
					} `json:"fileData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	for _, c := range p.Candidates {
		for _, part := range c.Content.Parts {
			if part.InlineData.Data != "" {
				return &ImageGenResult{Base64: part.InlineData.Data, MimeType: part.InlineData.MIMEType}, nil
			}
			if part.FileData.FileURI != "" {
				return &ImageGenResult{ImageURL: part.FileData.FileURI}, nil
			}
		}
	}
	return nil, fmt.Errorf("gemini response did not contain image data")
}
func (*GeminiImageAdapter) Poll(context.Context, AIConfig, string) (*ImagePollResult, error) {
	return &ImagePollResult{Status: "failed", Error: "gemini image generation is synchronous"}, nil
}

// MiniMaxImageAdapter implements /v1/image_generation.
type MiniMaxImageAdapter struct{}

func (*MiniMaxImageAdapter) Name() string { return "minimax" }
func (a *MiniMaxImageAdapter) Generate(ctx context.Context, cfg AIConfig, in ImageGenInput) (*ImageGenResult, error) {
	model := cfg.Model
	if model == "" {
		model = "image-01"
	}
	body := map[string]any{"model": model, "prompt": in.Prompt, "response_format": "url", "n": 1}
	if in.Size != "" {
		body["aspect_ratio"] = minimaxAspect(in.Size)
	}
	if len(in.ReferenceImages) > 0 {
		body["subject_reference"] = []map[string]any{{"type": "character", "image_file": in.ReferenceImages[0]}}
	}
	data, code, err := officialHTTP(ctx, http.MethodPost, joinV1(cfg.BaseURL, "/image_generation"), cfg.APIKey, body, nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return nil, fmt.Errorf("minimax image request failed with HTTP %d", code)
	}
	var p map[string]any
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if err := providerBaseError("minimax", p); err != nil {
		return nil, err
	}
	d := nested(p, "data")
	if id := value(p, "task_id", "id"); id != "" {
		return &ImageGenResult{IsAsync: true, TaskID: id}, nil
	}
	if d != nil {
		if arr, ok := d["image_urls"].([]any); ok && len(arr) > 0 {
			if s, ok := arr[0].(string); ok {
				return &ImageGenResult{ImageURL: s}, nil
			}
		}
		if u := value(d, "image_url", "url"); u != "" {
			return &ImageGenResult{ImageURL: u}, nil
		}
	}
	return nil, fmt.Errorf("minimax response did not contain image URL")
}
func (*MiniMaxImageAdapter) Poll(context.Context, AIConfig, string) (*ImagePollResult, error) {
	return &ImagePollResult{Status: "failed", Error: "minimax image generation is synchronous"}, nil
}
func minimaxAspect(size string) string {
	if strings.Contains(size, "x") {
		p := strings.SplitN(size, "x", 2)
		if len(p) == 2 {
			return p[0] + ":" + p[1]
		}
	}
	return size
}

// VolcengineImageAdapter implements Ark Seedream image generation.
type VolcengineImageAdapter struct{}

func (*VolcengineImageAdapter) Name() string { return "volcengine" }
func (a *VolcengineImageAdapter) Generate(ctx context.Context, cfg AIConfig, in ImageGenInput) (*ImageGenResult, error) {
	model := cfg.Model
	if model == "" {
		model = "doubao-seedream-4-0-250828"
	}
	body := map[string]any{"model": model, "prompt": in.Prompt, "response_format": "url", "watermark": false}
	if in.Size != "" {
		body["size"] = in.Size
	}
	if len(in.ReferenceImages) > 0 {
		body["image"] = in.ReferenceImages[0]
	}
	data, code, err := officialHTTP(ctx, http.MethodPost, joinPath(cfg.BaseURL, "/api/v3/images/generations"), cfg.APIKey, body, nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return nil, fmt.Errorf("volcengine image request failed with HTTP %d", code)
	}
	var p map[string]any
	if err = json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if id := value(p, "task_id", "id"); id != "" {
		return &ImageGenResult{IsAsync: true, TaskID: id}, nil
	}
	arr, _ := p["data"].([]any)
	if len(arr) > 0 {
		if m, ok := arr[0].(map[string]any); ok {
			if u := value(m, "url", "image_url"); u != "" {
				return &ImageGenResult{ImageURL: u}, nil
			}
		}
	}
	return nil, fmt.Errorf("volcengine response did not contain image URL")
}
func (a *VolcengineImageAdapter) Poll(ctx context.Context, cfg AIConfig, taskID string) (*ImagePollResult, error) {
	data, code, err := officialHTTP(ctx, http.MethodGet, joinPath(cfg.BaseURL, "/api/v3/images/generations/"+url.PathEscape(taskID)), cfg.APIKey, nil, nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return nil, fmt.Errorf("volcengine image poll failed with HTTP %d", code)
	}
	var p map[string]any
	_ = json.Unmarshal(data, &p)
	status := strings.ToLower(value(p, "status", "task_status"))
	if status == "" {
		status = "processing"
	}
	if status == "success" || status == "succeeded" || status == "completed" {
		status = "completed"
	}
	if status == "failed" || status == "error" {
		status = "failed"
	}
	var imageURL string
	if arr, _ := p["data"].([]any); len(arr) > 0 {
		if m, ok := arr[0].(map[string]any); ok {
			imageURL = value(m, "url", "image_url")
		}
	}
	result := &ImagePollResult{Status: status, ImageURL: imageURL}
	if status == "failed" {
		result.Error = "volcengine reported image generation failure"
	}
	return result, nil
}

// DashScopeImageAdapter implements Aliyun's async text-to-image contract.
type DashScopeImageAdapter struct{}

func (*DashScopeImageAdapter) Name() string { return "ali" }
func (a *DashScopeImageAdapter) Generate(ctx context.Context, cfg AIConfig, in ImageGenInput) (*ImageGenResult, error) {
	model := cfg.Model
	if model == "" {
		model = "wanx-v1"
	}
	body := map[string]any{"model": model, "input": map[string]any{"prompt": in.Prompt}, "parameters": map[string]any{"n": 1}}
	if in.Size != "" {
		body["parameters"].(map[string]any)["size"] = in.Size
	}
	data, code, err := officialHTTP(ctx, http.MethodPost, joinPath(cfg.BaseURL, "/api/v1/services/aigc/text2image/image-synthesis"), cfg.APIKey, body, map[string]string{"X-DashScope-Async": "enable"})
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return nil, fmt.Errorf("dashscope image request failed with HTTP %d", code)
	}
	var p map[string]any
	_ = json.Unmarshal(data, &p)
	if out := nested(p, "output"); out != nil {
		if id := value(out, "task_id", "id"); id != "" {
			return &ImageGenResult{IsAsync: true, TaskID: id}, nil
		}
	}
	if id := value(p, "task_id"); id != "" {
		return &ImageGenResult{IsAsync: true, TaskID: id}, nil
	}
	return dashscopeResult(p)
}
func dashscopeResult(p map[string]any) (*ImageGenResult, error) {
	out := nested(p, "output")
	if out == nil {
		out = p
	}
	if arr, ok := out["results"].([]any); ok && len(arr) > 0 {
		if m, ok := arr[0].(map[string]any); ok {
			if u := value(m, "url", "image_url"); u != "" {
				return &ImageGenResult{ImageURL: u}, nil
			}
		}
	}
	if u := value(out, "url", "image_url"); u != "" {
		return &ImageGenResult{ImageURL: u}, nil
	}
	if b := value(out, "b64_json", "base64"); b != "" {
		if _, err := base64.StdEncoding.DecodeString(b); err == nil {
			return &ImageGenResult{Base64: b, MimeType: "image/png"}, nil
		}
	}
	return nil, fmt.Errorf("dashscope response did not contain image result")
}
func (a *DashScopeImageAdapter) Poll(ctx context.Context, cfg AIConfig, taskID string) (*ImagePollResult, error) {
	data, code, err := officialHTTP(ctx, http.MethodGet, joinPath(cfg.BaseURL, "/api/v1/tasks/"+url.PathEscape(taskID)), cfg.APIKey, nil, nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return nil, fmt.Errorf("dashscope image poll failed with HTTP %d", code)
	}
	var p map[string]any
	if err = json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	out := nested(p, "output")
	if out == nil {
		out = p
	}
	st := strings.ToLower(value(out, "task_status", "status"))
	switch st {
	case "succeeded", "success", "completed", "done":
		st = "completed"
	case "failed", "canceled", "error":
		st = "failed"
	default:
		if st == "" {
			st = "processing"
		}
	}
	r := &ImagePollResult{Status: st}
	if st == "failed" {
		r.Error = "dashscope reported image generation failure"
	}
	if x, e := dashscopeResult(p); e == nil {
		r.ImageURL = x.ImageURL
	}
	return r, nil
}
