package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
)

type captureResetSender struct {
	email   string
	token   string
	expires time.Time
}

func (s *captureResetSender) SendPasswordReset(email, token string, expiresAt time.Time) error {
	s.email, s.token, s.expires = email, token, expiresAt
	return nil
}

func TestPasswordResetRequestDoesNotEnumerateAccounts(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	known := performRequest(router, http.MethodPost, "/api/v1/auth/password-reset/request", `{"email":"missing@example.com"}`, nil)
	unknown := performRequest(router, http.MethodPost, "/api/v1/auth/password-reset/request", `{"email":"not-an-email"}`, nil)
	if known.Code != http.StatusOK || unknown.Code != http.StatusOK || known.Body.String() != unknown.Body.String() {
		t.Fatalf("responses differ: known=%d %s unknown=%d %s", known.Code, known.Body.String(), unknown.Code, unknown.Body.String())
	}
}

func TestPasswordResetConsumesTokenAndRevokesSessions(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	sender := &captureResetSender{}
	server.ResetSender = sender
	router := server.Router()
	cookie, _, _ := createTestActorSession(t, server, "reset@example.com", "reset-org", "owner")
	request := performRequest(router, http.MethodPost, "/api/v1/auth/password-reset/request", `{"email":"reset@example.com"}`, nil)
	if request.Code != http.StatusOK || sender.token == "" || sender.email != "reset@example.com" {
		t.Fatalf("request status=%d body=%s sender=%+v", request.Code, request.Body.String(), sender)
	}
	consume := performRequest(router, http.MethodPost, "/api/v1/auth/password-reset/consume", `{"token":"`+sender.token+`","new_password":"new reset password"}`, nil)
	if consume.Code != http.StatusOK {
		t.Fatalf("consume status=%d body=%s", consume.Code, consume.Body.String())
	}
	revoked := performAuthRequest(router, http.MethodGet, "/api/v1/auth/me", "", cookie, "")
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("old session status=%d body=%s", revoked.Code, revoked.Body.String())
	}
	oldLogin := performRequest(router, http.MethodPost, "/api/v1/auth/login", `{"email":"reset@example.com","password":"test actor password"}`, nil)
	if oldLogin.Code != http.StatusUnauthorized {
		t.Fatalf("old password status=%d", oldLogin.Code)
	}
	newLogin := performRequest(router, http.MethodPost, "/api/v1/auth/login", `{"email":"reset@example.com","password":"new reset password"}`, nil)
	if newLogin.Code != http.StatusOK {
		t.Fatalf("new password status=%d body=%s", newLogin.Code, newLogin.Body.String())
	}
	replay := performRequest(router, http.MethodPost, "/api/v1/auth/password-reset/consume", `{"token":"`+sender.token+`","new_password":"another reset password"}`, nil)
	if replay.Code != http.StatusBadRequest {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	var row models.PasswordResetToken
	if err := db.DB.Where("token_hash = ?", tokenHash(sender.token)).First(&row).Error; err != nil || row.ConsumedAt == nil {
		t.Fatalf("consumed token not persisted: err=%v row=%+v", err, row)
	}
}

func TestPasswordResetExpiredTokenRejected(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	_, _, organization := createTestActorSession(t, server, "expired-reset@example.com", "expired-reset", "owner")
	var user models.User
	if err := db.DB.Where("email = ?", "expired-reset@example.com").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	_ = organization
	now := time.Now().UTC()
	row := models.PasswordResetToken{UserID: user.ID, TokenHash: tokenHash("expired-reset-token"), ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339), CreatedAt: now.Add(-time.Hour).Format(time.RFC3339)}
	if err := db.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/auth/password-reset/consume", `{"token":"expired-reset-token","new_password":"new reset password"}`, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expired status=%d body=%s", response.Code, response.Body.String())
	}
}
