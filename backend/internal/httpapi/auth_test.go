package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestAuthSetupLoginCSRFAndLogout(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()

	unauthorized := performRequest(router, http.MethodGet, "/api/v1/dramas", "", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	status := performRequest(router, http.MethodGet, "/api/v1/auth/status", "", nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"setup_required":true`) {
		t.Fatalf("status=%d body=%s", status.Code, status.Body.String())
	}

	setup := performRequest(router, http.MethodPost, "/api/v1/auth/setup", `{
		"organization_name":"Studio One","email":"owner@example.com",
		"display_name":"Owner","password":"correct horse battery staple"
	}`, nil)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup status=%d body=%s", setup.Code, setup.Body.String())
	}
	cookie := responseCookie(t, setup, "fly_session")
	csrf := authCSRFToken(t, setup)

	secondSetup := performRequest(router, http.MethodPost, "/api/v1/auth/setup", `{
		"organization_name":"Other","email":"other@example.com","password":"another long secure password"
	}`, nil)
	if secondSetup.Code != http.StatusConflict {
		t.Fatalf("second setup status=%d body=%s", secondSetup.Code, secondSetup.Body.String())
	}

	withoutCSRF := performAuthRequest(router, http.MethodPost, "/api/v1/dramas", `{"title":"blocked"}`, cookie, "")
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing csrf status=%d body=%s", withoutCSRF.Code, withoutCSRF.Body.String())
	}
	created := performAuthRequest(router, http.MethodPost, "/api/v1/dramas", `{"title":"private drama"}`, cookie, csrf)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}

	me := performAuthRequest(router, http.MethodGet, "/api/v1/auth/me", "", cookie, "")
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"role":"owner"`) {
		t.Fatalf("me status=%d body=%s", me.Code, me.Body.String())
	}

	logout := performAuthRequest(router, http.MethodPost, "/api/v1/auth/logout", "", cookie, csrf)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}
	revoked := performAuthRequest(router, http.MethodGet, "/api/v1/auth/me", "", cookie, "")
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("revoked status=%d body=%s", revoked.Code, revoked.Body.String())
	}

	wrong := performRequest(router, http.MethodPost, "/api/v1/auth/login", `{"email":"owner@example.com","password":"wrong password"}`, nil)
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong login status=%d body=%s", wrong.Code, wrong.Body.String())
	}
	login := performRequest(router, http.MethodPost, "/api/v1/auth/login", `{
		"email":"OWNER@example.com","password":"correct horse battery staple"
	}`, nil)
	if login.Code != http.StatusOK || responseCookie(t, login, "fly_session") == "" {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
}

func TestAuthSetupSeedsOrganizationDefaultsWithoutLegacyRows(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&models.AIServiceConfig{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&models.AgentConfig{}).Error; err != nil {
		t.Fatal(err)
	}

	setup := performRequest(server.Router(), http.MethodPost, "/api/v1/auth/setup", `{
		"organization_name":"Fresh Studio","email":"fresh@example.com",
		"display_name":"Fresh Owner","password":"correct horse battery staple"
	}`, nil)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup status=%d body=%s", setup.Code, setup.Body.String())
	}
	var organization models.Organization
	if err := db.DB.Where("slug = ?", "fresh-studio").First(&organization).Error; err != nil {
		t.Fatal(err)
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

func TestAuthSetupRollsBackWhenOrganizationDefaultsFail(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&models.AIServiceConfig{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&models.AgentConfig{}).Error; err != nil {
		t.Fatal(err)
	}
	callbackName := "test:fail-organization-defaults"
	if err := db.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*models.AgentConfig); ok {
			tx.AddError(errors.New("forced organization defaults failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DB.Callback().Create().Remove(callbackName) })

	setup := performRequest(server.Router(), http.MethodPost, "/api/v1/auth/setup", `{
		"organization_name":"Rollback Studio","email":"rollback@example.com",
		"display_name":"Rollback Owner","password":"correct horse battery staple"
	}`, nil)
	if setup.Code != http.StatusInternalServerError {
		t.Fatalf("setup status=%d body=%s", setup.Code, setup.Body.String())
	}
	for name, model := range map[string]any{
		"organizations": &models.Organization{}, "users": &models.User{}, "memberships": &models.Membership{},
	} {
		var count int64
		if err := db.DB.Model(model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s count=%d after rollback", name, count)
		}
	}
}

func TestViewerCanReadButCannotMutate(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	now := response.Now()
	organization := models.Organization{Name: "Viewer Org", Slug: "viewer-org", Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&organization).Error; err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("viewer secure password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{Email: "viewer@example.com", PasswordHash: string(hash), DisplayName: "Viewer", Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	membership := models.Membership{OrganizationID: organization.ID, UserID: user.ID, Role: "viewer", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&membership).Error; err != nil {
		t.Fatal(err)
	}
	token, csrf, err := server.createSession(user.ID, organization.ID)
	if err != nil {
		t.Fatal(err)
	}
	cookie := "fly_session=" + token

	read := performAuthRequest(router, http.MethodGet, "/api/v1/dramas", "", cookie, "")
	if read.Code != http.StatusOK {
		t.Fatalf("viewer read status=%d body=%s", read.Code, read.Body.String())
	}
	write := performAuthRequest(router, http.MethodPost, "/api/v1/dramas", `{"title":"forbidden"}`, cookie, csrf)
	if write.Code != http.StatusForbidden {
		t.Fatalf("viewer write status=%d body=%s", write.Code, write.Body.String())
	}
}

func TestDramaEndpointsAreIsolatedByOrganization(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	cookieA, csrfA, organizationA := createTestActorSession(t, server, "owner-a@example.com", "org-a", "owner")
	cookieB, csrfB, _ := createTestActorSession(t, server, "owner-b@example.com", "org-b", "owner")

	created := performAuthRequest(router, http.MethodPost, "/api/v1/dramas", `{"title":"tenant A drama"}`, cookieA, csrfA)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	dramaID := uint(decodeResponse(t, created)["data"].(map[string]any)["id"].(float64))
	var drama models.Drama
	if err := db.DB.First(&drama, dramaID).Error; err != nil {
		t.Fatal(err)
	}
	if drama.OrganizationID != organizationA.ID {
		t.Fatalf("organization_id=%d want %d", drama.OrganizationID, organizationA.ID)
	}

	listB := performAuthRequest(router, http.MethodGet, "/api/v1/dramas", "", cookieB, "")
	if listB.Code != http.StatusOK || strings.Contains(listB.Body.String(), "tenant A drama") {
		t.Fatalf("tenant B list leaked drama: status=%d body=%s", listB.Code, listB.Body.String())
	}
	getB := performAuthRequest(router, http.MethodGet, "/api/v1/dramas/"+itoa(dramaID), "", cookieB, "")
	if getB.Code != http.StatusNotFound {
		t.Fatalf("tenant B get status=%d body=%s", getB.Code, getB.Body.String())
	}
	updateB := performAuthRequest(router, http.MethodPut, "/api/v1/dramas/"+itoa(dramaID), `{"title":"stolen"}`, cookieB, csrfB)
	if updateB.Code != http.StatusNotFound {
		t.Fatalf("tenant B update status=%d body=%s", updateB.Code, updateB.Body.String())
	}
	deleteB := performAuthRequest(router, http.MethodDelete, "/api/v1/dramas/"+itoa(dramaID), "", cookieB, csrfB)
	if deleteB.Code != http.StatusNotFound {
		t.Fatalf("tenant B delete status=%d body=%s", deleteB.Code, deleteB.Body.String())
	}
}

func TestInitialSetupClaimsLegacyResources(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	now := response.Now()
	legacy := models.Drama{Title: "legacy", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	setup := performRequest(router, http.MethodPost, "/api/v1/auth/setup", `{
		"organization_name":"Legacy Studio","email":"legacy@example.com","password":"legacy secure password"
	}`, nil)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup status=%d body=%s", setup.Code, setup.Body.String())
	}
	if err := db.DB.First(&legacy, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if legacy.OrganizationID == 0 {
		t.Fatal("legacy drama was not assigned to the initial organization")
	}
}

func TestChildResourcesRejectCrossOrganizationIDs(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	_, _, organizationA := createTestActorSession(t, server, "child-a@example.com", "child-a", "owner")
	cookieB, csrfB, _ := createTestActorSession(t, server, "child-b@example.com", "child-b", "owner")
	now := response.Now()
	drama := models.Drama{OrganizationID: organizationA.ID, Title: "A", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{OrganizationID: organizationA.ID, DramaID: drama.ID, EpisodeNumber: 1, Title: "A1", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{OrganizationID: organizationA.ID, DramaID: drama.ID, Name: "A person", CreatedAt: now, UpdatedAt: now}
	scene := models.Scene{OrganizationID: organizationA.ID, DramaID: drama.ID, Location: "A room", Time: "day", Prompt: "room", CreatedAt: now, UpdatedAt: now}
	storyboard := models.Storyboard{OrganizationID: organizationA.ID, EpisodeID: episode.ID, StoryboardNumber: 1, Status: "pending", CreatedAt: now, UpdatedAt: now}
	prop := models.Prop{OrganizationID: organizationA.ID, DramaID: drama.ID, Name: "A prop", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&prop).Error; err != nil {
		t.Fatal(err)
	}
	dramaID, episodeID := drama.ID, episode.ID
	asset := models.Asset{OrganizationID: organizationA.ID, DramaID: &dramaID, EpisodeID: &episodeID, Name: "A asset", Type: "image", URL: "/static/a.png", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/episodes/" + itoa(episode.ID) + "/characters", ""},
		{http.MethodPut, "/api/v1/characters/" + itoa(character.ID), `{"name":"stolen"}`},
		{http.MethodPut, "/api/v1/scenes/" + itoa(scene.ID), `{"location":"stolen"}`},
		{http.MethodPut, "/api/v1/storyboards/" + itoa(storyboard.ID), `{"title":"stolen"}`},
		{http.MethodGet, "/api/v1/assets/" + itoa(asset.ID), ""},
	}
	for _, test := range tests {
		responseRecorder := performAuthRequest(router, test.method, test.path, test.body, cookieB, csrfB)
		if responseRecorder.Code != http.StatusNotFound {
			t.Errorf("%s %s status=%d body=%s", test.method, test.path, responseRecorder.Code, responseRecorder.Body.String())
		}
	}
	props := performAuthRequest(router, http.MethodGet, "/api/v1/props?drama_id="+itoa(drama.ID), "", cookieB, "")
	if props.Code != http.StatusOK || strings.Contains(props.Body.String(), "A prop") {
		t.Fatalf("props leaked cross-organization resource: status=%d body=%s", props.Code, props.Body.String())
	}
}

func TestSceneAndPropUpdatesRejectMalformedOrEmptyBodies(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	cookie, csrf, organization := createTestActorSession(t, server, "updates@example.com", "updates", "owner")
	now := response.Now()
	drama := models.Drama{OrganizationID: organization.ID, Title: "Updates", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{OrganizationID: organization.ID, DramaID: drama.ID, Location: "room", Time: "day", Prompt: "room", CreatedAt: now, UpdatedAt: now}
	prop := models.Prop{OrganizationID: organization.ID, DramaID: drama.ID, Name: "watch", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&prop).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path string
		body string
	}{
		{"/api/v1/scenes/" + itoa(scene.ID), "{"},
		{"/api/v1/scenes/" + itoa(scene.ID), "{}"},
		{"/api/v1/scenes/" + itoa(scene.ID), `{"time":12}`},
		{"/api/v1/props/" + itoa(prop.ID), "{"},
		{"/api/v1/props/" + itoa(prop.ID), "{}"},
		{"/api/v1/props/" + itoa(prop.ID), `{"name":12}`},
	} {
		got := performAuthRequest(router, http.MethodPut, test.path, test.body, cookie, csrf)
		if got.Code != http.StatusBadRequest {
			t.Errorf("PUT %s body=%s status=%d body=%s", test.path, test.body, got.Code, got.Body.String())
		}
	}
	good := performAuthRequest(router, http.MethodPut, "/api/v1/scenes/"+itoa(scene.ID), `{"location":"hall"}`, cookie, csrf)
	if good.Code != http.StatusOK {
		t.Fatalf("valid scene update status=%d body=%s", good.Code, good.Body.String())
	}
	good = performAuthRequest(router, http.MethodPut, "/api/v1/props/"+itoa(prop.ID), `{"name":"clock"}`, cookie, csrf)
	if good.Code != http.StatusOK {
		t.Fatalf("valid prop update status=%d body=%s", good.Code, good.Body.String())
	}
}

func TestPrivateMediaRequiresOwningOrganization(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	cookieA, _, organizationA := createTestActorSession(t, server, "media-a@example.com", "media-a", "owner")
	cookieB, _, _ := createTestActorSession(t, server, "media-b@example.com", "media-b", "owner")
	rel, _, err := server.Store.Save("uploads", "private.txt", strings.NewReader("private media"))
	if err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	asset := models.Asset{OrganizationID: organizationA.ID, Name: "private", Type: "document", URL: server.Store.PublicURL(rel), LocalPath: rel, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}

	owner := performAuthRequest(router, http.MethodGet, server.Store.PublicURL(rel), "", cookieA, "")
	if owner.Code != http.StatusOK || owner.Body.String() != "private media" {
		t.Fatalf("owner media status=%d body=%q", owner.Code, owner.Body.String())
	}
	foreign := performAuthRequest(router, http.MethodGet, server.Store.PublicURL(rel), "", cookieB, "")
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign media status=%d body=%s", foreign.Code, foreign.Body.String())
	}
	anonymous := performRequest(router, http.MethodGet, server.Store.PublicURL(rel), "", nil)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous media status=%d body=%s", anonymous.Code, anonymous.Body.String())
	}
}

func TestOrganizationQuotaIsScopedAndAdminManaged(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	ownerCookie, ownerCSRF, _ := createTestActorSession(t, server, "quota-owner@example.com", "quota-owner", "owner")
	viewerCookie, viewerCSRF, _ := createTestActorSession(t, server, "quota-viewer@example.com", "quota-viewer", "viewer")

	updated := performAuthRequest(router, http.MethodPut, "/api/v1/organization/quota", `{"daily_job_limit":25,"max_active_jobs":3}`, ownerCookie, ownerCSRF)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"daily_job_limit":25`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	ownerQuota := performAuthRequest(router, http.MethodGet, "/api/v1/organization/quota", "", ownerCookie, "")
	if ownerQuota.Code != http.StatusOK || !strings.Contains(ownerQuota.Body.String(), `"max_active_jobs":3`) {
		t.Fatalf("owner status=%d body=%s", ownerQuota.Code, ownerQuota.Body.String())
	}
	viewerQuota := performAuthRequest(router, http.MethodGet, "/api/v1/organization/quota", "", viewerCookie, "")
	if viewerQuota.Code != http.StatusOK || !strings.Contains(viewerQuota.Body.String(), `"daily_job_limit":200`) {
		t.Fatalf("viewer scoped status=%d body=%s", viewerQuota.Code, viewerQuota.Body.String())
	}
	blocked := performAuthRequest(router, http.MethodPut, "/api/v1/organization/quota", `{"daily_job_limit":1,"max_active_jobs":1}`, viewerCookie, viewerCSRF)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("viewer update status=%d body=%s", blocked.Code, blocked.Body.String())
	}
}

func createTestActorSession(t *testing.T, server *Server, email, slug, role string) (string, string, models.Organization) {
	t.Helper()
	now := response.Now()
	organization := models.Organization{Name: slug, Slug: slug, Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&organization).Error; err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("test actor password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{Email: email, PasswordHash: string(hash), DisplayName: email, Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	membership := models.Membership{OrganizationID: organization.ID, UserID: user.ID, Role: role, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&membership).Error; err != nil {
		t.Fatal(err)
	}
	token, csrf, err := server.createSession(user.ID, organization.ID)
	if err != nil {
		t.Fatal(err)
	}
	return server.Cfg.Auth.CookieName + "=" + token, csrf, organization
}

func TestAuthRejectsWeakSetupPasswordAndProtectsStaticFiles(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, CookieName: "fly_session"}
	router := server.Router()

	weak := performRequest(router, http.MethodPost, "/api/v1/auth/setup", `{
		"organization_name":"Studio","email":"owner@example.com","password":"short"
	}`, nil)
	if weak.Code != http.StatusBadRequest {
		t.Fatalf("weak password status=%d body=%s", weak.Code, weak.Body.String())
	}
	media := performRequest(router, http.MethodGet, "/static/not-found.png", "", nil)
	if media.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous media status=%d body=%s", media.Code, media.Body.String())
	}
}

func performAuthRequest(handler http.Handler, method, path, body, cookie, csrf string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func responseCookie(t *testing.T, response *httptest.ResponseRecorder, name string) string {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatalf("unsafe session cookie: %#v", cookie)
			}
			return cookie.Name + "=" + cookie.Value
		}
	}
	t.Fatalf("cookie %q missing", name)
	return ""
}

func authCSRFToken(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Data struct {
			CSRFToken string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.CSRFToken == "" {
		t.Fatal("csrf token missing")
	}
	return payload.Data.CSRFToken
}
