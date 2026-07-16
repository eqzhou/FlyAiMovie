package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
)

func TestAuditLogsAreTenantScopedAndDoNotCaptureBodies(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	cookieA, csrfA, organizationA := createTestActorSession(t, server, "audit-a@example.com", "audit-a", "owner")
	cookieB, csrfB, organizationB := createTestActorSession(t, server, "audit-b@example.com", "audit-b", "owner")

	secret := "sk-audit-must-never-be-logged"
	createdConfig := performAuthRequest(router, http.MethodPost, "/api/v1/ai-configs", `{
		"service_type":"image","provider":"openai","name":"private",
		"base_url":"https://api.example.test","api_key":"`+secret+`"
	}`, cookieA, csrfA)
	if createdConfig.Code != http.StatusCreated {
		t.Fatalf("config status=%d body=%s", createdConfig.Code, createdConfig.Body.String())
	}
	createdDrama := performAuthRequest(router, http.MethodPost, "/api/v1/dramas", `{"title":"tenant-b"}`, cookieB, csrfB)
	if createdDrama.Code != http.StatusCreated {
		t.Fatalf("drama status=%d body=%s", createdDrama.Code, createdDrama.Body.String())
	}

	var rows []models.AuditLog
	if err := db.DB.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("audit count=%d want 2", len(rows))
	}
	for _, row := range rows {
		serialized := row.Action + row.ResourceType + row.ResourceID + row.Method + row.Path + row.SourceIP
		if strings.Contains(serialized, secret) || strings.Contains(serialized, "api_key") {
			t.Fatalf("audit log captured secret material: %+v", row)
		}
	}
	if rows[0].OrganizationID != organizationA.ID || rows[1].OrganizationID != organizationB.ID {
		t.Fatalf("wrong organizations: %+v", rows)
	}

	listA := performAuthRequest(router, http.MethodGet, "/api/v1/audit-logs", "", cookieA, "")
	if listA.Code != http.StatusOK || !strings.Contains(listA.Body.String(), `"resource_type":"ai-configs"`) || strings.Contains(listA.Body.String(), `"resource_type":"dramas"`) {
		t.Fatalf("tenant A audit response status=%d body=%s", listA.Code, listA.Body.String())
	}
}

func TestAuditLogsRequireAdminRole(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	cookie, _, _ := createTestActorSession(t, server, "audit-viewer@example.com", "audit-viewer", "viewer")
	response := performAuthRequest(router, http.MethodGet, "/api/v1/audit-logs", "", cookie, "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
