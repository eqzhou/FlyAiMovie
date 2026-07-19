package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
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
	extraVariable := performRequest(router, http.MethodPost, "/api/v1/prompt-templates/"+jsonNumber(id)+"/preview", `{"variables":{"drama_title":"归途","episode_title":"不应发送"}}`, nil)
	if extraVariable.Code != http.StatusBadRequest {
		t.Fatalf("unused variable status=%d body=%s", extraVariable.Code, extraVariable.Body.String())
	}

	updated := performRequest(router, http.MethodPut, "/api/v1/prompt-templates/"+jsonNumber(id), `{"content":"{{episode_title}} 的画面","is_active":false}`, nil)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"version":2`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}

	history := performRequest(router, http.MethodGet, "/api/v1/prompt-templates/"+jsonNumber(id)+"/revisions", "", nil)
	if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), `"version":1`) || !strings.Contains(history.Body.String(), `"version":2`) {
		t.Fatalf("history status=%d body=%s", history.Code, history.Body.String())
	}
	restored := performRequest(router, http.MethodPost, "/api/v1/prompt-templates/"+jsonNumber(id)+"/revisions/1/restore", `{}`, nil)
	if restored.Code != http.StatusOK || !strings.Contains(restored.Body.String(), `"version":3`) || !strings.Contains(restored.Body.String(), "为 {{drama_title}} 生成图片") {
		t.Fatalf("restore revision status=%d body=%s", restored.Code, restored.Body.String())
	}
	history = performRequest(router, http.MethodGet, "/api/v1/prompt-templates/"+jsonNumber(id)+"/revisions", "", nil)
	if strings.Count(history.Body.String(), `"version":`) != 3 {
		t.Fatalf("expected three immutable revisions: %s", history.Body.String())
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
	oversized := performRequest(router, http.MethodPost, "/api/v1/prompt-templates", `{"key":"too_large","name":"Too large","category":"image","content":"`+strings.Repeat("x", maxTextRunes+1)+`"}`, nil)
	if oversized.Code != http.StatusBadRequest {
		t.Fatalf("oversized content status=%d body=%s", oversized.Code, oversized.Body.String())
	}

	row := models.PromptTemplate{OrganizationID: 9, Key: "other_org", Name: "Other", Category: "image", Content: "plain", Version: 1, IsActive: true, CreatedAt: "now", UpdatedAt: "now"}
	if err := db.DB.Create(&row).Error; err != nil {
		t.Fatalf("seed other organization: %v", err)
	}
	response := performRequest(router, http.MethodGet, "/api/v1/prompt-templates", "", nil)
	if strings.Contains(response.Body.String(), "other_org") {
		t.Fatalf("cross-organization prompt leaked: %s", response.Body.String())
	}
	history := performRequest(router, http.MethodGet, "/api/v1/prompt-templates/"+jsonNumber(float64(row.ID))+"/revisions", "", nil)
	if history.Code != http.StatusNotFound {
		t.Fatalf("cross-organization history status=%d body=%s", history.Code, history.Body.String())
	}
	restore := performRequest(router, http.MethodPost, "/api/v1/prompt-templates/"+jsonNumber(float64(row.ID))+"/revisions/1/restore", `{}`, nil)
	if restore.Code != http.StatusNotFound {
		t.Fatalf("cross-organization restore status=%d body=%s", restore.Code, restore.Body.String())
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
		{http.MethodGet, "/api/v1/prompt-templates/" + id + "/revisions", "", http.StatusOK},
		{http.MethodPost, "/api/v1/prompt-templates/" + id + "/revisions/bad/restore", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/prompt-templates/" + id + "/revisions/999/restore", `{}`, http.StatusNotFound},
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

func TestPromptTemplateRecreateAfterSoftDelete(t *testing.T) {
	_, router := testServerRouter(t)
	created := performRequest(router, http.MethodPost, "/api/v1/prompt-templates", `{
		"key":"revive_image","name":"Old","category":"image","content":"old {{drama_title}}"
	}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	data := decodeResponse(t, created)["data"].(map[string]any)
	id := jsonNumber(data["id"].(float64))

	deleted := performRequest(router, http.MethodDelete, "/api/v1/prompt-templates/"+id, "", nil)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}

	recreated := performRequest(router, http.MethodPost, "/api/v1/prompt-templates", `{
		"key":"revive_image","name":"New","category":"image","content":"new {{drama_title}}"
	}`, nil)
	if recreated.Code != http.StatusOK {
		t.Fatalf("recreate after soft-delete status=%d body=%s", recreated.Code, recreated.Body.String())
	}
	body := recreated.Body.String()
	if !strings.Contains(body, `"id":`+id) || !strings.Contains(body, "new {{drama_title}}") || !strings.Contains(body, `"name":"New"`) {
		t.Fatalf("expected revived template: %s", body)
	}
	if strings.Contains(body, `"deleted_at"`) && !strings.Contains(body, `"deleted_at":null`) && !strings.Contains(body, `"deleted_at": null`) {
		// omitempty should drop null, but if present ensure not a timestamp
		t.Fatalf("revived template still looks deleted: %s", body)
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/prompt-templates", "", nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "revive_image") || !strings.Contains(listed.Body.String(), "new {{drama_title}}") {
		t.Fatalf("list after revive status=%d body=%s", listed.Code, listed.Body.String())
	}

	conflict := performRequest(router, http.MethodPost, "/api/v1/prompt-templates", `{
		"key":"revive_image","name":"Dup","category":"image","content":"dup"
	}`, nil)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("active key conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
}

func TestPromptTemplatePreviewSupportsGenerationVariables(t *testing.T) {
	_, router := testServerRouter(t)
	created := performRequest(router, http.MethodPost, "/api/v1/prompt-templates", `{
		"key":"shot_motion","name":"Shot Motion","category":"video",
		"content":"{{shot_title}} / {{shot_description}} / {{image_prompt}} / {{grid_rows}}x{{grid_cols}} / {{grid_mode}}"
	}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	id := jsonNumber(decodeResponse(t, created)["data"].(map[string]any)["id"].(float64))
	preview := performRequest(router, http.MethodPost, "/api/v1/prompt-templates/"+id+"/preview", `{"variables":{
		"shot_title":"Station","shot_description":"Walk","image_prompt":"Rain","grid_rows":"2","grid_cols":"3","grid_mode":"first_frame"
	}}`, nil)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), "Station / Walk / Rain / 2x3 / first_frame") {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
}

func TestPromptTemplateDraftPreviewDoesNotPersist(t *testing.T) {
	_, router := testServerRouter(t)
	var before int64
	if err := db.DB.Model(&models.PromptTemplate{}).Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	preview := performRequest(router, http.MethodPost, "/api/v1/prompt-templates/preview", `{
		"content":"为 {{drama_title}} 的 {{episode_title}} 生成画面",
		"variables":{"drama_title":"归途","episode_title":"重逢"}
	}`, nil)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), "为 归途 的 重逢 生成画面") || !strings.Contains(preview.Body.String(), `"variables":["drama_title","episode_title"]`) {
		t.Fatalf("draft preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	var after int64
	if err := db.DB.Model(&models.PromptTemplate{}).Count(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("draft preview persisted template: before=%d after=%d", before, after)
	}
}

func TestPromptTemplateDraftPreviewValidation(t *testing.T) {
	_, router := testServerRouter(t)
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"content":"plain","variables":{},"save":true}`},
		{name: "missing content", body: `{"variables":{}}`},
		{name: "unknown variable", body: `{"content":"{{secret}}","variables":{"secret":"value"}}`},
		{name: "missing variable value", body: `{"content":"{{drama_title}}","variables":{}}`},
		{name: "non-string variable", body: `{"content":"{{drama_title}}","variables":{"drama_title":1}}`},
		{name: "unused approved variable", body: `{"content":"{{drama_title}}","variables":{"drama_title":"Drama","episode_title":"unused"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := performRequest(router, http.MethodPost, "/api/v1/prompt-templates/preview", tc.body, nil)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	tooMany := make(map[string]string, maxPromptPreviewVariables+1)
	for index := 0; index <= maxPromptPreviewVariables; index++ {
		tooMany["variable_"+strconv.Itoa(index)] = "value"
	}
	encoded, err := json.Marshal(map[string]any{"content": "plain", "variables": tooMany})
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/prompt-templates/preview", string(encoded), nil)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "too many") {
		t.Fatalf("too many variables status=%d body=%s", response.Code, response.Body.String())
	}
	oversized := `{"content":"` + strings.Repeat("x", maxPromptPreviewBodyBytes) + `","variables":{}}`
	response = performRequest(router, http.MethodPost, "/api/v1/prompt-templates/preview", oversized, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized preview status=%d body=%s", response.Code, response.Body.String())
	}
}
