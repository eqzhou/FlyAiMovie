package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestAuthRegisterCreatesOwnerWorkspaceAndSession(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()

	setup := performRequest(router, http.MethodPost, "/api/v1/auth/setup", `{
		"organization_name":"Platform","email":"platform@example.com",
		"display_name":"Platform","password":"correct horse battery staple"
	}`, nil)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}
	setupPayload := decodeResponse(t, setup)["data"].(map[string]any)
	setupUser := setupPayload["user"].(map[string]any)
	if setupUser["is_platform_admin"] != true {
		t.Fatalf("setup user should be platform admin: %v", setupUser)
	}

	status := performRequest(router, http.MethodGet, "/api/v1/auth/status", "", nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"registration_enabled":true`) {
		t.Fatalf("status=%d %s", status.Code, status.Body.String())
	}
	if !strings.Contains(status.Body.String(), `"require_email_verification":false`) {
		t.Fatalf("status missing require_email_verification=false: %s", status.Body.String())
	}

	reg := performRequest(router, http.MethodPost, "/api/v1/auth/register", `{
		"organization_name":"Indie Studio","email":"indie@example.com",
		"display_name":"Indie","password":"correct horse battery staple"
	}`, nil)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register=%d %s", reg.Code, reg.Body.String())
	}
	if responseCookie(t, reg, "fly_session") == "" {
		t.Fatal("missing session cookie")
	}
	payload := decodeResponse(t, reg)["data"].(map[string]any)
	user := payload["user"].(map[string]any)
	if payload["role"] != "owner" || user["is_platform_admin"] == true {
		t.Fatalf("payload=%v", payload)
	}
	if user["email"] != "indie@example.com" || user["display_name"] != "Indie" {
		t.Fatalf("user payload=%v", user)
	}

	var orgCount int64
	if err := db.DB.Model(&models.Organization{}).Count(&orgCount).Error; err != nil {
		t.Fatal(err)
	}
	if orgCount < 2 {
		t.Fatalf("orgCount=%d", orgCount)
	}

	var registered models.User
	if err := db.DB.Where("email = ?", "indie@example.com").First(&registered).Error; err != nil {
		t.Fatal(err)
	}
	if registered.IsPlatformAdmin {
		t.Fatal("registered user must not be platform admin")
	}
	if registered.EmailVerifiedAt == nil || *registered.EmailVerifiedAt == "" {
		t.Fatal("registered user should be verified when verification is not required")
	}

	var organization models.Organization
	if err := db.DB.Where("slug = ?", "indie-studio").First(&organization).Error; err != nil {
		t.Fatal(err)
	}
	var membership models.Membership
	if err := db.DB.Where("organization_id = ? AND user_id = ?", organization.ID, registered.ID).First(&membership).Error; err != nil {
		t.Fatal(err)
	}
	if membership.Role != "owner" {
		t.Fatalf("membership role=%s", membership.Role)
	}
	var mockCount, agentCount int64
	if err := db.DB.Model(&models.AIServiceConfig{}).Where("organization_id = ? AND provider = ?", organization.ID, "mock").Count(&mockCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Model(&models.AgentConfig{}).Where("organization_id = ?", organization.ID).Count(&agentCount).Error; err != nil {
		t.Fatal(err)
	}
	if mockCount != 4 || agentCount != 5 {
		t.Fatalf("organization defaults: mock=%d agents=%d", mockCount, agentCount)
	}
}

func TestAuthRegisterRejectedWhenDisabledOrDuplicate(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()

	setup := performRequest(router, http.MethodPost, "/api/v1/auth/setup", `{
		"organization_name":"Platform","email":"platform@example.com",
		"display_name":"Platform","password":"correct horse battery staple"
	}`, nil)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}

	if err := db.DB.Model(&models.PlatformSettings{}).Where("id = ?", 1).Update("registration_enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	disabled := performRequest(router, http.MethodPost, "/api/v1/auth/register", `{
		"organization_name":"Indie Studio","email":"indie@example.com",
		"display_name":"Indie","password":"correct horse battery staple"
	}`, nil)
	if disabled.Code != http.StatusForbidden || !strings.Contains(disabled.Body.String(), "registration disabled") {
		t.Fatalf("disabled register=%d %s", disabled.Code, disabled.Body.String())
	}

	if err := db.DB.Model(&models.PlatformSettings{}).Where("id = ?", 1).Update("registration_enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	first := performRequest(router, http.MethodPost, "/api/v1/auth/register", `{
		"organization_name":"Indie Studio","email":"indie@example.com",
		"display_name":"Indie","password":"correct horse battery staple"
	}`, nil)
	if first.Code != http.StatusCreated {
		t.Fatalf("first register=%d %s", first.Code, first.Body.String())
	}
	duplicate := performRequest(router, http.MethodPost, "/api/v1/auth/register", `{
		"organization_name":"Another Studio","email":"INDIE@example.com",
		"display_name":"Indie2","password":"correct horse battery staple"
	}`, nil)
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), "email already registered") {
		t.Fatalf("duplicate register=%d %s", duplicate.Code, duplicate.Body.String())
	}
}

func TestAuthRegisterRequiresCompletedSetup(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()

	reg := performRequest(router, http.MethodPost, "/api/v1/auth/register", `{
		"organization_name":"Indie Studio","email":"indie@example.com",
		"display_name":"Indie","password":"correct horse battery staple"
	}`, nil)
	if reg.Code != http.StatusConflict && reg.Code != http.StatusBadRequest {
		t.Fatalf("register without setup=%d %s", reg.Code, reg.Body.String())
	}
	body := strings.ToLower(reg.Body.String())
	if !strings.Contains(body, "setup") {
		t.Fatalf("expected setup guidance, got %s", reg.Body.String())
	}
}

func TestAuthRegisterVerificationRequiredSkipsSession(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()

	setup := performRequest(router, http.MethodPost, "/api/v1/auth/setup", `{
		"organization_name":"Platform","email":"platform@example.com",
		"display_name":"Platform","password":"correct horse battery staple"
	}`, nil)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}
	if err := db.DB.Model(&models.PlatformSettings{}).Where("id = ?", 1).Update("require_email_verification", true).Error; err != nil {
		t.Fatal(err)
	}

	reg := performRequest(router, http.MethodPost, "/api/v1/auth/register", `{
		"organization_name":"Indie Studio","email":"indie@example.com",
		"display_name":"Indie","password":"correct horse battery staple"
	}`, nil)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register=%d %s", reg.Code, reg.Body.String())
	}
	for _, cookie := range reg.Result().Cookies() {
		if cookie.Name == "fly_session" && cookie.Value != "" && cookie.MaxAge >= 0 {
			t.Fatalf("session cookie should not be issued when verification is required: %#v", cookie)
		}
	}
	payload := decodeResponse(t, reg)["data"].(map[string]any)
	if payload["verification_required"] != true || payload["email"] != "indie@example.com" {
		t.Fatalf("payload=%v", payload)
	}
	if _, ok := payload["role"]; ok {
		t.Fatalf("should not return actor role when verification required: %v", payload)
	}

	var registered models.User
	if err := db.DB.Where("email = ?", "indie@example.com").First(&registered).Error; err != nil {
		t.Fatal(err)
	}
	if registered.EmailVerifiedAt != nil {
		t.Fatalf("email_verified_at should be nil, got %v", registered.EmailVerifiedAt)
	}
	if registered.IsPlatformAdmin {
		t.Fatal("registered user must not be platform admin")
	}
}

func TestPlatformSettingsOnlyPlatformAdmin(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()

	setup := performRequest(router, http.MethodPost, "/api/v1/auth/setup", `{
		"organization_name":"Platform","email":"platform@example.com",
		"display_name":"Platform","password":"correct horse battery staple"
	}`, nil)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}
	adminCookie := responseCookie(t, setup, "fly_session")
	adminCSRF := authCSRFToken(t, setup)

	getAdmin := performAuthRequest(router, http.MethodGet, "/api/v1/auth/platform-settings", "", adminCookie, "")
	if getAdmin.Code != http.StatusOK {
		t.Fatalf("admin get=%d %s", getAdmin.Code, getAdmin.Body.String())
	}
	getPayload := decodeResponse(t, getAdmin)["data"].(map[string]any)
	if getPayload["registration_enabled"] != true || getPayload["require_email_verification"] != false {
		t.Fatalf("default settings payload=%v", getPayload)
	}

	putAdmin := performAuthRequest(router, http.MethodPut, "/api/v1/auth/platform-settings", `{
		"registration_enabled":false,
		"require_email_verification":true
	}`, adminCookie, adminCSRF)
	if putAdmin.Code != http.StatusOK {
		t.Fatalf("admin put=%d %s", putAdmin.Code, putAdmin.Body.String())
	}
	putPayload := decodeResponse(t, putAdmin)["data"].(map[string]any)
	if putPayload["registration_enabled"] != false || putPayload["require_email_verification"] != true {
		t.Fatalf("updated settings payload=%v", putPayload)
	}

	var settings models.PlatformSettings
	if err := db.DB.First(&settings, 1).Error; err != nil {
		t.Fatal(err)
	}
	if settings.RegistrationEnabled || !settings.RequireEmailVerification {
		t.Fatalf("db settings not updated: %+v", settings)
	}
	if settings.UpdatedBy == nil || *settings.UpdatedBy == 0 {
		t.Fatalf("updated_by should be set: %+v", settings)
	}
	if settings.UpdatedAt == "" {
		t.Fatal("updated_at should be set")
	}

	// Re-enable registration so a normal owner can register.
	restore := performAuthRequest(router, http.MethodPut, "/api/v1/auth/platform-settings", `{
		"registration_enabled":true,
		"require_email_verification":false
	}`, adminCookie, adminCSRF)
	if restore.Code != http.StatusOK {
		t.Fatalf("restore put=%d %s", restore.Code, restore.Body.String())
	}

	reg := performRequest(router, http.MethodPost, "/api/v1/auth/register", `{
		"organization_name":"Indie Studio","email":"indie@example.com",
		"display_name":"Indie","password":"correct horse battery staple"
	}`, nil)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register=%d %s", reg.Code, reg.Body.String())
	}
	ownerCookie := responseCookie(t, reg, "fly_session")
	ownerCSRF := authCSRFToken(t, reg)

	getOwner := performAuthRequest(router, http.MethodGet, "/api/v1/auth/platform-settings", "", ownerCookie, "")
	if getOwner.Code != http.StatusForbidden || !strings.Contains(getOwner.Body.String(), "platform admin required") {
		t.Fatalf("owner get=%d %s", getOwner.Code, getOwner.Body.String())
	}
	putOwner := performAuthRequest(router, http.MethodPut, "/api/v1/auth/platform-settings", `{
		"registration_enabled":false,
		"require_email_verification":true
	}`, ownerCookie, ownerCSRF)
	if putOwner.Code != http.StatusForbidden || !strings.Contains(putOwner.Body.String(), "platform admin required") {
		t.Fatalf("owner put=%d %s", putOwner.Code, putOwner.Body.String())
	}
}

func TestEmailVerificationGateOnRegisterAndLogin(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()

	setup := performRequest(router, http.MethodPost, "/api/v1/auth/setup", `{
		"organization_name":"Platform","email":"platform@example.com",
		"display_name":"Platform","password":"correct horse battery staple"
	}`, nil)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}
	adminCookie := responseCookie(t, setup, "fly_session")
	adminCSRF := authCSRFToken(t, setup)

	enable := performAuthRequest(router, http.MethodPut, "/api/v1/auth/platform-settings", `{
		"registration_enabled":true,
		"require_email_verification":true
	}`, adminCookie, adminCSRF)
	if enable.Code != http.StatusOK {
		t.Fatalf("enable verification=%d %s", enable.Code, enable.Body.String())
	}

	reg := performRequest(router, http.MethodPost, "/api/v1/auth/register", `{
		"organization_name":"Indie Studio","email":"indie@example.com",
		"display_name":"Indie","password":"correct horse battery staple"
	}`, nil)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register=%d %s", reg.Code, reg.Body.String())
	}
	for _, cookie := range reg.Result().Cookies() {
		if cookie.Name == "fly_session" && cookie.Value != "" && cookie.MaxAge >= 0 {
			t.Fatalf("session cookie should not be issued when verification is required: %#v", cookie)
		}
	}
	payload := decodeResponse(t, reg)["data"].(map[string]any)
	if payload["verification_required"] != true || payload["email"] != "indie@example.com" {
		t.Fatalf("payload=%v", payload)
	}

	blockedLogin := performRequest(router, http.MethodPost, "/api/v1/auth/login", `{
		"email":"indie@example.com","password":"correct horse battery staple"
	}`, nil)
	if blockedLogin.Code != http.StatusForbidden || !strings.Contains(blockedLogin.Body.String(), "email verification required") {
		t.Fatalf("blocked login=%d %s", blockedLogin.Code, blockedLogin.Body.String())
	}
	for _, cookie := range blockedLogin.Result().Cookies() {
		if cookie.Name == "fly_session" && cookie.Value != "" && cookie.MaxAge >= 0 {
			t.Fatalf("blocked login must not set session cookie: %#v", cookie)
		}
	}

	now := response.Now()
	if err := db.DB.Model(&models.User{}).Where("email = ?", "indie@example.com").Update("email_verified_at", now).Error; err != nil {
		t.Fatal(err)
	}

	okLogin := performRequest(router, http.MethodPost, "/api/v1/auth/login", `{
		"email":"indie@example.com","password":"correct horse battery staple"
	}`, nil)
	if okLogin.Code != http.StatusOK {
		t.Fatalf("verified login=%d %s", okLogin.Code, okLogin.Body.String())
	}
	if responseCookie(t, okLogin, "fly_session") == "" {
		t.Fatal("verified login missing session cookie")
	}
}
