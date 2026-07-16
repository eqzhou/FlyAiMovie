package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
)

func TestOrganizationInvitationAcceptsNewUserOnce(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	ownerCookie, ownerCSRF, organization := createTestActorSession(t, server, "invite-owner@example.com", "invite-owner", "owner")
	created := performAuthRequest(router, http.MethodPost, "/api/v1/organization/members/invitations", `{"email":"invite-new@example.com","role":"editor","ttl_hours":24}`, ownerCookie, ownerCSRF)
	if created.Code != http.StatusCreated {
		t.Fatalf("create invitation status=%d body=%s", created.Code, created.Body.String())
	}
	token, ok := decodeResponse(t, created)["data"].(map[string]any)["token"].(string)
	if !ok || token == "" {
		t.Fatalf("missing invitation token: %s", created.Body.String())
	}
	preview := performRequest(router, http.MethodGet, "/api/v1/auth/invitations/"+token, "", nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	accepted := performRequest(router, http.MethodPost, "/api/v1/auth/invitations/"+token+"/accept", `{"email":"invite-new@example.com","display_name":"Invited","new_password":"new invited password"}`, nil)
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	var user models.User
	if err := db.DB.Where("email = ?", "invite-new@example.com").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	var membership models.Membership
	if err := db.DB.Where("organization_id = ? AND user_id = ? AND role = ?", organization.ID, user.ID, "editor").First(&membership).Error; err != nil {
		t.Fatalf("membership missing: %v", err)
	}
	replay := performRequest(router, http.MethodPost, "/api/v1/auth/invitations/"+token+"/accept", `{"email":"invite-new@example.com","new_password":"another password"}`, nil)
	if replay.Code != http.StatusBadRequest {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
}

func TestOrganizationInvitationRequiresExistingUserPassword(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	ownerCookie, ownerCSRF, _ := createTestActorSession(t, server, "invite-owner2@example.com", "invite-owner2", "owner")
	_, _, _ = createTestActorSession(t, server, "invite-existing@example.com", "invite-existing", "owner")
	created := performAuthRequest(router, http.MethodPost, "/api/v1/organization/members/invitations", `{"email":"invite-existing@example.com","role":"viewer"}`, ownerCookie, ownerCSRF)
	if created.Code != http.StatusCreated {
		t.Fatalf("create invitation status=%d body=%s", created.Code, created.Body.String())
	}
	token := decodeResponse(t, created)["data"].(map[string]any)["token"].(string)
	wrong := performRequest(router, http.MethodPost, "/api/v1/auth/invitations/"+token+"/accept", `{"email":"invite-existing@example.com","current_password":"wrong password"}`, nil)
	if wrong.Code != http.StatusBadRequest {
		t.Fatalf("wrong password status=%d body=%s", wrong.Code, wrong.Body.String())
	}
	okResponse := performRequest(router, http.MethodPost, "/api/v1/auth/invitations/"+token+"/accept", `{"email":"invite-existing@example.com","current_password":"test actor password"}`, nil)
	if okResponse.Code != http.StatusOK {
		t.Fatalf("valid password status=%d body=%s", okResponse.Code, okResponse.Body.String())
	}
}

func TestOrganizationInvitationExpires(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	ownerCookie, ownerCSRF, organization := createTestActorSession(t, server, "invite-owner3@example.com", "invite-owner3", "owner")
	now := time.Now().UTC()
	invitation := models.OrganizationInvitation{OrganizationID: organization.ID, InvitedBy: 1, Email: "expired@example.com", Role: "viewer", TokenHash: tokenHash("expired-token"), ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339), CreatedAt: now.Add(-2 * time.Hour).Format(time.RFC3339)}
	if err := db.DB.Create(&invitation).Error; err != nil {
		t.Fatal(err)
	}
	_ = ownerCookie
	_ = ownerCSRF
	response := performRequest(router, http.MethodGet, "/api/v1/auth/invitations/expired-token", "", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expired status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestOrganizationInvitationCanBeRevokedAndResent(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	ownerCookie, ownerCSRF, _ := createTestActorSession(t, server, "invite-owner4@example.com", "invite-owner4", "owner")
	created := performAuthRequest(router, http.MethodPost, "/api/v1/organization/members/invitations", `{"email":"revoke@example.com","role":"editor"}`, ownerCookie, ownerCSRF)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	data := decodeResponse(t, created)["data"].(map[string]any)
	id := uint(data["id"].(float64))
	token := data["token"].(string)
	revoked := performAuthRequest(router, http.MethodDelete, "/api/v1/organization/members/invitations/"+itoa(id), "", ownerCookie, ownerCSRF)
	if revoked.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", revoked.Code, revoked.Body.String())
	}
	preview := performRequest(router, http.MethodGet, "/api/v1/auth/invitations/"+token, "", nil)
	if preview.Code != http.StatusNotFound {
		t.Fatalf("revoked preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	resend := performAuthRequest(router, http.MethodPost, "/api/v1/organization/members/invitations/"+itoa(id)+"/resend", "", ownerCookie, ownerCSRF)
	if resend.Code != http.StatusNotFound {
		t.Fatalf("revoked resend status=%d body=%s", resend.Code, resend.Body.String())
	}
	created2 := performAuthRequest(router, http.MethodPost, "/api/v1/organization/members/invitations", `{"email":"resend@example.com","role":"editor","ttl_hours":1}`, ownerCookie, ownerCSRF)
	data2 := decodeResponse(t, created2)["data"].(map[string]any)
	id2 := uint(data2["id"].(float64))
	token2 := data2["token"].(string)
	resend2 := performAuthRequest(router, http.MethodPost, "/api/v1/organization/members/invitations/"+itoa(id2)+"/resend", "", ownerCookie, ownerCSRF)
	if resend2.Code != http.StatusCreated {
		t.Fatalf("resend status=%d body=%s", resend2.Code, resend2.Body.String())
	}
	if performRequest(router, http.MethodGet, "/api/v1/auth/invitations/"+token2, "", nil).Code != http.StatusNotFound {
		t.Fatal("old invitation token remained valid after resend")
	}
}
