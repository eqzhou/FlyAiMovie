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
		return nil, fmt.Errorf("%s", humanizeProviderProbeError(err))
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, fmt.Errorf("读取厂商响应失败：%v", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("厂商拒绝了凭据（HTTP %d），请检查 API Key 与权限", resp.StatusCode)
		}
		if plan.verifyModel || resp.StatusCode >= 500 {
			return nil, fmt.Errorf("厂商探测返回 HTTP %d", resp.StatusCode)
		}
	}
	if cfg.Model != "" && plan.verifyModel {
		if found, known := probeResponseContainsModel(body, cfg.Model); known && !found {
			return nil, fmt.Errorf("厂商未返回配置的模型 %q，请确认模型名是否正确", cfg.Model)
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

func humanizeProviderProbeError(err error) string {
	if err == nil {
		return "厂商连接失败"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "disallowed addresses"):
		return "连接失败：厂商域名解析到了受保护的地址。若使用 Clash fake-ip，请确保后端已支持该路由；企业内网域名可配置 AI_PROVIDER_PRIVATE_HOSTS 与 AI_PROVIDER_CA_FILE"
	case strings.Contains(msg, "provider address is not allowed"):
		return "连接失败：厂商地址不被允许（私网/回环/元数据地址需显式放行）"
	case strings.Contains(msg, "x509"):
		return "连接失败：TLS 证书校验未通过。企业自签证书可设置 AI_PROVIDER_CA_FILE"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "Timeout") || strings.Contains(msg, "deadline"):
		return "连接失败：访问厂商超时，请检查网络、代理与 Base URL"
	case strings.Contains(msg, "no such host") || strings.Contains(msg, "resolve provider host"):
		return "连接失败：无法解析厂商域名，请检查 Base URL 与 DNS"
	case strings.Contains(msg, "connection refused"):
		return "连接失败：厂商拒绝连接，请检查 Base URL 与本地代理"
	default:
		return "连接失败：" + msg
	}
}
