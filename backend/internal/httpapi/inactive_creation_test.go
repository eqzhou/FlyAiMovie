package httpapi

import (
	"net/http"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
)

// GORM omits a zero-value field from the INSERT when the field carries a
// `default` tag, so a bool column tagged `default:true` silently stores true
// even when the caller explicitly asked for false. These endpoints all accept
// is_active, so a template or service created as inactive must stay inactive.
func TestCreatingInactiveResourcesPersistsFalse(t *testing.T) {
	server, router := testServerRouter(t)
	_ = server

	template := performRequest(router, http.MethodPost, "/api/v1/prompt-templates", `{
		"key":"inactive_template","name":"停用模板","category":"image",
		"description":"probe","content":"为 {{drama_title}} 生成图片","is_active":false
	}`, nil)
	if template.Code != http.StatusCreated {
		t.Fatalf("prompt template status=%d body=%s", template.Code, template.Body.String())
	}
	var storedTemplate models.PromptTemplate
	if err := db.DB.Where("key = ?", "inactive_template").First(&storedTemplate).Error; err != nil {
		t.Fatal(err)
	}
	if storedTemplate.IsActive {
		t.Fatal("prompt template created with is_active=false was stored as active")
	}

	// The revision written alongside the template must mirror the same state,
	// otherwise restoring that version would silently reactivate it.
	var revision models.PromptTemplateRevision
	if err := db.DB.Where("prompt_template_id = ?", storedTemplate.ID).Order("version desc").First(&revision).Error; err != nil {
		t.Fatal(err)
	}
	if revision.IsActive {
		t.Fatal("prompt template revision recorded the template as active")
	}

	config := performRequest(router, http.MethodPost, "/api/v1/ai-configs", `{
		"service_type":"image","provider":"mock","name":"inactive-service",
		"base_url":"http://localhost","api_key":"mock","model":"mock","is_active":false
	}`, nil)
	if config.Code != http.StatusCreated {
		t.Fatalf("ai config status=%d body=%s", config.Code, config.Body.String())
	}
	var storedConfig models.AIServiceConfig
	if err := db.DB.Where("name = ?", "inactive-service").First(&storedConfig).Error; err != nil {
		t.Fatal(err)
	}
	if storedConfig.IsActive {
		t.Fatal("AI service config created with is_active=false was stored as active")
	}

	agent := performRequest(router, http.MethodPost, "/api/v1/agent-configs", `{
		"agent_type":"script_rewriter","name":"停用改写","model":"mock","is_active":false
	}`, nil)
	if agent.Code != http.StatusOK && agent.Code != http.StatusCreated {
		t.Fatalf("agent config status=%d body=%s", agent.Code, agent.Body.String())
	}
	var storedAgent models.AgentConfig
	if err := db.DB.Where("agent_type = ?", "script_rewriter").First(&storedAgent).Error; err != nil {
		t.Fatal(err)
	}
	if storedAgent.IsActive {
		t.Fatal("agent config saved with is_active=false was stored as active")
	}
}

// Creating without is_active must still default to active, so the fix for the
// false case cannot regress the common path.
func TestCreatingResourcesWithoutIsActiveDefaultsToActive(t *testing.T) {
	_, router := testServerRouter(t)

	template := performRequest(router, http.MethodPost, "/api/v1/prompt-templates", `{
		"key":"default_template","name":"默认模板","category":"image",
		"description":"probe","content":"为 {{drama_title}} 生成图片"
	}`, nil)
	if template.Code != http.StatusCreated {
		t.Fatalf("prompt template status=%d body=%s", template.Code, template.Body.String())
	}
	var storedTemplate models.PromptTemplate
	if err := db.DB.Where("key = ?", "default_template").First(&storedTemplate).Error; err != nil {
		t.Fatal(err)
	}
	if !storedTemplate.IsActive {
		t.Fatal("prompt template created without is_active should default to active")
	}

	config := performRequest(router, http.MethodPost, "/api/v1/ai-configs", `{
		"service_type":"image","provider":"mock","name":"default-service",
		"base_url":"http://localhost","api_key":"mock","model":"mock"
	}`, nil)
	if config.Code != http.StatusCreated {
		t.Fatalf("ai config status=%d body=%s", config.Code, config.Body.String())
	}
	var storedConfig models.AIServiceConfig
	if err := db.DB.Where("name = ?", "default-service").First(&storedConfig).Error; err != nil {
		t.Fatal(err)
	}
	if !storedConfig.IsActive {
		t.Fatal("AI service config created without is_active should default to active")
	}
}
