package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ConnectionProbeResult struct {
	Status    string `json:"status"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	LatencyMS int64  `json:"latency_ms"`
	Detail    string `json:"detail"`
}

func ProbeConnection(ctx context.Context, cfg AIConfig) (*ConnectionProbeResult, error) {
	started := time.Now()
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "mock" {
		return probeResult(cfg, started, "Mock 服务可用"), nil
	}
	plan, err := connectionProbeRequest(cfg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, plan.method, plan.endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build provider probe: %w", err)
	}
	for key, value := range plan.headers {
		req.Header.Set(key, value)
	}
	client := providerHTTPClient(20 * time.Second)
	if provider == "openai_local" {
		client = &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 20 * time.Second}
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider connection failed: %w", err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, fmt.Errorf("read provider response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("provider rejected the credential (HTTP %d)", resp.StatusCode)
		}
		if plan.verifyModel || resp.StatusCode >= 500 {
			return nil, fmt.Errorf("provider probe returned HTTP %d", resp.StatusCode)
		}
	}
	if cfg.Model != "" && plan.verifyModel {
		if found, known := probeResponseContainsModel(body, cfg.Model); known && !found {
			return nil, fmt.Errorf("configured model %q was not returned by the provider", cfg.Model)
		}
	}
	detail := "连接可达；厂商未提供无费用模型枚举，未触发生成任务"
	if plan.verifyModel {
		detail = "连接成功，凭据与模型端点可用"
	}
	return probeResult(cfg, started, detail), nil
}

type connectionProbePlan struct {
	method      string
	endpoint    string
	headers     map[string]string
	verifyModel bool
}

func connectionProbeRequest(cfg AIConfig) (*connectionProbePlan, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("provider base URL is invalid")
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	headers := map[string]string{}
	if cfg.APIKey != "" {
		headers["Authorization"] = "Bearer " + strings.TrimPrefix(cfg.APIKey, "Bearer ")
	}
	plan := &connectionProbePlan{method: http.MethodHead, endpoint: base, headers: headers}
	switch provider {
	case "openai", "openai_local", "chatfire":
		plan.method = http.MethodGet
		plan.endpoint = joinV1(base, "/models")
		plan.verifyModel = true
	case "gemini":
		plan.method = http.MethodGet
		plan.endpoint = joinPath(base, "/v1beta/models")
		plan.verifyModel = true
		delete(headers, "Authorization")
		headers["X-Goog-Api-Key"] = cfg.APIKey
	case "minimax", "volcengine", "ali", "vidu":
		// These providers do not expose one stable, free model-list contract
		// across all image/video/audio products. Probe transport and auth status
		// without creating a billable generation task.
	default:
		return nil, fmt.Errorf("provider %q does not support connection testing", provider)
	}
	return plan, nil
}

func probeResponseContainsModel(body []byte, model string) (bool, bool) {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return false, false
	}
	items, ok := payload["data"].([]any)
	if !ok {
		items, ok = payload["models"].([]any)
	}
	if !ok {
		return false, false
	}
	wanted := strings.TrimPrefix(strings.TrimSpace(model), "models/")
	for _, item := range items {
		entry, _ := item.(map[string]any)
		id, _ := entry["id"].(string)
		if id == "" {
			id, _ = entry["name"].(string)
		}
		if strings.TrimPrefix(id, "models/") == wanted {
			return true, true
		}
	}
	return false, true
}

func probeResult(cfg AIConfig, started time.Time, detail string) *ConnectionProbeResult {
	return &ConnectionProbeResult{Status: "ok", Provider: cfg.Provider, Model: cfg.Model, LatencyMS: time.Since(started).Milliseconds(), Detail: detail}
}
