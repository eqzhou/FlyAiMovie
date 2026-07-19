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

func TestCharacterVoiceSampleUsesOrganizationConfigAndEpisode(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	cookie, csrf, organization := createTestActorSession(t, server, "voice-owner@example.com", "voice-org", "owner")
	now := response.Now()
	globalConfig := models.AIServiceConfig{OrganizationID: 0, ServiceType: "audio", Provider: "unsupported", Name: "wrong global", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	organizationConfig := models.AIServiceConfig{OrganizationID: organization.ID, ServiceType: "audio", Provider: "mock", Name: "tenant mock", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&globalConfig).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&organizationConfig).Error; err != nil {
		t.Fatal(err)
	}
	drama := models.Drama{OrganizationID: organization.ID, Title: "Voice Drama", CreatedAt: now, UpdatedAt: now}
	otherDrama := models.Drama{OrganizationID: organization.ID, Title: "Other Drama", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&otherDrama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{OrganizationID: organization.ID, DramaID: drama.ID, EpisodeNumber: 1, AudioConfigID: &organizationConfig.ID, CreatedAt: now, UpdatedAt: now}
	otherEpisode := models.Episode{OrganizationID: organization.ID, DramaID: otherDrama.ID, EpisodeNumber: 1, AudioConfigID: &organizationConfig.ID, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&otherEpisode).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{OrganizationID: organization.ID, DramaID: drama.ID, Name: "林舟", VoiceStyle: "mock-voice", VoiceProvider: "mock", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&character).Error; err != nil {
		t.Fatal(err)
	}

	preview := performAuthRequest(router, http.MethodPost, "/api/v1/characters/"+itoa(character.ID)+"/generate-voice-sample", `{"episode_id":`+itoa(episode.ID)+`}`, cookie, csrf)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), "voice_sample_url") {
		t.Fatalf("tenant voice sample status=%d body=%s", preview.Code, preview.Body.String())
	}
	defaultPreview := performAuthRequest(router, http.MethodPost, "/api/v1/characters/"+itoa(character.ID)+"/generate-voice-sample", `{}`, cookie, csrf)
	if defaultPreview.Code != http.StatusOK {
		t.Fatalf("tenant default voice sample status=%d body=%s", defaultPreview.Code, defaultPreview.Body.String())
	}
	wrongEpisode := performAuthRequest(router, http.MethodPost, "/api/v1/characters/"+itoa(character.ID)+"/generate-voice-sample", `{"episode_id":`+itoa(otherEpisode.ID)+`}`, cookie, csrf)
	if wrongEpisode.Code != http.StatusBadRequest {
		t.Fatalf("cross-drama episode status=%d body=%s", wrongEpisode.Code, wrongEpisode.Body.String())
	}
}

func TestVoiceCatalogSyncAndPreview(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	cookie, csrf, organization := createTestActorSession(t, server, "catalog-owner@example.com", "catalog-org", "owner")
	now := response.Now()
	configRow := models.AIServiceConfig{OrganizationID: organization.ID, ServiceType: "audio", Provider: "mock", Name: "tenant mock", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&configRow).Error; err != nil {
		t.Fatal(err)
	}

	synced := performAuthRequest(router, http.MethodPost, "/api/v1/ai-voices/sync", `{}`, cookie, csrf)
	if synced.Code != http.StatusOK || !strings.Contains(synced.Body.String(), `"count":6`) {
		t.Fatalf("sync status=%d body=%s", synced.Code, synced.Body.String())
	}
	listed := performAuthRequest(router, http.MethodGet, "/api/v1/ai-voices", "", cookie, "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "male-qn-qingse") {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	retired := models.AIVoice{OrganizationID: organization.ID, VoiceID: "retired", VoiceName: "Retired", Provider: "mock", IsActive: false, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&retired).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Model(&retired).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	listed = performAuthRequest(router, http.MethodGet, "/api/v1/ai-voices", "", cookie, "")
	if strings.Contains(listed.Body.String(), "retired") {
		t.Fatalf("inactive voice leaked into default list: %s", listed.Body.String())
	}
	allVoices := performAuthRequest(router, http.MethodGet, "/api/v1/ai-voices?include_inactive=1", "", cookie, "")
	if !strings.Contains(allVoices.Body.String(), "retired") {
		t.Fatalf("inactive voice missing from catalog: %s", allVoices.Body.String())
	}
	invalidProvider := performAuthRequest(router, http.MethodGet, "/api/v1/ai-voices?provider="+strings.Repeat("x", 81), "", cookie, "")
	if invalidProvider.Code != http.StatusBadRequest {
		t.Fatalf("invalid provider status=%d", invalidProvider.Code)
	}
	preview := performAuthRequest(router, http.MethodPost, "/api/v1/ai-voices/male-qn-qingse/preview", `{"text":"欢迎来到音色库"}`, cookie, csrf)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"audio_url":"/static/`) {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	for _, body := range []string{`{"text":""}`, `{"text":"ok","unknown":true}`, `{"text":"` + strings.Repeat("长", 201) + `"}`} {
		invalid := performAuthRequest(router, http.MethodPost, "/api/v1/ai-voices/male-qn-qingse/preview", body, cookie, csrf)
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("invalid preview status=%d body=%s", invalid.Code, invalid.Body.String())
		}
	}
	foreignConfig := models.AIServiceConfig{OrganizationID: organization.ID + 99, ServiceType: "audio", Provider: "mock", Name: "foreign", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&foreignConfig).Error; err != nil {
		t.Fatal(err)
	}
	foreign := performAuthRequest(router, http.MethodPost, "/api/v1/ai-voices/male-qn-qingse/preview", `{"text":"test","config_id":`+itoa(foreignConfig.ID)+`}`, cookie, csrf)
	if foreign.Code != http.StatusBadRequest {
		t.Fatalf("foreign config status=%d body=%s", foreign.Code, foreign.Body.String())
	}
	missingVoice := performAuthRequest(router, http.MethodPost, "/api/v1/ai-voices/missing/preview", `{"text":"test"}`, cookie, csrf)
	if missingVoice.Code != http.StatusNotFound {
		t.Fatalf("missing voice status=%d", missingVoice.Code)
	}
	inactiveVoice := performAuthRequest(router, http.MethodPost, "/api/v1/ai-voices/retired/preview", `{"text":"test"}`, cookie, csrf)
	if inactiveVoice.Code != http.StatusNotFound {
		t.Fatalf("inactive voice status=%d", inactiveVoice.Code)
	}
	if err := db.DB.Model(&configRow).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	missingConfig := performAuthRequest(router, http.MethodPost, "/api/v1/ai-voices/male-qn-qingse/preview", `{"text":"test"}`, cookie, csrf)
	if missingConfig.Code != http.StatusBadRequest {
		t.Fatalf("missing provider config status=%d body=%s", missingConfig.Code, missingConfig.Body.String())
	}
}

func TestVoiceSyncRejectsMissingAndUnsupportedAudioConfig(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	cookie, csrf, organization := createTestActorSession(t, server, "voice-errors@example.com", "voice-errors", "owner")
	missing := performAuthRequest(router, http.MethodPost, "/api/v1/ai-voices/sync", `{}`, cookie, csrf)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing config status=%d body=%s", missing.Code, missing.Body.String())
	}
	now := response.Now()
	unsupported := models.AIServiceConfig{OrganizationID: organization.ID, ServiceType: "audio", Provider: "unsupported", Name: "unsupported", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&unsupported).Error; err != nil {
		t.Fatal(err)
	}
	result := performAuthRequest(router, http.MethodPost, "/api/v1/ai-voices/sync", `{}`, cookie, csrf)
	if result.Code != http.StatusBadRequest {
		t.Fatalf("unsupported config status=%d body=%s", result.Code, result.Body.String())
	}
}
