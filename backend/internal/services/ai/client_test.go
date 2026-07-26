package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/security"
	"github.com/eqzhou/flyaimovie/internal/testsupport"
)

func TestChatWithMaxTokensForwardsLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["max_tokens"] != float64(321) {
			t.Fatalf("max_tokens=%v, want 321", body["max_tokens"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	result, err := ChatWithMaxTokens(context.Background(), &ServiceConfig{BaseURL: server.URL, APIKey: "test", Model: "test-model"}, "system", "user", 0.2, 321)
	if err != nil || result != "ok" {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

func TestChatProviderErrorDoesNotExposeResponseBodyOrAPIKey(t *testing.T) {
	secret := "sk-provider-secret"
	echoedPrompt := "private screenplay content"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"echo ` + secret + ` and ` + echoedPrompt + `","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	_, err := ChatWithMaxTokens(context.Background(), &ServiceConfig{Provider: "openai_local", BaseURL: server.URL, APIKey: secret, Model: "test"}, "system", echoedPrompt, 0.2, 100)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("unexpected provider error: %v", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), echoedPrompt) || strings.Contains(err.Error(), "echo") {
		t.Fatalf("provider error leaked sensitive response content: %v", err)
	}
}

func TestChatCachesIdenticalRequestsWithinOrganization(t *testing.T) {
	testsupport.OpenDatabase(t)
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"cached"}}]}`))
	}))
	defer server.Close()
	cfg := &ServiceConfig{OrganizationID: 77, ID: 3, Provider: "openai", BaseURL: server.URL, APIKey: "test", Model: "test-model"}
	for range 2 {
		result, err := ChatWithMaxTokens(context.Background(), cfg, "system", "same prompt", 0.2, 100)
		if err != nil || result != "cached" {
			t.Fatalf("result=%q err=%v", result, err)
		}
	}
	if requestCount != 1 {
		t.Fatalf("provider request count=%d want 1", requestCount)
	}
	cfg.UpdatedAt = "new-version"
	if _, err := ChatWithMaxTokens(context.Background(), cfg, "system", "same prompt", 0.2, 100); err != nil {
		t.Fatal(err)
	}
	if requestCount != 2 {
		t.Fatalf("updated config reused stale cache, count=%d", requestCount)
	}
	other := *cfg
	other.OrganizationID = 78
	if _, err := ChatWithMaxTokens(context.Background(), &other, "system", "same prompt", 0.2, 100); err != nil {
		t.Fatal(err)
	}
	if requestCount != 3 {
		t.Fatalf("cross-organization cache leaked, count=%d", requestCount)
	}
}

func TestPreferredConfigMustMatchServiceType(t *testing.T) {
	database := testsupport.OpenDatabase(t)
	now := response.Now()
	row := models.AIServiceConfig{
		ServiceType: "text", Provider: "mock", Name: "text-only", BaseURL: "http://localhost",
		APIKey: "mock", Model: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := database.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := GetActiveConfig("image", &row.ID); err == nil {
		t.Fatal("text config was accepted for image generation")
	}
}

func TestTaskConfigCanLoadInactiveOriginalConfig(t *testing.T) {
	database := testsupport.OpenDatabase(t)
	now := response.Now()
	row := models.AIServiceConfig{
		ServiceType: "video", Provider: "vidu", Name: "disabled-after-submit", BaseURL: "https://api.example.com",
		APIKey: "secret", Model: "video", IsActive: false, CreatedAt: now, UpdatedAt: now,
	}
	if err := database.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	config, err := GetTaskConfig("video", &row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if config.ID != row.ID || config.Provider != "vidu" {
		t.Fatalf("unexpected task config: %+v", config)
	}
}

func TestOrganizationConfigCannotCrossTenant(t *testing.T) {
	database := testsupport.OpenDatabase(t)
	now := response.Now()
	row := models.AIServiceConfig{OrganizationID: 2, ServiceType: "image", Provider: "mock", Name: "tenant-b", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := GetOrganizationConfig(1, "image", &row.ID); err == nil {
		t.Fatal("tenant A selected tenant B config")
	}
}

func TestConfigSelectionPriorityTaskFallbackAndDecryption(t *testing.T) {
	database := testsupport.OpenDatabase(t)
	t.Setenv("AI_CONFIG_ENCRYPTION_KEY", "selection-key")
	protected, err := security.EncryptSecret("provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	rows := []models.AIServiceConfig{
		{ServiceType: "text", Provider: "openai", Name: "priority", BaseURL: "https://priority.test", APIKey: "plain", Model: "priority-model", Priority: 100, IsActive: true, CreatedAt: now, UpdatedAt: now},
		{ServiceType: "text", Provider: "openai", Name: "default", BaseURL: "https://default.test", APIKey: protected, Model: "default-model", IsDefault: true, IsActive: true, CreatedAt: now, UpdatedAt: now},
		{OrganizationID: 7, ServiceType: "image", Provider: "mock", Name: "low", APIKey: "mock", Priority: 1, IsActive: true, CreatedAt: now, UpdatedAt: now},
		{OrganizationID: 7, ServiceType: "image", Provider: "mock", Name: "high", APIKey: "mock", Priority: 9, IsActive: true, CreatedAt: now, UpdatedAt: now},
	}
	for i := range rows {
		if err := database.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	active, err := GetActiveConfig("text", nil)
	if err != nil || active.ID != rows[1].ID || active.APIKey != "provider-secret" {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	preferred, err := GetActiveConfig("text", &rows[0].ID)
	if err != nil || preferred.ID != rows[0].ID {
		t.Fatalf("preferred=%+v err=%v", preferred, err)
	}
	organization, err := GetOrganizationConfig(7, "image", nil)
	if err != nil || organization.ID != rows[3].ID {
		t.Fatalf("organization=%+v err=%v", organization, err)
	}
	if _, err := GetOrganizationConfig(0, "image", nil); err == nil {
		t.Fatal("zero organization accepted")
	}
	task, err := GetTaskConfigOrganization(7, "image", nil)
	if err != nil || task.ID != rows[3].ID {
		t.Fatalf("task=%+v err=%v", task, err)
	}
	if _, err := GetActiveConfig("audio", nil); err == nil {
		t.Fatal("missing active config accepted")
	}
	byID, err := GetByID(rows[1].ID)
	if err != nil || byID.APIKey != "provider-secret" {
		t.Fatalf("byID=%+v err=%v", byID, err)
	}
	if _, err := GetByID(9999); err == nil {
		t.Fatal("missing config ID accepted")
	}
}

func TestTaskConfigRejectsLegacyInsecureRemoteURL(t *testing.T) {
	database := testsupport.OpenDatabase(t)
	now := response.Now()
	remote := models.AIServiceConfig{OrganizationID: 7, ServiceType: "image", Provider: "openai", Name: "legacy-http", BaseURL: "http://api.example.com", APIKey: "secret", Model: "image", IsActive: true, CreatedAt: now, UpdatedAt: now}
	local := models.AIServiceConfig{OrganizationID: 7, ServiceType: "text", Provider: "openai_local", Name: "local-http", BaseURL: "http://127.0.0.1:11434", Model: "local", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&remote).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&local).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := GetTaskConfigOrganization(7, "image", &remote.ID); err == nil {
		t.Fatal("legacy remote HTTP config was accepted")
	}
	if _, err := GetTaskConfigOrganization(7, "text", &local.ID); err != nil {
		t.Fatalf("explicit local HTTP config rejected: %v", err)
	}
}

func TestPrivateProviderRequiresValidCAAndExactHostAllowlist(t *testing.T) {
	t.Setenv("AI_PROVIDER_CA_FILE", filepath.Join(t.TempDir(), "missing-ca.pem"))
	t.Setenv("AI_PROVIDER_PRIVATE_HOSTS", "127.0.0.1")
	if err := validateProviderURL("vidu", "https://127.0.0.1:9443"); err == nil {
		t.Fatal("invalid CA file enabled a private provider")
	}
}

func TestProviderURLRejectsReservedMetadataAddress(t *testing.T) {
	t.Setenv("AI_PROVIDER_CA_FILE", "")
	t.Setenv("AI_PROVIDER_PRIVATE_HOSTS", "")
	if err := validateProviderURL("openai", "https://100.100.100.200/v1"); err == nil {
		t.Fatal("reserved metadata address was accepted")
	}
}

func TestChatDefaultModelWrapperAndEmptyResponse(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
			t.Errorf("path=%q", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "gpt-4o-mini" {
			t.Errorf("model=%v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"wrapper ok"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()
	cfg := &ServiceConfig{Provider: "openai_local", BaseURL: server.URL + "/", APIKey: ""}
	result, err := Chat(context.Background(), cfg, "system", "user", 0)
	if err != nil || result != "wrapper ok" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if _, err := ChatWithMaxTokens(context.Background(), cfg, "system", "different user", 0, 0); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty response error=%v", err)
	}
}

func TestMapConfigRejectsUnreadableEncryptedCredential(t *testing.T) {
	t.Setenv("AI_CONFIG_ENCRYPTION_KEY", "temporary")
	protected, err := security.EncryptSecret("secret")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_CONFIG_ENCRYPTION_KEY", "")
	mapped, err := mapConfig(models.AIServiceConfig{ID: 1, Provider: "mock", APIKey: protected})
	if err == nil || mapped != nil {
		t.Fatalf("unreadable credential accepted: config=%+v err=%v", mapped, err)
	}
}
