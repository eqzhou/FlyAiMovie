package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
)

func TestOrganizationExportIsScopedAndRedactsCredentials(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	cookie, _, organization := createTestActorSession(t, server, "export-owner@example.com", "export-org", "owner")
	now := response.Now()
	drama := models.Drama{OrganizationID: organization.ID, Title: "exported drama", CreatedAt: now, UpdatedAt: now}
	foreign := models.Drama{OrganizationID: organization.ID + 999, Title: "foreign drama", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	var aiConfig models.AIServiceConfig
	if err := json.Unmarshal([]byte(`{"organization_id":1,"service_type":"image","provider":"openai","name":"private config","base_url":"https://api.example.test","api_key":"test-placeholder-key","is_active":true}`), &aiConfig); err != nil {
		t.Fatal(err)
	}
	aiConfig.OrganizationID = organization.ID
	aiConfig.CreatedAt, aiConfig.UpdatedAt = now, now
	if err := db.DB.Create(&aiConfig).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := server.Cache.PutValue(organization.ID, "ai_request", "export-cache", "text", "private cached output", time.Hour); err != nil {
		t.Fatal(err)
	}
	bundle := models.AIServiceBundle{
		OrganizationID: organization.ID, Key: "export-bundle", Name: "Export bundle",
		ServicesJSON: `[{"service_type":"text","provider":"openai","name":"Text","base_url":"https://api.example.test","model":"gpt-test","is_default":true,"is_active":true}]`,
		IsActive:     true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.DB.Create(&bundle).Error; err != nil {
		t.Fatal(err)
	}
	exported := performAuthRequest(router, http.MethodGet, "/api/v1/organization/export", "", cookie, "")
	if exported.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", exported.Code, exported.Body.String())
	}
	if got := exported.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control=%q", got)
	}
	if got := exported.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma=%q", got)
	}
	if got := exported.Header().Get("Content-Disposition"); got != `attachment; filename="flyaimovie-organization-export.json"` {
		t.Fatalf("Content-Disposition=%q", got)
	}
	body := exported.Body.String()
	if !strings.Contains(body, "exported drama") || strings.Contains(body, "foreign drama") {
		t.Fatalf("scope failure: %s", body)
	}
	for _, secret := range []string{"test-placeholder-key", "private cached output", "password_hash", "csrf_token", "token_hash"} {
		if strings.Contains(body, secret) {
			t.Fatalf("export leaked %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, `"api_key_set":true`) {
		t.Fatalf("credential presence marker missing: %s", body)
	}
	if !strings.Contains(body, `"media_cache_objects"`) || !strings.Contains(body, `"media_cache_references"`) {
		t.Fatalf("cache metadata missing from export: %s", body)
	}
	if !strings.Contains(body, `"prompt_templates"`) || !strings.Contains(body, `"prompt_template_revisions"`) {
		t.Fatalf("prompt history missing from export: %s", body)
	}
	if !strings.Contains(body, `"service_bundles"`) || !strings.Contains(body, `"skills"`) || !strings.Contains(body, `"skill_versions"`) || !strings.Contains(body, `"skill_publications"`) {
		t.Fatalf("service bundle or skill registry data missing from export: %s", body)
	}
	if !strings.Contains(body, `"export-bundle"`) || !strings.Contains(body, `"services":[{"service_type":"text"`) {
		t.Fatalf("service bundle payload missing from export: %s", body)
	}
}

func TestOrganizationDeletionPurgesDataMediaAndSessionsButKeepsSharedUser(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	cookie, csrf, organization := createTestActorSession(t, server, "delete-owner@example.com", "delete-org", "owner")
	var user models.User
	if err := db.DB.Where("email = ?", "delete-owner@example.com").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	other := models.Organization{Name: "kept", Slug: "kept-org", Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.Membership{OrganizationID: other.ID, UserID: user.ID, Role: "viewer", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	drama := models.Drama{OrganizationID: organization.ID, Title: "delete me", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	rel, abs, err := server.Store.Save("uploads", "delete.txt", strings.NewReader("private data"))
	if err != nil {
		t.Fatal(err)
	}
	asset := models.Asset{OrganizationID: organization.ID, DramaID: &drama.ID, Name: "private", Type: "document", URL: server.Store.PublicURL(rel), LocalPath: rel, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	cacheRel, cacheAbs, err := server.Store.Save("cache", "cache-only.bin", strings.NewReader("cache-only"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := server.Cache.Put(mediacache.PutInput{OrganizationID: organization.ID, Namespace: "test", Key: "cache-only", ContentHash: "cache-only-hash", Kind: "binary", LocalPath: cacheRel, PublicURL: server.Store.PublicURL(cacheRel), Size: 10}); err != nil {
		t.Fatal(err)
	}
	template := models.CharacterTemplate{OrganizationID: organization.ID, Name: "private template", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	job := models.GenerationJob{OrganizationID: organization.ID, Kind: "test", Status: "failed", TargetType: "test", TargetID: 99, Attempt: 1, MaxAttempts: 3, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.JobEvent{OrganizationID: organization.ID, JobID: job.ID, Stage: "failed", Message: "private event", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	agentRun := models.AgentRun{OrganizationID: organization.ID, AgentType: "extractor", DramaID: drama.ID, EpisodeID: 1, Status: "failed", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&agentRun).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.AgentRunEvent{OrganizationID: organization.ID, AgentRunID: agentRun.ID, Sequence: 1, EventType: "failed", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.MediaMigration{OrganizationID: organization.ID, TargetType: "asset", TargetID: asset.ID, SourceURL: "https://cdn.example/private", Status: "completed", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	bundle := models.AIServiceBundle{OrganizationID: organization.ID, Key: "private-bundle", Name: "Private bundle", ServicesJSON: `[]`, IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&bundle).Error; err != nil {
		t.Fatal(err)
	}
	skill := models.Skill{OrganizationID: organization.ID, AgentType: "extractor", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&skill).Error; err != nil {
		t.Fatal(err)
	}
	skillVersion := models.SkillVersion{OrganizationID: organization.ID, SkillID: skill.ID, Version: 1, MainMarkdown: "private skill", ReferencesJSON: `{}`, ContentSHA256: strings.Repeat("a", 64), CreatedByUserID: user.ID, CreatedAt: now}
	if err := db.DB.Create(&skillVersion).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.SkillPublication{OrganizationID: organization.ID, SkillID: skill.ID, VersionID: &skillVersion.ID, Action: "publish", CreatedByUserID: user.ID, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}

	wrong := performAuthRequest(router, http.MethodDelete, "/api/v1/organization", `{"password":"test actor password","confirmation":"wrong"}`, cookie, csrf)
	if wrong.Code != http.StatusBadRequest {
		t.Fatalf("wrong confirmation status=%d", wrong.Code)
	}
	deleted := performAuthRequest(router, http.MethodDelete, "/api/v1/organization", `{"password":"test actor password","confirmation":"delete-org"}`, cookie, csrf)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("media still exists: %v", err)
	}
	if _, err := os.Stat(cacheAbs); !os.IsNotExist(err) {
		t.Fatalf("cache-only media still exists: %v", err)
	}
	var deletionTask models.MediaDeletionTask
	if err := db.DB.Where("organization_id = ?", organization.ID).First(&deletionTask).Error; err != nil || deletionTask.Status != "completed" {
		t.Fatalf("media deletion task=%+v err=%v", deletionTask, err)
	}
	for model, name := range map[any]string{
		&models.Organization{}: "organization", &models.Drama{}: "drama", &models.Asset{}: "asset", &models.Membership{}: "membership", &models.Session{}: "session",
		&models.CharacterTemplate{}: "character template", &models.GenerationJob{}: "job", &models.JobEvent{}: "job event", &models.ProductionRun{}: "production run",
		&models.AgentRun{}: "agent run", &models.AgentRunEvent{}: "agent event", &models.MediaMigration{}: "media migration",
		&models.MediaCacheObject{}: "cache object", &models.MediaCacheReference{}: "cache reference",
		&models.PromptTemplate{}: "prompt template", &models.PromptTemplateRevision{}: "prompt template revision",
		&models.AIServiceBundle{}: "service bundle", &models.Skill{}: "skill", &models.SkillVersion{}: "skill version", &models.SkillPublication{}: "skill publication",
	} {
		var count int64
		query := db.DB.Model(model).Where("organization_id = ?", organization.ID)
		if name == "organization" {
			query = db.DB.Model(model).Where("id = ?", organization.ID)
		}
		if err := query.Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", name, count, err)
		}
	}
	if err := db.DB.First(&user, user.ID).Error; err != nil {
		t.Fatalf("shared user was deleted: %v", err)
	}
	var keptMembership models.Membership
	if err := db.DB.Where("organization_id = ? AND user_id = ?", other.ID, user.ID).First(&keptMembership).Error; err != nil {
		t.Fatalf("other membership removed: %v", err)
	}
	revoked := performAuthRequest(router, http.MethodGet, "/api/v1/auth/me", "", cookie, "")
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("deleted organization session status=%d", revoked.Code)
	}
}
