package httpapi

import (
	"net/http"
	"testing"
)

func TestValidateAIConfigRejectsNonPublicBaseURLs(t *testing.T) {
	tests := []struct {
		name string
		base string
	}{
		{name: "loopback", base: "http://127.0.0.1:8080"},
		{name: "ipv6 loopback", base: "http://[::1]:8080"},
		{name: "private", base: "http://10.0.0.1"},
		{name: "link local", base: "http://169.254.169.254"},
		{name: "localhost", base: "http://localhost:8080"},
		{name: "userinfo", base: "https://user:pass@example.com"},
		{name: "query", base: "https://api.example.com/?token=secret"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateAIConfigInput("image", "openai", "test", tc.base, "key"); err == nil {
				t.Fatalf("expected base URL %q to be rejected", tc.base)
			}
		})
	}
}

func TestCreateLocalTextConfigUsesServerAllowlist(t *testing.T) {
	server, router := testServerRouter(t)
	body := `{"service_type":"text","provider":"openai_local","name":"Ollama","base_url":"http://host.docker.internal:11434","model":"qwen2.5:latest"}`
	denied := performRequest(router, http.MethodPost, "/api/v1/ai-configs", body, nil)
	if denied.Code != http.StatusBadRequest {
		t.Fatalf("without allowlist status=%d body=%s", denied.Code, denied.Body.String())
	}
	server.Cfg.AI.AllowedPrivateBaseURLHosts = []string{"host.docker.internal"}
	created := performRequest(router, http.MethodPost, "/api/v1/ai-configs", body, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("with allowlist status=%d body=%s", created.Code, created.Body.String())
	}
}

func TestValidateAIConfigAllowsPublicBaseURL(t *testing.T) {
	if err := validateAIConfigInput("image", "openai", "test", "https://api.example.com/v1", "key"); err != nil {
		t.Fatalf("public base URL rejected: %v", err)
	}
}

func TestValidateAIConfigAllowsOpenAIVideo(t *testing.T) {
	if err := validateAIConfigInput("video", "openai", "Sora", "https://api.openai.com", "key"); err != nil {
		t.Fatalf("OpenAI video config rejected: %v", err)
	}
}

func TestValidateAIConfigPrivateCompatibleHostsRequireExplicitAllowlist(t *testing.T) {
	allowed := []string{"host.docker.internal", "127.0.0.1"}
	for _, baseURL := range []string{"http://host.docker.internal:11434", "http://127.0.0.1:11434/v1"} {
		if err := validateAIConfigInputWithPrivateHosts("text", "openai_local", "Ollama", baseURL, "", allowed); err != nil {
			t.Fatalf("allowed local endpoint %q rejected: %v", baseURL, err)
		}
	}
	for _, tc := range []struct {
		serviceType string
		provider    string
		baseURL     string
		allowed     []string
	}{
		{"text", "openai_local", "http://127.0.0.1:11434", nil},
		{"text", "openai_local", "http://10.0.0.8:11434", allowed},
		{"image", "openai_local", "http://127.0.0.1:11434", allowed},
		{"text", "openai_local", "http://169.254.169.254/latest", []string{"169.254.169.254"}},
	} {
		if err := validateAIConfigInputWithPrivateHosts(tc.serviceType, tc.provider, "local", tc.baseURL, "", tc.allowed); err == nil {
			t.Fatalf("unsafe local config accepted: %+v", tc)
		}
	}
}
