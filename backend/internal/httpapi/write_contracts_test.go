package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestWriteEndpointsRejectMalformedJSON(t *testing.T) {
	router := testRouter(t)
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPut, "/api/v1/dramas/1"},
		{http.MethodPut, "/api/v1/episodes/1"},
		{http.MethodPut, "/api/v1/characters/1"},
		{http.MethodPut, "/api/v1/scenes/1"},
		{http.MethodPut, "/api/v1/storyboards/1"},
		{http.MethodPut, "/api/v1/props/1"},
		{http.MethodPut, "/api/v1/assets/1"},
		{http.MethodPut, "/api/v1/ai-configs/1"},
		{http.MethodPut, "/api/v1/agent-configs/1"},
		{http.MethodPost, "/api/v1/characters/1/generate-voice-sample"},
		{http.MethodPost, "/api/v1/characters/1/generate-image"},
		{http.MethodPost, "/api/v1/scenes/1/generate-image"},
		{http.MethodPost, "/api/v1/props/1/generate-image"},
		{http.MethodPost, "/api/v1/storyboards/1/generate-frame"},
		{http.MethodPost, "/api/v1/storyboards/batch-generate-videos"},
		{http.MethodPost, "/api/v1/storyboards/batch-generate-tts"},
		{http.MethodPost, "/api/v1/storyboards/1/generate-video"},
		{http.MethodPost, "/api/v1/grid/prompt"},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			got := performRequest(router, test.method, test.path, `{"broken":`, nil)
			if got.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want 400; body=%s", got.Code, got.Body.String())
			}
		})
	}
}

func TestUpdateEndpointsRejectEmptyAndUnknownBodies(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "contract", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "episode", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{DramaID: drama.ID, Name: "character", CreatedAt: now, UpdatedAt: now}
	scene := models.Scene{DramaID: drama.ID, Location: "scene", CreatedAt: now, UpdatedAt: now}
	prop := models.Prop{DramaID: drama.ID, Name: "prop", CreatedAt: now, UpdatedAt: now}
	storyboard := models.Storyboard{EpisodeID: episode.ID, StoryboardNumber: 1, Title: "shot", CreatedAt: now, UpdatedAt: now}
	asset := models.Asset{DramaID: &drama.ID, Name: "asset", Type: "image", URL: "/static/a.png", CreatedAt: now, UpdatedAt: now}
	aiConfig := models.AIServiceConfig{ServiceType: "image", Provider: "mock", Name: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now}
	agentConfig := models.AgentConfig{AgentType: "script_rewriter", Name: "agent", IsActive: true, CreatedAt: now, UpdatedAt: now}
	for _, model := range []any{&character, &scene, &prop, &storyboard, &asset, &aiConfig, &agentConfig} {
		if err := db.DB.Create(model).Error; err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		path string
	}{
		{"/api/v1/dramas/" + itoa(drama.ID)},
		{"/api/v1/episodes/" + itoa(episode.ID)},
		{"/api/v1/characters/" + itoa(character.ID)},
		{"/api/v1/scenes/" + itoa(scene.ID)},
		{"/api/v1/storyboards/" + itoa(storyboard.ID)},
		{"/api/v1/props/" + itoa(prop.ID)},
		{"/api/v1/assets/" + itoa(asset.ID)},
		{"/api/v1/ai-configs/" + itoa(aiConfig.ID)},
		{"/api/v1/agent-configs/" + itoa(agentConfig.ID)},
	}
	for _, test := range tests {
		for _, body := range []string{`{}`, `{"unknown":"field"}`} {
			t.Run(test.path+" "+body, func(t *testing.T) {
				got := performRequest(router, http.MethodPut, test.path, body, nil)
				if got.Code != http.StatusBadRequest {
					t.Fatalf("status=%d want 400; body=%s", got.Code, got.Body.String())
				}
			})
		}
	}
	mixedBodies := map[string]string{
		"/api/v1/dramas/" + itoa(drama.ID):              `{"title":"updated","unknown":"field"}`,
		"/api/v1/episodes/" + itoa(episode.ID):          `{"title":"updated","unknown":"field"}`,
		"/api/v1/characters/" + itoa(character.ID):      `{"name":"updated","unknown":"field"}`,
		"/api/v1/scenes/" + itoa(scene.ID):              `{"location":"updated","unknown":"field"}`,
		"/api/v1/storyboards/" + itoa(storyboard.ID):    `{"title":"updated","unknown":"field"}`,
		"/api/v1/props/" + itoa(prop.ID):                `{"name":"updated","unknown":"field"}`,
		"/api/v1/assets/" + itoa(asset.ID):              `{"name":"updated","unknown":"field"}`,
		"/api/v1/ai-configs/" + itoa(aiConfig.ID):       `{"name":"updated","unknown":"field"}`,
		"/api/v1/agent-configs/" + itoa(agentConfig.ID): `{"name":"updated","unknown":"field"}`,
	}
	for path, body := range mixedBodies {
		got := performRequest(router, http.MethodPut, path, body, nil)
		if got.Code != http.StatusBadRequest || !strings.Contains(got.Body.String(), "unknown field") {
			t.Errorf("%s: status=%d want 400 unknown field; body=%s", path, got.Code, got.Body.String())
		}
	}
}

func TestResourceWritesRejectWrongTypesAndOversizedNames(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "contract", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{DramaID: drama.ID, Name: "character", CreatedAt: now, UpdatedAt: now}
	scene := models.Scene{DramaID: drama.ID, Location: "scene", CreatedAt: now, UpdatedAt: now}
	prop := models.Prop{DramaID: drama.ID, Name: "prop", CreatedAt: now, UpdatedAt: now}
	for _, model := range []any{&character, &scene, &prop} {
		if err := db.DB.Create(model).Error; err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		path string
		body string
	}{
		{"/api/v1/characters/" + itoa(character.ID), `{"name":12}`},
		{"/api/v1/scenes/" + itoa(scene.ID), `{"location":false}`},
		{"/api/v1/props/" + itoa(prop.ID), `{"name":[]}`},
		{"/api/v1/characters/" + itoa(character.ID), `{"name":"` + strings.Repeat("x", 201) + `"}`},
		{"/api/v1/scenes/" + itoa(scene.ID), `{"location":"` + strings.Repeat("x", 201) + `"}`},
		{"/api/v1/props/" + itoa(prop.ID), `{"name":"` + strings.Repeat("x", 201) + `"}`},
	}
	for _, test := range tests {
		got := performRequest(router, http.MethodPut, test.path, test.body, nil)
		if got.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d want 400; body=%s", test.path, got.Code, got.Body.String())
		}
	}
}

func TestStoryboardAndEpisodeRejectCoercedJSONTypes(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "typed", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "episode", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	imageConfig := models.AIServiceConfig{ServiceType: "image", Provider: "mock", Name: "image", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&imageConfig).Error; err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPut, "/api/v1/episodes/" + itoa(episode.ID), `{"image_config_id":"` + itoa(imageConfig.ID) + `"}`},
		{http.MethodPost, "/api/v1/storyboards", `{"episode_id":"` + itoa(episode.ID) + `","title":"shot"}`},
		{http.MethodPost, "/api/v1/storyboards", `{"episode_id":` + itoa(episode.ID) + `,"duration":1.5}`},
		{http.MethodPost, "/api/v1/storyboards", `{"episode_id":` + itoa(episode.ID) + `,"character_ids":["1"]}`},
	}
	for _, test := range tests {
		got := performRequest(router, test.method, test.path, test.body, nil)
		if got.Code != http.StatusBadRequest {
			t.Errorf("%s %s: status=%d want 400; body=%s", test.method, test.path, got.Code, got.Body.String())
		}
	}
}
