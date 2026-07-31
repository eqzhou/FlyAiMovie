package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/security"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func serviceBundleTestRouter(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	server, _ := testServerRouter(t)
	if err := db.DB.AutoMigrate(&models.AIServiceBundle{}); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	api := router.Group("/api/v1", server.protectBusinessAPI(), server.auditMutations())
	server.registerServiceBundles(api)
	return server, router
}

func fourServiceDraft(key string) string {
	return `{"services":[
		{"service_type":"text","provider":"mock","name":"bundle-text","base_url":"http://localhost","model":"mock","api_key":"` + key + `","is_default":true},
		{"service_type":"image","provider":"mock","name":"bundle-image","base_url":"http://localhost","model":"mock","api_key":"` + key + `","is_default":true},
		{"service_type":"video","provider":"mock","name":"bundle-video","base_url":"http://localhost","model":"mock","api_key":"` + key + `","is_default":true},
		{"service_type":"audio","provider":"mock","name":"bundle-audio","base_url":"http://localhost","model":"mock","api_key":"` + key + `","is_default":true}
	]}`
}

func TestServiceBundlePreviewIsReadOnlyReportsReuseAndHidesCredentials(t *testing.T) {
	_, router := serviceBundleTestRouter(t)
	existing := models.AIServiceConfig{ServiceType: "text", Provider: "mock", Name: "old-text", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsDefault: true, IsActive: true, CreatedAt: "old", UpdatedAt: "old"}
	if err := db.DB.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	var before int64
	db.DB.Model(&models.AIServiceConfig{}).Count(&before)

	secret := "preview-secret-that-must-not-leak"
	preview := performRequest(router, http.MethodPost, "/api/v1/service-bundles/preview", fourServiceDraft(secret), nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	if strings.Contains(preview.Body.String(), secret) || strings.Contains(preview.Body.String(), "api_key") {
		t.Fatalf("preview leaked credential: %s", preview.Body.String())
	}
	var after int64
	db.DB.Model(&models.AIServiceConfig{}).Count(&after)
	if after != before {
		t.Fatalf("preview wrote configs: before=%d after=%d", before, after)
	}
	data := decodeResponse(t, preview)["data"].(map[string]any)
	if data["preview_token"] == "" || len(data["items"].([]any)) != 4 {
		t.Fatalf("unexpected preview: %s", preview.Body.String())
	}
	if len(data["conflicts"].([]any)) == 0 {
		t.Fatalf("expected reuse/default conflict report: %s", preview.Body.String())
	}
}

func TestServiceBundlePreviewAndTestAreAuditedWithoutCredentialMaterial(t *testing.T) {
	_, router := serviceBundleTestRouter(t)
	secret := "bundle-audit-secret-must-not-leak"

	for _, endpoint := range []string{"/api/v1/service-bundles/preview", "/api/v1/service-bundles/test"} {
		response := performRequest(router, http.MethodPost, endpoint, fourServiceDraft(secret), nil)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", endpoint, response.Code, response.Body.String())
		}
	}

	var rows []models.AuditLog
	if err := db.DB.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("audit count=%d want=2", len(rows))
	}
	wantedResources := map[string]bool{"service_bundle_preview": true, "service-bundles": true}
	for _, row := range rows {
		serialized := row.Action + row.ResourceType + row.ResourceID + row.Method + row.Path + row.SourceIP
		if strings.Contains(serialized, secret) || strings.Contains(serialized, "api_key") {
			t.Fatalf("audit log captured credential material: %+v", row)
		}
		if !wantedResources[row.ResourceType] {
			t.Fatalf("unexpected audit resource: %+v", row)
		}
		delete(wantedResources, row.ResourceType)
	}
	if len(wantedResources) != 0 {
		t.Fatalf("missing audit resources: %+v", wantedResources)
	}
}

func TestServiceBundleApplyCreatesOrReusesAllFourAtomicallyAndEncrypts(t *testing.T) {
	t.Setenv("AI_CONFIG_ENCRYPTION_KEY", "bundle-test-encryption-key")
	_, router := serviceBundleTestRouter(t)
	preview := performRequest(router, http.MethodPost, "/api/v1/service-bundles/preview", fourServiceDraft("new-secret"), nil)
	token := decodeResponse(t, preview)["data"].(map[string]any)["preview_token"].(string)
	body := strings.TrimSuffix(fourServiceDraft("new-secret"), "}") + `,"preview_token":"` + token + `"}`
	applied := performRequest(router, http.MethodPost, "/api/v1/service-bundles/apply", body, nil)
	if applied.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", applied.Code, applied.Body.String())
	}
	if strings.Contains(applied.Body.String(), "new-secret") || strings.Contains(applied.Body.String(), "api_key") {
		t.Fatalf("apply leaked credential: %s", applied.Body.String())
	}
	var rows []models.AIServiceConfig
	if err := db.DB.Where("name LIKE ?", "bundle-%").Order("service_type").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("created=%d want=4", len(rows))
	}
	for _, row := range rows {
		plain, err := security.DecryptSecret(row.APIKey)
		if err != nil || plain != "new-secret" || row.APIKey == plain || !row.IsDefault {
			t.Fatalf("invalid protected/default row: %+v decrypt=%q err=%v", row, plain, err)
		}
	}

	secondPreview := performRequest(router, http.MethodPost, "/api/v1/service-bundles/preview", fourServiceDraft("replacement-secret"), nil)
	secondToken := decodeResponse(t, secondPreview)["data"].(map[string]any)["preview_token"].(string)
	secondBody := strings.TrimSuffix(fourServiceDraft("replacement-secret"), "}") + `,"preview_token":"` + secondToken + `"}`
	second := performRequest(router, http.MethodPost, "/api/v1/service-bundles/apply", secondBody, nil)
	if second.Code != http.StatusOK {
		t.Fatalf("second apply status=%d body=%s", second.Code, second.Body.String())
	}
	var count int64
	db.DB.Model(&models.AIServiceConfig{}).Where("name LIKE ?", "bundle-%").Count(&count)
	if count != 4 {
		t.Fatalf("reapply duplicated configs: %d", count)
	}
}

func TestServiceBundleApplyRejectsStalePreviewAndRollsBackInvalidDraft(t *testing.T) {
	_, router := serviceBundleTestRouter(t)
	draft := fourServiceDraft("secret")
	preview := performRequest(router, http.MethodPost, "/api/v1/service-bundles/preview", draft, nil)
	token := decodeResponse(t, preview)["data"].(map[string]any)["preview_token"].(string)
	if err := db.DB.Create(&models.AIServiceConfig{ServiceType: "text", Provider: "mock", Name: "changed", BaseURL: "http://changed", Model: "mock", APIKey: "mock", IsActive: true, CreatedAt: "now", UpdatedAt: "now"}).Error; err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(draft, "}") + `,"preview_token":"` + token + `"}`
	stale := performRequest(router, http.MethodPost, "/api/v1/service-bundles/apply", body, nil)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "stale") {
		t.Fatalf("stale status=%d body=%s", stale.Code, stale.Body.String())
	}
	var count int64
	db.DB.Model(&models.AIServiceConfig{}).Where("name LIKE ?", "bundle-%").Count(&count)
	if count != 0 {
		t.Fatalf("stale apply wrote %d rows", count)
	}

	var invalid map[string]any
	if err := json.Unmarshal([]byte(fourServiceDraft("secret")), &invalid); err != nil {
		t.Fatal(err)
	}
	services := invalid["services"].([]any)
	services[3].(map[string]any)["provider"] = "openai"
	invalidJSON, _ := json.Marshal(invalid)
	badPreview := performRequest(router, http.MethodPost, "/api/v1/service-bundles/preview", string(invalidJSON), nil)
	if badPreview.Code != http.StatusBadRequest {
		t.Fatalf("invalid preview status=%d body=%s", badPreview.Code, badPreview.Body.String())
	}
}

func TestServiceBundleTemplatesAreCredentialFreeAndOrganizationScoped(t *testing.T) {
	_, router := serviceBundleTestRouter(t)
	global := models.AIServiceBundle{OrganizationID: 0, Key: "global", Name: "Global", ServicesJSON: `[{"service_type":"text","provider":"mock","name":"g","base_url":"http://localhost","model":"mock"}]`, IsBuiltin: true, IsActive: true, CreatedAt: "now", UpdatedAt: "now"}
	foreign := models.AIServiceBundle{OrganizationID: 9, Key: "foreign", Name: "Foreign", ServicesJSON: global.ServicesJSON, IsActive: true, CreatedAt: "now", UpdatedAt: "now"}
	if err := db.DB.Create(&global).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	listed := performRequest(router, http.MethodGet, "/api/v1/service-bundles", "", nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"key":"global"`) || strings.Contains(listed.Body.String(), `"key":"foreign"`) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	if strings.Contains(listed.Body.String(), "api_key") {
		t.Fatalf("template response contains credential field: %s", listed.Body.String())
	}
}

func TestServiceBundleRejectsUnknownFields(t *testing.T) {
	_, router := serviceBundleTestRouter(t)
	body := strings.TrimSuffix(fourServiceDraft("secret"), "}") + `,"unexpected":true}`
	response := performRequest(router, http.MethodPost, "/api/v1/service-bundles/preview", body, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestServiceBundleTemplateRejectsCredentialsNestedInSettingsJSON(t *testing.T) {
	_, _ = serviceBundleTestRouter(t)
	row := models.AIServiceBundle{
		Key: "unsafe-settings", Name: "Unsafe", IsActive: true, CreatedAt: "now", UpdatedAt: "now",
		ServicesJSON: `[{"service_type":"text","provider":"mock","name":"text","base_url":"http://localhost","model":"mock","settings":"{\"api_key\":\"secret\"}"}]`,
	}
	if err := db.DB.Create(&row).Error; err == nil {
		t.Fatal("expected nested settings credential to be rejected")
	}
}

func TestServiceBundleTestAllReportsFourResultsWithoutWriting(t *testing.T) {
	_, router := serviceBundleTestRouter(t)
	before := int64(0)
	db.DB.Model(&models.AIServiceConfig{}).Count(&before)
	response := performRequest(router, http.MethodPost, "/api/v1/service-bundles/test", fourServiceDraft("secret"), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("test-all status=%d body=%s", response.Code, response.Body.String())
	}
	data := decodeResponse(t, response)["data"].(map[string]any)
	if len(data["results"].([]any)) != 4 {
		t.Fatalf("results=%s", response.Body.String())
	}
	after := int64(0)
	db.DB.Model(&models.AIServiceConfig{}).Count(&after)
	if after != before {
		t.Fatalf("connection tests wrote configs: before=%d after=%d", before, after)
	}
}

func TestServiceBundleDefaultsFiveAgentsFromTextModelAndPreservesCustomFields(t *testing.T) {
	t.Setenv("AI_CONFIG_ENCRYPTION_KEY", "bundle-agent-defaults-key")
	_, router := serviceBundleTestRouter(t)
	temperature, maxTokens, maxIterations := 1.25, 7777, 3
	deleted := "2026-07-31T00:00:00Z"
	existing := []models.AgentConfig{
		{AgentType: "script_rewriter", Name: "Custom writer", Description: "custom description", Model: "mock", SystemPrompt: "custom system", Temperature: &temperature, MaxTokens: &maxTokens, MaxIterations: &maxIterations, IsActive: true, CreatedAt: "old", UpdatedAt: "old"},
		{AgentType: "extractor", Name: "Custom extractor", Description: "keep description", Model: "old-model", SystemPrompt: "keep prompt", Temperature: &temperature, MaxTokens: &maxTokens, MaxIterations: &maxIterations, IsActive: false, DeletedAt: &deleted, CreatedAt: "old", UpdatedAt: "old"},
	}
	if err := db.DB.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}

	preview := performRequest(router, http.MethodPost, "/api/v1/service-bundles/preview", fourServiceDraft("secret"), nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	previewData := decodeResponse(t, preview)["data"].(map[string]any)
	agents := previewData["agents"].([]any)
	if len(agents) != 5 {
		t.Fatalf("agents=%s", preview.Body.String())
	}
	actions := map[string]string{}
	for _, raw := range agents {
		item := raw.(map[string]any)
		actions[item["agent_type"].(string)] = item["action"].(string)
		if item["model"] != "mock" {
			t.Fatalf("agent does not use text model: %v", item)
		}
	}
	if actions["script_rewriter"] != "reuse" || actions["extractor"] != "update" || actions["storyboard_breaker"] != "create" {
		t.Fatalf("actions=%v", actions)
	}

	token := previewData["preview_token"].(string)
	body := strings.TrimSuffix(fourServiceDraft("secret"), "}") + `,"preview_token":"` + token + `"}`
	applied := performRequest(router, http.MethodPost, "/api/v1/service-bundles/apply", body, nil)
	if applied.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", applied.Code, applied.Body.String())
	}
	var rows []models.AgentConfig
	if err := db.DB.Order("agent_type").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("agent rows=%d want=5: %+v", len(rows), rows)
	}
	var updated models.AgentConfig
	if err := db.DB.First(&updated, existing[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Model != "mock" || !updated.IsActive || updated.DeletedAt != nil {
		t.Fatalf("agent defaults not synchronized: %+v", updated)
	}
	if updated.Name != "Custom extractor" || updated.Description != "keep description" || updated.SystemPrompt != "keep prompt" || updated.Temperature == nil || *updated.Temperature != temperature || updated.MaxTokens == nil || *updated.MaxTokens != maxTokens || updated.MaxIterations == nil || *updated.MaxIterations != maxIterations {
		t.Fatalf("custom agent fields were overwritten: %+v", updated)
	}
}

func TestServiceBundleAgentChangeMakesPreviewStaleAndAgentFailureRollsBackServices(t *testing.T) {
	t.Setenv("AI_CONFIG_ENCRYPTION_KEY", "bundle-agent-transaction-key")
	_, router := serviceBundleTestRouter(t)
	draft := fourServiceDraft("secret")
	preview := performRequest(router, http.MethodPost, "/api/v1/service-bundles/preview", draft, nil)
	token := decodeResponse(t, preview)["data"].(map[string]any)["preview_token"].(string)
	if err := db.DB.Create(&models.AgentConfig{AgentType: "script_rewriter", Name: "changed", Model: "other", IsActive: true, CreatedAt: "now", UpdatedAt: "now"}).Error; err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(draft, "}") + `,"preview_token":"` + token + `"}`
	stale := performRequest(router, http.MethodPost, "/api/v1/service-bundles/apply", body, nil)
	if stale.Code != http.StatusConflict {
		t.Fatalf("agent stale status=%d body=%s", stale.Code, stale.Body.String())
	}

	if err := db.DB.Exec(`DELETE FROM agent_configs`).Error; err != nil {
		t.Fatal(err)
	}
	preview = performRequest(router, http.MethodPost, "/api/v1/service-bundles/preview", draft, nil)
	token = decodeResponse(t, preview)["data"].(map[string]any)["preview_token"].(string)
	const callbackName = "test:fail_bundle_agent_create"
	if err := db.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "AgentConfig" {
			tx.AddError(errors.New("agent insert blocked"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DB.Callback().Create().Remove(callbackName) })
	body = strings.TrimSuffix(draft, "}") + `,"preview_token":"` + token + `"}`
	failed := performRequest(router, http.MethodPost, "/api/v1/service-bundles/apply", body, nil)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("agent failure status=%d body=%s", failed.Code, failed.Body.String())
	}
	var services int64
	db.DB.Model(&models.AIServiceConfig{}).Where("name LIKE ?", "bundle-%").Count(&services)
	if services != 0 {
		t.Fatalf("service writes escaped failed transaction: %d", services)
	}
}

func TestServiceBundleCanExplicitlySkipAgentDefaults(t *testing.T) {
	_, router := serviceBundleTestRouter(t)
	body := strings.TrimSuffix(fourServiceDraft("secret"), "}") + `,"apply_agent_defaults":false}`
	preview := performRequest(router, http.MethodPost, "/api/v1/service-bundles/preview", body, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	data := decodeResponse(t, preview)["data"].(map[string]any)
	if agents, ok := data["agents"].([]any); !ok || len(agents) != 0 {
		t.Fatalf("agents should be empty: %s", preview.Body.String())
	}
}

func TestServiceBundleRequiresTextModelWhenSynchronizingAgents(t *testing.T) {
	_, router := serviceBundleTestRouter(t)
	var draft map[string]any
	if err := json.Unmarshal([]byte(fourServiceDraft("secret")), &draft); err != nil {
		t.Fatal(err)
	}
	draft["services"].([]any)[0].(map[string]any)["model"] = ""
	body, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}

	response := performRequest(router, http.MethodPost, "/api/v1/service-bundles/preview", string(body), nil)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "text model") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	draft["apply_agent_defaults"] = false
	body, err = json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	response = performRequest(router, http.MethodPost, "/api/v1/service-bundles/preview", string(body), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("opt-out status=%d body=%s", response.Code, response.Body.String())
	}
}
