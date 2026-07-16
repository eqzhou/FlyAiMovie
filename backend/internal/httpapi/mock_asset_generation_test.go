package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
)

func TestMockAssetGenerationWorkflow(t *testing.T) {
	server, router := testServerRouter(t)
	imageConfig := createMockConfig(t, router, "image")
	videoConfig := createMockConfig(t, router, "video")
	audioConfig := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"asset generation","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	episode := requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"image_config_id":`+itoa(imageConfig)+`,"video_config_id":`+itoa(videoConfig)+`,"audio_config_id":`+itoa(audioConfig)+`}`)
	episodeID := uintField(t, episode, "id")

	character := requestData(t, router, http.MethodPost, "/api/v1/characters", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"name":"林舟","role":"主角","appearance":"黑发，深色外套","voice_style":"mock-voice"}`)
	characterID := uintField(t, character, "id")
	scene := requestData(t, router, http.MethodPost, "/api/v1/scenes", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"location":"旧车站","time":"夜","prompt":"old station at night"}`)
	sceneID := uintField(t, scene, "id")
	prop := requestData(t, router, http.MethodPost, "/api/v1/props", `{"drama_id":`+itoa(dramaID)+`,"name":"怀表","type":"prop","prompt":"antique pocket watch"}`)
	propID := uintField(t, prop, "id")

	charImage := performRequest(router, http.MethodPost, "/api/v1/characters/"+itoa(characterID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`, nil)
	if charImage.Code != http.StatusOK {
		t.Fatalf("character image status=%d body=%s", charImage.Code, charImage.Body.String())
	}
	sceneImage := performRequest(router, http.MethodPost, "/api/v1/scenes/"+itoa(sceneID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`, nil)
	if sceneImage.Code != http.StatusOK {
		t.Fatalf("scene image status=%d body=%s", sceneImage.Code, sceneImage.Body.String())
	}
	propImage := performRequest(router, http.MethodPost, "/api/v1/props/"+itoa(propID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`, nil)
	if propImage.Code != http.StatusOK {
		t.Fatalf("prop image status=%d body=%s", propImage.Code, propImage.Body.String())
	}

	var characterRow models.Character
	var sceneRow models.Scene
	var propRow models.Prop
	if err := db.DB.First(&characterRow, characterID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.First(&sceneRow, sceneID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.First(&propRow, propID).Error; err != nil {
		t.Fatal(err)
	}
	if characterRow.ImageURL == "" || sceneRow.ImageURL == "" || propRow.ImageURL == "" {
		t.Fatalf("generated asset URLs missing: character=%q scene=%q prop=%q", characterRow.ImageURL, sceneRow.ImageURL, propRow.ImageURL)
	}
	assets := performRequest(router, http.MethodGet, "/api/v1/assets?drama_id="+itoa(dramaID), "", nil)
	if assets.Code != http.StatusOK || !strings.Contains(assets.Body.String(), "character") || !strings.Contains(assets.Body.String(), "scene") || !strings.Contains(assets.Body.String(), "prop") {
		t.Fatalf("asset library missing generated categories: status=%d body=%s", assets.Code, assets.Body.String())
	}

	voice := performRequest(router, http.MethodPost, "/api/v1/characters/"+itoa(characterID)+"/generate-voice-sample", `{"episode_id":`+itoa(episodeID)+`}`, nil)
	if voice.Code != http.StatusOK || !strings.Contains(voice.Body.String(), "voice_sample_url") {
		t.Fatalf("voice sample status=%d body=%s", voice.Code, voice.Body.String())
	}
	_ = server
}
