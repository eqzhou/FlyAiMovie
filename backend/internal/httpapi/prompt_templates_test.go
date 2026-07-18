package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
)

func TestPromptTemplateWorkflow(t *testing.T) {
	_, router := testServerRouter(t)
	created := performRequest(router, http.MethodPost, "/api/v1/prompt-templates", `{
		"key":"custom_image","name":"自定义图片提示词","category":"image",
		"description":"项目图片生成","content":"为 {{drama_title}} 生成图片","is_active":true
	}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	id := decodeResponse(t, created)["data"].(map[string]any)["id"].(float64)

	preview := performRequest(router, http.MethodPost, "/api/v1/prompt-templates/"+jsonNumber(id)+"/preview", `{"variables":{"drama_title":"归途"}}`, nil)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), "为 归途 生成图片") {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}

	updated := performRequest(router, http.MethodPut, "/api/v1/prompt-templates/"+jsonNumber(id), `{"content":"{{episode_title}} 的画面","is_active":false}`, nil)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"version":2`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/prompt-templates", "", nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "custom_image") {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
}

func TestPromptTemplateValidationAndOrganizationIsolation(t *testing.T) {
	_, router := testServerRouter(t)
	bad := performRequest(router, http.MethodPost, "/api/v1/prompt-templates", `{"key":"bad","name":"Bad","category":"image","content":"{{secret}}"}`, nil)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("unknown variable status=%d body=%s", bad.Code, bad.Body.String())
	}

	row := models.PromptTemplate{OrganizationID: 9, Key: "other_org", Name: "Other", Category: "image", Content: "plain", Version: 1, IsActive: true, CreatedAt: "now", UpdatedAt: "now"}
	if err := db.DB.Create(&row).Error; err != nil {
		t.Fatalf("seed other organization: %v", err)
	}
	response := performRequest(router, http.MethodGet, "/api/v1/prompt-templates", "", nil)
	if strings.Contains(response.Body.String(), "other_org") {
		t.Fatalf("cross-organization prompt leaked: %s", response.Body.String())
	}
}

func TestPromptTemplateRestoreDeleteAndFailureSurfaces(t *testing.T) {
	_, router := testServerRouter(t)
	created := performRequest(router, http.MethodPost, "/api/v1/prompt-templates", `{"key":"script_rewriter","name":"Changed","category":"agent_system","content":"custom"}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %s", created.Body.String())
	}
	id := jsonNumber(decodeResponse(t, created)["data"].(map[string]any)["id"].(float64))

	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/api/v1/prompt-templates?category=agent_system", "", http.StatusOK},
		{http.MethodPost, "/api/v1/prompt-templates/" + id + "/restore-default", `{}`, http.StatusOK},
		{http.MethodPost, "/api/v1/prompt-templates/" + id + "/preview", `{"variables":{},"extra":true}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/prompt-templates/" + id + "/preview", `{"variables":{"drama_title":1}}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/prompt-templates/" + id, `{"category":"invalid"}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/prompt-templates/" + id, `{"unknown":true}`, http.StatusBadRequest},
		{http.MethodDelete, "/api/v1/prompt-templates/" + id, "", http.StatusOK},
		{http.MethodDelete, "/api/v1/prompt-templates/" + id, "", http.StatusNotFound},
		{http.MethodPut, "/api/v1/prompt-templates/bad", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/prompt-templates/999/restore-default", `{}`, http.StatusNotFound},
	} {
		response := performRequest(router, tc.method, tc.path, tc.body, nil)
		if response.Code != tc.status {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, response.Code, response.Body.String())
		}
	}

	custom := performRequest(router, http.MethodPost, "/api/v1/prompt-templates", `{"key":"custom_grid","name":"Grid","category":"grid","content":"plain"}`, nil)
	customID := jsonNumber(decodeResponse(t, custom)["data"].(map[string]any)["id"].(float64))
	restored := performRequest(router, http.MethodPost, "/api/v1/prompt-templates/"+customID+"/restore-default", `{}`, nil)
	if restored.Code != http.StatusBadRequest {
		t.Fatalf("custom restore status=%d", restored.Code)
	}
}
