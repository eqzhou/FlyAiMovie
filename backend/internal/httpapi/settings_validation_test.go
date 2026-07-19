package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
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

func TestValidateAIConfigRejectsPlainHTTPForRemoteProviders(t *testing.T) {
	if err := validateAIConfigInput("image", "openai", "test", "http://api.example.com/v1", "key"); err == nil {
		t.Fatal("expected a remote provider using plain HTTP to be rejected")
	}
}

func TestValidateAIConfigRejectsReservedMetadataAddress(t *testing.T) {
	if err := validateAIConfigInput("image", "openai", "reserved", "https://100.100.100.200/v1", "secret"); err == nil {
		t.Fatal("reserved metadata address was accepted")
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

func TestAIConfigConnectionTestUsesStoredCredential(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer stored-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"local-model"}]}`))
	}))
	defer provider.Close()

	server, router := testServerRouter(t)
	server.Cfg.AI.AllowedPrivateBaseURLHosts = []string{"127.0.0.1"}
	created := performRequest(router, http.MethodPost, "/api/v1/ai-configs", `{
		"service_type":"text","provider":"openai_local","name":"Local",
		"base_url":"`+provider.URL+`","api_key":"stored-key","model":"local-model"
	}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	id := decodeResponse(t, created)["data"].(map[string]any)["id"].(float64)
	probed := performRequest(router, http.MethodPost, "/api/v1/ai-configs/"+jsonNumber(id)+"/test", `{}`, nil)
	if probed.Code != http.StatusOK || !strings.Contains(probed.Body.String(), `"status":"ok"`) || strings.Contains(probed.Body.String(), "stored-key") {
		t.Fatalf("probe status=%d body=%s", probed.Code, probed.Body.String())
	}
}

func TestAIConfigConnectionTestRejectsLegacyInsecureBaseURL(t *testing.T) {
	requested := false
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
		w.WriteHeader(http.StatusOK)
	}))
	defer provider.Close()

	_, router := testServerRouter(t)
	row := models.AIServiceConfig{
		ServiceType: "image",
		Provider:    "openai",
		Name:        "Legacy insecure config",
		BaseURL:     provider.URL,
		APIKey:      "stored-key",
		Model:       "gpt-image-1",
	}
	if err := db.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	probed := performRequest(router, http.MethodPost, "/api/v1/ai-configs/"+itoa(row.ID)+"/test", `{}`, nil)
	if probed.Code != http.StatusBadRequest {
		t.Fatalf("probe status=%d body=%s", probed.Code, probed.Body.String())
	}
	if requested {
		t.Fatal("insecure provider endpoint received a connection probe")
	}
}

func TestAIConfigConnectionTestRejectsInvalidID(t *testing.T) {
	_, router := testServerRouter(t)
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/ai-configs/not-an-id/test", `{}`, http.StatusBadRequest)
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/ai-configs/99999/test", `{}`, http.StatusNotFound)
}

func TestAIConfigDraftConnectionTestUsesUnsavedFieldsWithoutPersistence(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer stored-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"draft-model"}]}`))
	}))
	defer provider.Close()

	server, router := testServerRouter(t)
	server.Cfg.AI.AllowedPrivateBaseURLHosts = []string{"127.0.0.1"}
	created := performRequest(router, http.MethodPost, "/api/v1/ai-configs", `{
		"service_type":"text","provider":"openai_local","name":"Local",
		"base_url":"`+provider.URL+`","api_key":"stored-key","model":"stored-model"
	}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	id := uint(decodeResponse(t, created)["data"].(map[string]any)["id"].(float64))
	probed := performRequest(router, http.MethodPost, "/api/v1/ai-configs/test", `{
		"id":`+itoa(id)+`,"service_type":"text","provider":"openai_local","name":"Local Draft",
		"base_url":"`+provider.URL+`","model":"draft-model","api_key":""
	}`, nil)
	if probed.Code != http.StatusOK || !strings.Contains(probed.Body.String(), `"status":"ok"`) || strings.Contains(probed.Body.String(), "stored-key") {
		t.Fatalf("probe status=%d body=%s", probed.Code, probed.Body.String())
	}
	var row models.AIServiceConfig
	if err := db.DB.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	if row.Name != "Local" || row.Model != "stored-model" {
		t.Fatalf("draft probe mutated persisted row: %+v", row)
	}
}

func TestAIConfigDraftConnectionTestDoesNotSendStoredKeyToChangedBaseURL(t *testing.T) {
	var leakedAuthorization string
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leakedAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer attacker.Close()

	server, router := testServerRouter(t)
	server.Cfg.AI.AllowedPrivateBaseURLHosts = []string{"127.0.0.1"}
	created := performRequest(router, http.MethodPost, "/api/v1/ai-configs", `{
		"service_type":"text","provider":"openai_local","name":"Local",
		"base_url":"http://127.0.0.1:11434","api_key":"stored-key","model":"stored-model"
	}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	id := uint(decodeResponse(t, created)["data"].(map[string]any)["id"].(float64))
	probed := performRequest(router, http.MethodPost, "/api/v1/ai-configs/test", `{
		"id":`+itoa(id)+`,"service_type":"text","provider":"openai_local","name":"Changed Host",
		"base_url":"`+attacker.URL+`","model":"draft-model","api_key":""
	}`, nil)
	if probed.Code != http.StatusBadRequest {
		t.Fatalf("probe status=%d body=%s", probed.Code, probed.Body.String())
	}
	if leakedAuthorization != "" {
		t.Fatalf("stored credential was sent to changed endpoint: %q", leakedAuthorization)
	}
}

func TestAIConfigDraftConnectionTestIsIsolatedByOrganization(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	cookieA, csrfA, organizationA := createTestActorSession(t, server, "config-owner-a@example.com", "config-org-a", "owner")
	cookieB, csrfB, _ := createTestActorSession(t, server, "config-owner-b@example.com", "config-org-b", "owner")

	created := performAuthRequest(router, http.MethodPost, "/api/v1/ai-configs", `{
		"service_type":"text","provider":"mock","name":"Tenant A config","model":"mock"
	}`, cookieA, csrfA)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	id := uint(decodeResponse(t, created)["data"].(map[string]any)["id"].(float64))
	var row models.AIServiceConfig
	if err := db.DB.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	if row.OrganizationID != organizationA.ID {
		t.Fatalf("organization_id=%d want %d", row.OrganizationID, organizationA.ID)
	}

	probed := performAuthRequest(router, http.MethodPost, "/api/v1/ai-configs/test", `{
		"id":`+itoa(id)+`,"service_type":"text","provider":"mock","name":"Tenant B probe","model":"mock"
	}`, cookieB, csrfB)
	if probed.Code != http.StatusNotFound {
		t.Fatalf("cross-organization probe status=%d body=%s", probed.Code, probed.Body.String())
	}
}

func TestUpdateAIConfigRequiresNewKeyWhenEndpointIdentityChanges(t *testing.T) {
	server, router := testServerRouter(t)
	server.Cfg.AI.AllowedPrivateBaseURLHosts = []string{"127.0.0.1"}
	created := performRequest(router, http.MethodPost, "/api/v1/ai-configs", `{
		"service_type":"text","provider":"openai_local","name":"Local",
		"base_url":"http://127.0.0.1:11434","api_key":"stored-key","model":"stored-model"
	}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	id := uint(decodeResponse(t, created)["data"].(map[string]any)["id"].(float64))
	updated := performRequest(router, http.MethodPut, "/api/v1/ai-configs/"+itoa(id), `{
		"service_type":"text","provider":"openai_local","name":"Changed Host",
		"base_url":"http://127.0.0.1:11435","model":"draft-model","api_key":""
	}`, nil)
	if updated.Code != http.StatusBadRequest {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	var row models.AIServiceConfig
	if err := db.DB.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	if row.BaseURL != "http://127.0.0.1:11434" || row.Model != "stored-model" {
		t.Fatalf("rejected update mutated stored config: %+v", row)
	}
}

func TestAIConfigDraftConnectionTestRejectsInvalidDrafts(t *testing.T) {
	_, router := testServerRouter(t)

	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "invalid JSON", body: `{`, want: http.StatusBadRequest},
		{name: "unknown config", body: `{"id":99999}`, want: http.StatusNotFound},
		{name: "oversized field", body: `{"service_type":"text","provider":"mock","name":"` + strings.Repeat("a", maxNameRunes+1) + `"}`, want: http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			probed := performRequest(router, http.MethodPost, "/api/v1/ai-configs/test", tc.body, nil)
			if probed.Code != tc.want {
				t.Fatalf("probe status=%d body=%s", probed.Code, probed.Body.String())
			}
		})
	}
}

func TestAIConfigDraftConnectionTestHonorsExplicitEmptyFields(t *testing.T) {
	_, router := testServerRouter(t)
	created := performRequest(router, http.MethodPost, "/api/v1/ai-configs", `{
		"service_type":"image","provider":"mock","name":"Mock image",
		"base_url":"http://localhost","api_key":"mock","model":"stored-model"
	}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	id := uint(decodeResponse(t, created)["data"].(map[string]any)["id"].(float64))
	missingBaseURL := performRequest(router, http.MethodPost, "/api/v1/ai-configs/test", `{
		"id":`+itoa(id)+`,"service_type":"image","provider":"openai","name":"No URL",
		"base_url":"","api_key":"replacement","model":"gpt-image-1"
	}`, nil)
	if missingBaseURL.Code != http.StatusBadRequest {
		t.Fatalf("empty base URL status=%d body=%s", missingBaseURL.Code, missingBaseURL.Body.String())
	}
	emptyModel := performRequest(router, http.MethodPost, "/api/v1/ai-configs/test", `{
		"id":`+itoa(id)+`,"service_type":"image","provider":"mock","name":"Mock image",
		"base_url":"http://localhost","api_key":"","model":""
	}`, nil)
	if emptyModel.Code != http.StatusOK || !strings.Contains(emptyModel.Body.String(), `"model":""`) {
		t.Fatalf("empty model status=%d body=%s", emptyModel.Code, emptyModel.Body.String())
	}
}

func TestAIConfigProbeHelpers(t *testing.T) {
	empty := ""
	configured := "configured"
	if value := probeStringValue(&empty, "fallback"); value != "" {
		t.Fatalf("probeStringValue=%q", value)
	}
	if value := probeStringValue(&configured, "fallback"); value != "configured" {
		t.Fatalf("probeStringValue=%q", value)
	}
	if value := probeStringValue(nil, "fallback"); value != "fallback" {
		t.Fatalf("probeStringValue=%q", value)
	}
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{err: errors.New("database is locked"), want: true},
		{err: errors.New("database table is locked"), want: true},
		{err: errors.New("permission denied"), want: false},
	} {
		if got := isSQLiteBusyError(tc.err); got != tc.want {
			t.Fatalf("isSQLiteBusyError(%q)=%t want=%t", tc.err, got, tc.want)
		}
	}
}

func TestAIConfigDefaultIsExclusiveAndMustBeActive(t *testing.T) {
	router := testRouter(t)
	create := func(name string, defaultValue bool) *httptest.ResponseRecorder {
		return performRequest(router, http.MethodPost, "/api/v1/ai-configs", `{"service_type":"text","provider":"mock","name":"`+name+`","base_url":"http://localhost","api_key":"mock","model":"mock","is_default":`+strconv.FormatBool(defaultValue)+`,"is_active":true}`, nil)
	}
	first := create("first", true)
	second := create("second", true)
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("create first=%d second=%d", first.Code, second.Code)
	}
	firstID := uint(decodeResponse(t, first)["data"].(map[string]any)["id"].(float64))
	secondID := uint(decodeResponse(t, second)["data"].(map[string]any)["id"].(float64))
	var firstRow, secondRow models.AIServiceConfig
	if err := db.DB.First(&firstRow, firstID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.First(&secondRow, secondID).Error; err != nil {
		t.Fatal(err)
	}
	if firstRow.IsDefault || !secondRow.IsDefault {
		t.Fatalf("first=%v second=%v", firstRow.IsDefault, secondRow.IsDefault)
	}

	disabled := performRequest(router, http.MethodPut, "/api/v1/ai-configs/"+itoa(secondID), `{"is_active":false}`, nil)
	if disabled.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disabled.Code, disabled.Body.String())
	}
	if err := db.DB.First(&secondRow, secondID).Error; err != nil {
		t.Fatal(err)
	}
	if secondRow.IsActive || secondRow.IsDefault {
		t.Fatalf("disabled row=%+v", secondRow)
	}

	invalid := performRequest(router, http.MethodPut, "/api/v1/ai-configs/"+itoa(firstID), `{"is_default":true,"is_active":false}`, nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}
