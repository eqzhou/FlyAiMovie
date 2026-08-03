package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"gorm.io/gorm"
)

const testAuthRequestBodyLimit = 64 << 10

func TestAuthRejectsEmptyStoredCSRFToken(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	cookie, _, _ := createTestActorSession(t, server, "empty-csrf@example.com", "empty-csrf", "owner")
	if err := db.DB.Model(&models.Session{}).Where("csrf_token <> ?", "").Update("csrf_token", "").Error; err != nil {
		t.Fatal(err)
	}

	result := performAuthRequest(server.Router(), http.MethodPost, "/api/v1/dramas", `{"title":"must not be created"}`, cookie, "")
	if result.Code != http.StatusForbidden {
		t.Fatalf("empty stored csrf status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestAuthRejectsUnsafeRequestFromUntrustedOrigin(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	createTestActorSession(t, server, "origin-login@example.com", "origin-login", "owner")

	result := performRequest(server.Router(), http.MethodPost, "/api/v1/auth/login", `{"email":"origin-login@example.com","password":"test actor password"}`, map[string]string{
		"Content-Type": "application/json",
		"Origin":       "https://attacker.example",
	})
	if result.Code != http.StatusForbidden {
		t.Fatalf("untrusted cross-origin login status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestAuthWildcardCORSDoesNotPermitCredentialSettingRequests(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Server.CORSOrigins = []string{"*"}
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	createTestActorSession(t, server, "wildcard-login@example.com", "wildcard-login", "owner")

	result := performRequest(server.Router(), http.MethodPost, "/api/v1/auth/login", `{"email":"wildcard-login@example.com","password":"test actor password"}`, map[string]string{
		"Content-Type": "application/json",
		"Origin":       "https://attacker.example",
	})
	if result.Code != http.StatusForbidden {
		t.Fatalf("wildcard cross-origin login status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestAuthRejectsOversizedRequestBody(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	body := `{"email":"` + strings.Repeat("a", testAuthRequestBodyLimit) + `","password":"test actor password"}`

	result := performRequest(server.Router(), http.MethodPost, "/api/v1/auth/login", body, map[string]string{"Content-Type": "application/json"})
	if result.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized auth body status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestAuthRejectsPasswordBeyondBcryptLimitAsBadRequest(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	body := `{"organization_name":"Studio","email":"owner@example.com","password":"` + strings.Repeat("p", 73) + `"}`

	result := performRequest(server.Router(), http.MethodPost, "/api/v1/auth/setup", body, map[string]string{"Content-Type": "application/json"})
	if result.Code != http.StatusBadRequest || strings.Contains(strings.ToLower(result.Body.String()), "bcrypt") {
		t.Fatalf("overlong password status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestPasswordResetDoesNotExposePersistenceErrors(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	createTestActorSession(t, server, "reset-leak@example.com", "reset-leak", "owner")
	var user models.User
	if err := db.DB.Where("email = ?", "reset-leak@example.com").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	row := models.PasswordResetToken{
		UserID: user.ID, TokenHash: tokenHash("valid-reset-token"),
		ExpiresAt: now.Add(time.Hour).Format(time.RFC3339), CreatedAt: now.Format(time.RFC3339),
	}
	if err := db.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}

	const callbackName = "test:password-reset-persistence-error"
	secretDetail := "forced persistence detail /var/private/database.sqlite"
	if err := db.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "password_reset_tokens" {
			tx.AddError(errors.New(secretDetail))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DB.Callback().Update().Remove(callbackName) })

	result := performRequest(server.Router(), http.MethodPost, "/api/v1/auth/password-reset/consume", `{"token":"valid-reset-token","new_password":"new secure password"}`, map[string]string{"Content-Type": "application/json"})
	if result.Code != http.StatusInternalServerError || strings.Contains(result.Body.String(), secretDetail) {
		t.Fatalf("reset persistence error status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestOrganizationSwitchRollsBackWhenOldSessionCannotBeRevoked(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	cookie, csrf, _ := createTestActorSession(t, server, "switch-atomic@example.com", "switch-source", "owner")
	var user models.User
	if err := db.DB.Where("email = ?", "switch-atomic@example.com").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	target := models.Organization{Name: "switch-target", Slug: "switch-target", Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.Membership{OrganizationID: target.ID, UserID: user.ID, Role: "editor", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}

	const callbackName = "test:switch-session-revocation-error"
	detail := "forced session revocation detail"
	if err := db.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "sessions" {
			tx.AddError(errors.New(detail))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DB.Callback().Update().Remove(callbackName) })

	result := performAuthRequest(server.Router(), http.MethodPost, "/api/v1/auth/switch-organization", `{"organization_id":`+itoa(target.ID)+`}`, cookie, csrf)
	if result.Code != http.StatusInternalServerError || strings.Contains(result.Body.String(), detail) {
		t.Fatalf("switch failure status=%d body=%s", result.Code, result.Body.String())
	}
	if len(result.Result().Cookies()) != 0 {
		t.Fatalf("failed switch set cookies: %#v", result.Result().Cookies())
	}
	var sessions int64
	if err := db.DB.Model(&models.Session{}).Where("user_id = ?", user.ID).Count(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatalf("session rotation was not rolled back: count=%d", sessions)
	}
}

func TestLogoutDoesNotReportSuccessWhenRevocationFails(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	cookie, csrf, _ := createTestActorSession(t, server, "logout-failure@example.com", "logout-failure", "owner")

	const callbackName = "test:logout-session-revocation-error"
	detail := "forced logout persistence detail"
	if err := db.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "sessions" {
			tx.AddError(errors.New(detail))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DB.Callback().Update().Remove(callbackName) })

	result := performAuthRequest(server.Router(), http.MethodPost, "/api/v1/auth/logout", "", cookie, csrf)
	if result.Code != http.StatusInternalServerError || strings.Contains(result.Body.String(), detail) {
		t.Fatalf("logout failure status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestMemberCreationDoesNotExposePersistenceErrors(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	cookie, csrf, _ := createTestActorSession(t, server, "member-error-owner@example.com", "member-error", "owner")

	const callbackName = "test:member-create-persistence-error"
	detail := "forced member database detail"
	if err := db.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New(detail))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DB.Callback().Create().Remove(callbackName) })

	result := performAuthRequest(server.Router(), http.MethodPost, "/api/v1/organization/members", `{"email":"new-member@example.com","password":"new member password","role":"editor"}`, cookie, csrf)
	if result.Code != http.StatusInternalServerError || strings.Contains(result.Body.String(), detail) {
		t.Fatalf("member persistence error status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestInvitationAcceptanceDoesNotExposePersistenceErrors(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	_, _, organization := createTestActorSession(t, server, "invite-error-owner@example.com", "invite-error", "owner")
	var owner models.User
	if err := db.DB.Where("email = ?", "invite-error-owner@example.com").First(&owner).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	invitation := models.OrganizationInvitation{
		OrganizationID: organization.ID, InvitedBy: owner.ID, Email: "invite-error-target@example.com", Role: "editor",
		TokenHash: tokenHash("invite-error-token"), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339), CreatedAt: now.Format(time.RFC3339),
	}
	if err := db.DB.Create(&invitation).Error; err != nil {
		t.Fatal(err)
	}

	const callbackName = "test:invitation-accept-persistence-error"
	detail := "forced invitation database detail"
	if err := db.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New(detail))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DB.Callback().Create().Remove(callbackName) })

	result := performRequest(server.Router(), http.MethodPost, "/api/v1/auth/invitations/invite-error-token/accept", `{"email":"invite-error-target@example.com","new_password":"invited user password"}`, map[string]string{"Content-Type": "application/json"})
	if result.Code != http.StatusInternalServerError || strings.Contains(result.Body.String(), detail) {
		t.Fatalf("invitation persistence error status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestSecurityAndSensitiveResponseHeaders(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SecureCookies: true, SessionTTLHours: 24, CookieName: "fly_session"}
	result := performRequest(server.Router(), http.MethodGet, "/api/v1/auth/status", "", nil)

	wants := map[string]string{
		"Cache-Control":              "no-store",
		"X-Content-Type-Options":     "nosniff",
		"X-Frame-Options":            "DENY",
		"Referrer-Policy":            "no-referrer",
		"Cross-Origin-Opener-Policy": "same-origin",
		"Strict-Transport-Security":  "max-age=31536000",
	}
	for name, want := range wants {
		if got := result.Header().Get(name); got != want {
			t.Errorf("%s=%q want %q", name, got, want)
		}
	}
	if result.Header().Get("Pragma") != "no-cache" {
		t.Errorf("Pragma=%q", result.Header().Get("Pragma"))
	}
	if result.Code != http.StatusOK {
		t.Fatalf("auth status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestPublicStaticRejectsSymlinkOutsideStorageRoot(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth.Enabled = false
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "outside-secret.txt")
	if err := os.WriteFile(outside, []byte("outside secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(server.Store.Root, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	inside := filepath.Join(server.Store.Root, "inside.txt")
	if err := os.WriteFile(inside, []byte("inside media"), 0o600); err != nil {
		t.Fatal(err)
	}
	router := server.Router()

	escape := performRequest(router, http.MethodGet, "/static/escape.txt", "", nil)
	if escape.Code != http.StatusNotFound || strings.Contains(escape.Body.String(), "outside secret") {
		t.Fatalf("symlink escape status=%d body=%q", escape.Code, escape.Body.String())
	}
	regular := performRequest(router, http.MethodGet, "/static/inside.txt", "", nil)
	if regular.Code != http.StatusOK || regular.Body.String() != "inside media" {
		t.Fatalf("regular static status=%d body=%q", regular.Code, regular.Body.String())
	}
	if got := regular.Header().Get("Content-Disposition"); got != "attachment" {
		t.Fatalf("passive non-media Content-Disposition=%q", got)
	}
}

func TestStaticActiveDocumentsAreForcedToDownload(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth.Enabled = false
	path := filepath.Join(server.Store.Root, "payload.html")
	if err := os.WriteFile(path, []byte("<script>alert(1)</script>"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := performRequest(server.Router(), http.MethodGet, "/static/payload.html", "", nil)
	if result.Code != http.StatusOK {
		t.Fatalf("static document status=%d body=%s", result.Code, result.Body.String())
	}
	if got := result.Header().Get("Content-Disposition"); got != "attachment" {
		t.Fatalf("Content-Disposition=%q", got)
	}
	if got := result.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type=%q", got)
	}
}
