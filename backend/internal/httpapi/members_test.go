package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
)

func TestMemberManagementAndOrganizationSwitch(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	ownerACookie, ownerACSRF, organizationA := createTestActorSession(t, server, "owner-a@example.com", "members-a", "owner")
	ownerBCookie, ownerBCSRF, organizationB := createTestActorSession(t, server, "owner-b@example.com", "members-b", "owner")

	created := performAuthRequest(router, http.MethodPost, "/api/v1/organization/members", `{
		"email":"editor@example.com","display_name":"Editor","password":"editor secure password","role":"editor"
	}`, ownerACookie, ownerACSRF)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	userID := uint(decodeResponse(t, created)["data"].(map[string]any)["user_id"].(float64))
	invalidMemberID := performAuthRequest(router, http.MethodPut, "/api/v1/organization/members/bad", `{"role":"viewer"}`, ownerACookie, ownerACSRF)
	if invalidMemberID.Code != http.StatusBadRequest {
		t.Fatalf("invalid member id status=%d body=%s", invalidMemberID.Code, invalidMemberID.Body.String())
	}
	invalidMemberBody := performAuthRequest(router, http.MethodPut, "/api/v1/organization/members/"+strconv.Itoa(int(userID)), `{`, ownerACookie, ownerACSRF)
	if invalidMemberBody.Code != http.StatusBadRequest {
		t.Fatalf("invalid member body status=%d body=%s", invalidMemberBody.Code, invalidMemberBody.Body.String())
	}
	invalidRole := performAuthRequest(router, http.MethodPut, "/api/v1/organization/members/"+strconv.Itoa(int(userID)), `{"role":"owner"}`, ownerACookie, ownerACSRF)
	if invalidRole.Code != http.StatusBadRequest {
		t.Fatalf("invalid role status=%d body=%s", invalidRole.Code, invalidRole.Body.String())
	}
	missingMember := performAuthRequest(router, http.MethodPut, "/api/v1/organization/members/99999", `{"role":"viewer"}`, ownerACookie, ownerACSRF)
	if missingMember.Code != http.StatusNotFound {
		t.Fatalf("missing member status=%d body=%s", missingMember.Code, missingMember.Body.String())
	}
	updated := performAuthRequest(router, http.MethodPut, "/api/v1/organization/members/"+strconv.Itoa(int(userID)), `{"role":"viewer"}`, ownerACookie, ownerACSRF)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"role":"viewer"`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	listed := performAuthRequest(router, http.MethodGet, "/api/v1/organization/members", "", ownerACookie, "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "editor@example.com") || strings.Contains(listed.Body.String(), "password") {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}

	joinedB := performAuthRequest(router, http.MethodPost, "/api/v1/organization/members", `{"email":"editor@example.com","role":"viewer"}`, ownerBCookie, ownerBCSRF)
	if joinedB.Code != http.StatusConflict {
		t.Fatalf("existing user join status=%d body=%s", joinedB.Code, joinedB.Body.String())
	}
	if err := db.DB.Create(&models.Membership{OrganizationID: organizationB.ID, UserID: userID, Role: "viewer"}).Error; err != nil {
		t.Fatal(err)
	}
	login := performRequest(router, http.MethodPost, "/api/v1/auth/login", `{"email":"editor@example.com","password":"editor secure password"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	loginCookie, loginCSRF := responseCookie(t, login, "fly_session"), authCSRFToken(t, login)
	organizations := performAuthRequest(router, http.MethodGet, "/api/v1/auth/organizations", "", loginCookie, "")
	if organizations.Code != http.StatusOK || !strings.Contains(organizations.Body.String(), organizationA.Slug) || !strings.Contains(organizations.Body.String(), organizationB.Slug) {
		t.Fatalf("organizations status=%d body=%s", organizations.Code, organizations.Body.String())
	}
	invalidSwitch := performAuthRequest(router, http.MethodPost, "/api/v1/auth/switch-organization", `{"organization_id":99999}`, loginCookie, loginCSRF)
	if invalidSwitch.Code != http.StatusNotFound {
		t.Fatalf("invalid switch status=%d body=%s", invalidSwitch.Code, invalidSwitch.Body.String())
	}

	switched := performAuthRequest(router, http.MethodPost, "/api/v1/auth/switch-organization", `{"organization_id":`+strconv.Itoa(int(organizationB.ID))+`}`, loginCookie, loginCSRF)
	if switched.Code != http.StatusOK || !strings.Contains(switched.Body.String(), `"role":"viewer"`) {
		t.Fatalf("switch status=%d body=%s", switched.Code, switched.Body.String())
	}
	switchedCookie := responseCookie(t, switched, "fly_session")
	if switchedCookie == "" || switchedCookie == loginCookie {
		t.Fatal("session cookie was not rotated")
	}
	oldSession := performAuthRequest(router, http.MethodGet, "/api/v1/auth/me", "", loginCookie, "")
	if oldSession.Code != http.StatusUnauthorized {
		t.Fatalf("old session status=%d", oldSession.Code)
	}

	removed := performAuthRequest(router, http.MethodDelete, "/api/v1/organization/members/"+strconv.Itoa(int(userID)), "", ownerBCookie, ownerBCSRF)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove status=%d body=%s", removed.Code, removed.Body.String())
	}
	revoked := performAuthRequest(router, http.MethodGet, "/api/v1/auth/me", "", switchedCookie, "")
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("removed member session status=%d body=%s", revoked.Code, revoked.Body.String())
	}
}

func TestAdminCannotManageOwnerOrGrantAdmin(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	ownerCookie, ownerCSRF, organization := createTestActorSession(t, server, "protected-owner@example.com", "protected-owner", "owner")
	created := performAuthRequest(router, http.MethodPost, "/api/v1/organization/members", `{"email":"admin@example.com","password":"admin secure password","role":"admin"}`, ownerCookie, ownerCSRF)
	if created.Code != http.StatusCreated {
		t.Fatalf("create admin=%d body=%s", created.Code, created.Body.String())
	}
	adminLogin := performRequest(router, http.MethodPost, "/api/v1/auth/login", `{"email":"admin@example.com","password":"admin secure password"}`, nil)
	adminCookie, adminCSRF := responseCookie(t, adminLogin, "fly_session"), authCSRFToken(t, adminLogin)
	var ownerMembership models.Membership
	if err := db.DB.Where("organization_id = ? AND role = ?", organization.ID, "owner").First(&ownerMembership).Error; err != nil {
		t.Fatal(err)
	}
	removeOwner := performAuthRequest(router, http.MethodDelete, "/api/v1/organization/members/"+strconv.Itoa(int(ownerMembership.UserID)), "", adminCookie, adminCSRF)
	if removeOwner.Code != http.StatusBadRequest {
		t.Fatalf("remove owner status=%d body=%s", removeOwner.Code, removeOwner.Body.String())
	}
	grantAdmin := performAuthRequest(router, http.MethodPost, "/api/v1/organization/members", `{"email":"second-admin@example.com","password":"second admin password","role":"admin"}`, adminCookie, adminCSRF)
	if grantAdmin.Code != http.StatusBadRequest {
		t.Fatalf("grant admin status=%d body=%s", grantAdmin.Code, grantAdmin.Body.String())
	}
}
