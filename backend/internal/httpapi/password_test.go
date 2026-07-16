package httpapi

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/config"
)

func TestChangePasswordRotatesSessionsAndInvalidatesOldPassword(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	cookie, csrf, _ := createTestActorSession(t, server, "change-password@example.com", "change-password", "owner")
	wrong := performAuthRequest(router, http.MethodPost, "/api/v1/auth/change-password", `{"current_password":"wrong password","new_password":"new secure password value"}`, cookie, csrf)
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong status=%d body=%s", wrong.Code, wrong.Body.String())
	}
	changed := performAuthRequest(router, http.MethodPost, "/api/v1/auth/change-password", `{"current_password":"test actor password","new_password":"new secure password value"}`, cookie, csrf)
	if changed.Code != http.StatusOK {
		t.Fatalf("change status=%d body=%s", changed.Code, changed.Body.String())
	}
	newCookie := responseCookie(t, changed, "fly_session")
	if newCookie == "" || newCookie == cookie {
		t.Fatal("session was not rotated")
	}
	oldSession := performAuthRequest(router, http.MethodGet, "/api/v1/auth/me", "", cookie, "")
	if oldSession.Code != http.StatusUnauthorized {
		t.Fatalf("old session status=%d", oldSession.Code)
	}
	oldLogin := performRequest(router, http.MethodPost, "/api/v1/auth/login", `{"email":"change-password@example.com","password":"test actor password"}`, nil)
	if oldLogin.Code != http.StatusUnauthorized {
		t.Fatalf("old password status=%d", oldLogin.Code)
	}
	newLogin := performRequest(router, http.MethodPost, "/api/v1/auth/login", `{"email":"change-password@example.com","password":"new secure password value"}`, nil)
	if newLogin.Code != http.StatusOK {
		t.Fatalf("new password status=%d body=%s", newLogin.Code, newLogin.Body.String())
	}
}

func TestOrganizationAdminCannotResetGlobalMemberPassword(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	ownerCookie, ownerCSRF, _ := createTestActorSession(t, server, "reset-owner@example.com", "reset-owner", "owner")
	created := performAuthRequest(router, http.MethodPost, "/api/v1/organization/members", `{"email":"reset-member@example.com","password":"member old password","role":"editor"}`, ownerCookie, ownerCSRF)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	memberID := uint(decodeResponse(t, created)["data"].(map[string]any)["user_id"].(float64))
	memberLogin := performRequest(router, http.MethodPost, "/api/v1/auth/login", `{"email":"reset-member@example.com","password":"member old password"}`, nil)
	memberCookie := responseCookie(t, memberLogin, "fly_session")
	reset := performAuthRequest(router, http.MethodPut, "/api/v1/organization/members/"+strconv.Itoa(int(memberID))+"/password", `{"password":"member new password"}`, ownerCookie, ownerCSRF)
	if reset.Code != http.StatusForbidden {
		t.Fatalf("reset status=%d body=%s", reset.Code, reset.Body.String())
	}
	active := performAuthRequest(router, http.MethodGet, "/api/v1/auth/me", "", memberCookie, "")
	if active.Code != http.StatusOK {
		t.Fatalf("member session status=%d", active.Code)
	}
	oldLogin := performRequest(router, http.MethodPost, "/api/v1/auth/login", `{"email":"reset-member@example.com","password":"member old password"}`, nil)
	if oldLogin.Code != http.StatusOK {
		t.Fatalf("old password status=%d", oldLogin.Code)
	}
	newLogin := performRequest(router, http.MethodPost, "/api/v1/auth/login", `{"email":"reset-member@example.com","password":"member new password"}`, nil)
	if newLogin.Code != http.StatusUnauthorized {
		t.Fatalf("new password status=%d body=%s", newLogin.Code, newLogin.Body.String())
	}
}
