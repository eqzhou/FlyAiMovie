package httpapi

import (
	"net/http"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
)

func TestUpsertAgentConfigPersistsEditablePromptFields(t *testing.T) {
	_, router := testServerRouter(t)
	existing := models.AgentConfig{
		AgentType:    "script_rewriter",
		Name:         "旧名称",
		Description:  "旧说明",
		SystemPrompt: "旧提示词",
		IsActive:     true,
	}
	if err := db.DB.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}

	response := performRequest(router, http.MethodPost, "/api/v1/agent-configs", `{
		"agent_type":"script_rewriter",
		"name":"剧本改写",
		"description":"用于保持人物和叙事一致性",
		"system_prompt":"遵守既有世界观并输出结构化剧本",
		"model":"text-model",
		"temperature":0.3,
		"max_tokens":4096,
		"max_iterations":3,
		"is_active":true
	}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	var stored models.AgentConfig
	if err := db.DB.First(&stored, existing.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Description != "用于保持人物和叙事一致性" {
		t.Fatalf("description=%q", stored.Description)
	}
	if stored.SystemPrompt != "遵守既有世界观并输出结构化剧本" {
		t.Fatalf("system_prompt=%q", stored.SystemPrompt)
	}
}
