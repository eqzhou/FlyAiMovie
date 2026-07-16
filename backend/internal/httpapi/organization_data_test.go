package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
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
	exported := performAuthRequest(router, http.MethodGet, "/api/v1/organization/export", "", cookie, "")
	if exported.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", exported.Code, exported.Body.String())
	}
	body := exported.Body.String()
	if !strings.Contains(body, "exported drama") || strings.Contains(body, "foreign drama") {
		t.Fatalf("scope failure: %s", body)
	}
	for _, secret := range []string{"test-placeholder-key", "password_hash", "csrf_token", "token_hash"} {
		if strings.Contains(body, secret) {
			t.Fatalf("export leaked %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, `"api_key_set":true`) {
		t.Fatalf("credential presence marker missing: %s", body)
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
	for model, name := range map[any]string{&models.Organization{}: "organization", &models.Drama{}: "drama", &models.Asset{}: "asset", &models.Membership{}: "membership", &models.Session{}: "session"} {
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
