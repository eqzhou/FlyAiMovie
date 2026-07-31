package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"golang.org/x/crypto/bcrypt"
)

func TestSkillRegistryHTTPWorkflowAndRBAC(t *testing.T) {
	server, _ := testServerRouter(t)
	if err := db.DB.AutoMigrate(&models.Skill{}, &models.SkillVersion{}, &models.SkillPublication{}); err != nil {
		t.Fatal(err)
	}
	server.Cfg.Auth.Enabled = true
	server.Cfg.Auth.CookieName = "fly_session"
	server.Cfg.Auth.SessionTTLHours = 24
	ownerCookie, ownerCSRF, _ := createTestActorSession(t, server, "skill-owner@example.com", "skill-owner", "owner")
	editorCookie, editorCSRF, _ := createTestActorSession(t, server, "skill-editor@example.com", "skill-editor", "editor")
	router := server.Router()

	blocked := performAuthRequest(router, http.MethodPost, "/api/v1/skills/extractor/versions", `{"main_markdown":"editor content"}`, editorCookie, editorCSRF)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("editor status=%d body=%s", blocked.Code, blocked.Body.String())
	}
	created := performAuthRequest(router, http.MethodPost, "/api/v1/skills/extractor/versions", `{"main_markdown":"owner content","references":{"references/rules.md":"rules"}}`, ownerCookie, ownerCSRF)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	versionID := jsonNumber(decodeResponse(t, created)["data"].(map[string]any)["id"].(float64))
	published := performAuthRequest(router, http.MethodPost, "/api/v1/skills/extractor/versions/"+versionID+"/publish", `{}`, ownerCookie, ownerCSRF)
	if published.Code != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", published.Code, published.Body.String())
	}
	detail := performAuthRequest(router, http.MethodGet, "/api/v1/skills/extractor", "", ownerCookie, "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "owner content") || !strings.Contains(detail.Body.String(), `"action":"publish"`) {
		t.Fatalf("detail=%d %s", detail.Code, detail.Body.String())
	}
	archived := performAuthRequest(router, http.MethodPost, "/api/v1/skills/extractor/archive", `{}`, ownerCookie, ownerCSRF)
	if archived.Code != http.StatusOK {
		t.Fatalf("archive=%d %s", archived.Code, archived.Body.String())
	}
	restored := performAuthRequest(router, http.MethodPost, "/api/v1/skills/extractor/versions/"+versionID+"/publish", `{}`, ownerCookie, ownerCSRF)
	if restored.Code != http.StatusOK || strings.Contains(restored.Body.String(), `"archived_at"`) {
		t.Fatalf("restore archived=%d %s", restored.Code, restored.Body.String())
	}
}

func TestSkillRegistryLocalWorkspaceWorkflow(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth.Enabled = false
	router := server.Router()

	created := performRequest(router, http.MethodPost, "/api/v1/skills/extractor/versions", `{"main_markdown":"local content"}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create local status=%d body=%s", created.Code, created.Body.String())
	}
	versionID := jsonNumber(decodeResponse(t, created)["data"].(map[string]any)["id"].(float64))
	published := performRequest(router, http.MethodPost, "/api/v1/skills/extractor/versions/"+versionID+"/publish", `{}`, nil)
	if published.Code != http.StatusOK {
		t.Fatalf("publish local status=%d body=%s", published.Code, published.Body.String())
	}
	detail := performRequest(router, http.MethodGet, "/api/v1/skills/extractor", "", nil)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "local content") {
		t.Fatalf("local detail=%d %s", detail.Code, detail.Body.String())
	}
	listed := performRequest(router, http.MethodGet, "/api/v1/skills", "", nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"source":"database"`) {
		t.Fatalf("local list=%d %s", listed.Code, listed.Body.String())
	}
	archived := performRequest(router, http.MethodPost, "/api/v1/skills/extractor/archive", `{}`, nil)
	if archived.Code != http.StatusOK {
		t.Fatalf("archive local=%d %s", archived.Code, archived.Body.String())
	}
	restored := performRequest(router, http.MethodPost, "/api/v1/skills/extractor/versions/"+versionID+"/publish", `{}`, nil)
	if restored.Code != http.StatusOK || strings.Contains(restored.Body.String(), `"archived_at"`) {
		t.Fatalf("restore local=%d %s", restored.Code, restored.Body.String())
	}
}

func TestSkillRegistryDetailSanitizesNonAdminView(t *testing.T) {
	server, _ := testServerRouter(t)
	if err := db.DB.AutoMigrate(&models.Skill{}, &models.SkillVersion{}, &models.SkillPublication{}); err != nil {
		t.Fatal(err)
	}
	server.Cfg.Auth.Enabled = true
	server.Cfg.Auth.CookieName = "fly_session"
	server.Cfg.Auth.SessionTTLHours = 24
	ownerCookie, ownerCSRF, organization := createTestActorSession(t, server, "skill-view-owner@example.com", "skill-view", "owner")
	viewerCookie := createSkillRegistryActorInOrganization(t, server, organization.ID, "skill-viewer@example.com", "viewer")
	editorCookie := createSkillRegistryActorInOrganization(t, server, organization.ID, "skill-editor-same-org@example.com", "editor")
	router := server.Router()

	draft := performAuthRequest(router, http.MethodPost, "/api/v1/skills/extractor/versions", `{"main_markdown":"published body"}`, ownerCookie, ownerCSRF)
	publishedID := jsonNumber(decodeResponse(t, draft)["data"].(map[string]any)["id"].(float64))
	if response := performAuthRequest(router, http.MethodPost, "/api/v1/skills/extractor/versions/"+publishedID+"/publish", `{}`, ownerCookie, ownerCSRF); response.Code != http.StatusOK {
		t.Fatalf("publish=%d %s", response.Code, response.Body.String())
	}
	if response := performAuthRequest(router, http.MethodPost, "/api/v1/skills/extractor/versions", `{"main_markdown":"secret draft"}`, ownerCookie, ownerCSRF); response.Code != http.StatusCreated {
		t.Fatalf("draft=%d %s", response.Code, response.Body.String())
	}

	for role, cookie := range map[string]string{"viewer": viewerCookie, "editor": editorCookie} {
		t.Run(role, func(t *testing.T) {
			response := performAuthRequest(router, http.MethodGet, "/api/v1/skills/extractor", "", cookie, "")
			body := response.Body.String()
			if response.Code != http.StatusOK || !strings.Contains(body, "published body") {
				t.Fatalf("detail=%d %s", response.Code, body)
			}
			for _, forbidden := range []string{"secret draft", `"versions"`, `"publications"`, `"created_by_user_id"`} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("non-admin detail leaked %q: %s", forbidden, body)
				}
			}
		})
	}
}

func createSkillRegistryActorInOrganization(t *testing.T, server *Server, organizationID uint, email, role string) string {
	t.Helper()
	now := response.Now()
	hash, err := bcrypt.GenerateFromPassword([]byte("skill registry actor password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{Email: email, PasswordHash: string(hash), DisplayName: email, Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.Membership{OrganizationID: organizationID, UserID: user.ID, Role: role, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := server.createSession(user.ID, organizationID)
	if err != nil {
		t.Fatal(err)
	}
	return server.Cfg.Auth.CookieName + "=" + token
}

func TestSkillRegistryHTTPValidationAndCrossOrganization(t *testing.T) {
	server, _ := testServerRouter(t)
	if err := db.DB.AutoMigrate(&models.Skill{}, &models.SkillVersion{}, &models.SkillPublication{}); err != nil {
		t.Fatal(err)
	}
	server.Cfg.Auth.Enabled = true
	server.Cfg.Auth.CookieName = "fly_session"
	server.Cfg.Auth.SessionTTLHours = 24
	cookieA, csrfA, _ := createTestActorSession(t, server, "skill-a@example.com", "skill-a", "owner")
	cookieB, csrfB, _ := createTestActorSession(t, server, "skill-b@example.com", "skill-b", "owner")
	router := server.Router()
	bad := performAuthRequest(router, http.MethodPost, "/api/v1/skills/not_agent/versions", `{"main_markdown":"x"}`, cookieA, csrfA)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad agent=%d %s", bad.Code, bad.Body.String())
	}
	created := performAuthRequest(router, http.MethodPost, "/api/v1/skills/extractor/versions", `{"main_markdown":"org a"}`, cookieA, csrfA)
	versionID := jsonNumber(decodeResponse(t, created)["data"].(map[string]any)["id"].(float64))
	cross := performAuthRequest(router, http.MethodPost, "/api/v1/skills/extractor/versions/"+versionID+"/publish", `{}`, cookieB, csrfB)
	if cross.Code != http.StatusNotFound {
		t.Fatalf("cross org=%d %s", cross.Code, cross.Body.String())
	}
}
