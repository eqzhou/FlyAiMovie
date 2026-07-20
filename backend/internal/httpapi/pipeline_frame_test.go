package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
)

func TestStoryboardFrameGenerationValidatesAndPersistsComposedFrame(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfigID := createMockConfig(t, router, "image")
	videoConfigID := createMockConfig(t, router, "video")
	audioConfigID := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"frame modes","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	episode := requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"title":"第一集","image_config_id":`+itoa(imageConfigID)+`,"video_config_id":`+itoa(videoConfigID)+`,"audio_config_id":`+itoa(audioConfigID)+`}`)
	episodeID := uintField(t, episode, "id")
	storyboard := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"title":"开场","image_prompt":"quiet room"}`)
	storyboardID := uintField(t, storyboard, "id")

	invalid := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(storyboardID)+"/generate-frame", `{"frame_type":"typo","config_id":`+itoa(imageConfigID)+`}`, nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid frame type status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	generated := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(storyboardID)+"/generate-frame", `{"frame_type":"composed","config_id":`+itoa(imageConfigID)+`}`, nil)
	if generated.Code != http.StatusOK {
		t.Fatalf("composed frame status=%d body=%s", generated.Code, generated.Body.String())
	}
	var stored models.Storyboard
	if err := db.DB.First(&stored, storyboardID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ComposedImage == "" {
		t.Fatal("composed frame was not persisted")
	}
}

func TestBatchFrameGenerationRejectsInvalidFrameType(t *testing.T) {
	router := testRouter(t)
	empty := performRequest(router, http.MethodPost, "/api/v1/storyboards/batch-generate-frames", `{}`, nil)
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty selection status=%d body=%s", empty.Code, empty.Body.String())
	}
	response := performRequest(router, http.MethodPost, "/api/v1/storyboards/batch-generate-frames", `{"episode_id":1,"frame_type":"typo"}`, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBatchGenerationRejectsStoryboardsFromAnotherEpisode(t *testing.T) {
	router := testRouter(t)
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"batch ownership","total_episodes":2}`)
	dramaID := uintField(t, drama, "id")
	detail := requestData(t, router, http.MethodGet, "/api/v1/dramas/"+itoa(dramaID), "")
	episodes := detail["episodes"].([]any)
	firstEpisodeID := uint(episodes[0].(map[string]any)["id"].(float64))
	secondEpisodeID := uint(episodes[1].(map[string]any)["id"].(float64))
	foreign := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(secondEpisodeID)+`,"title":"foreign","image_prompt":"room","video_prompt":"room","dialogue":"阿宁：你好"}`)
	foreignID := uintField(t, foreign, "id")

	requests := []struct {
		path string
		body string
	}{
		{"/api/v1/storyboards/batch-generate-frames", `{"episode_id":` + itoa(firstEpisodeID) + `,"storyboard_ids":[` + itoa(foreignID) + `],"frame_type":"first_frame"}`},
		{"/api/v1/storyboards/batch-generate-videos", `{"episode_id":` + itoa(firstEpisodeID) + `,"storyboard_ids":[` + itoa(foreignID) + `]}`},
		{"/api/v1/storyboards/batch-generate-tts", `{"episode_id":` + itoa(firstEpisodeID) + `,"storyboard_ids":[` + itoa(foreignID) + `]}`},
	}
	for _, request := range requests {
		response := performRequest(router, http.MethodPost, request.path, request.body, nil)
		if response.Code != http.StatusConflict {
			t.Fatalf("%s status=%d body=%s", request.path, response.Code, response.Body.String())
		}
	}
}

func TestStoryboardFrameUsesOrganizationPromptTemplate(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfigID := createMockConfig(t, router, "image")
	videoConfigID := createMockConfig(t, router, "video")
	audioConfigID := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"模板短剧","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	episode := requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"title":"第一集","image_config_id":`+itoa(imageConfigID)+`,"video_config_id":`+itoa(videoConfigID)+`,"audio_config_id":`+itoa(audioConfigID)+`}`)
	episodeID := uintField(t, episode, "id")
	storyboard := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"title":"重逢","description":"两人在雨中站台相遇","image_prompt":"quiet rain","video_prompt":"slow push in"}`)
	storyboardID := uintField(t, storyboard, "id")

	upsertPromptTemplate(t, "storyboard_image", "image", "{{drama_title}} :: {{shot_title}} :: {{shot_description}}", `["drama_title","shot_title","shot_description"]`)
	upsertPromptTemplate(t, "grid_composition", "grid", "{{drama_title}} grid {{grid_rows}}x{{grid_cols}} {{user_instruction}}", `["drama_title","grid_rows","grid_cols","user_instruction"]`)

	generated := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(storyboardID)+"/generate-frame", `{"frame_type":"first_frame","config_id":`+itoa(imageConfigID)+`}`, nil)
	if generated.Code != http.StatusOK {
		t.Fatalf("frame status=%d body=%s", generated.Code, generated.Body.String())
	}
	var rec models.ImageGeneration
	if err := db.DB.Where("storyboard_id = ?", storyboardID).Order("id desc").First(&rec).Error; err != nil {
		t.Fatal(err)
	}
	if rec.Prompt != "opening frame, 模板短剧 :: 重逢 :: 两人在雨中站台相遇" {
		t.Fatalf("prompt=%q", rec.Prompt)
	}

	grid := performRequest(router, http.MethodPost, "/api/v1/grid/prompt", `{"episode_id":`+itoa(episodeID)+`,"rows":1,"cols":1,"mode":"first_frame"}`, nil)
	if grid.Code != http.StatusOK {
		t.Fatalf("grid prompt status=%d body=%s", grid.Code, grid.Body.String())
	}
	if !strings.Contains(grid.Body.String(), "模板短剧 grid 1x1") {
		t.Fatalf("grid body=%s", grid.Body.String())
	}
}

func upsertPromptTemplate(t *testing.T, key, category, content, variablesJSON string) {
	t.Helper()
	var template models.PromptTemplate
	err := db.DB.Where("organization_id = ? AND key = ? AND deleted_at IS NULL", 0, key).First(&template).Error
	if err == nil {
		if err := db.DB.Model(&template).Updates(map[string]any{
			"category": category, "content": content, "variables_json": variablesJSON,
			"version": template.Version + 1, "is_active": true, "updated_at": "now",
		}).Error; err != nil {
			t.Fatal(err)
		}
		return
	}
	template = models.PromptTemplate{
		OrganizationID: 0, Key: key, Name: key, Category: category, Content: content, VariablesJSON: variablesJSON,
		Version: 1, IsActive: true, CreatedAt: "now", UpdatedAt: "now",
	}
	if err := db.DB.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
}

func TestAssetImageGenerationUsesOrganizationPromptTemplate(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfigID := createMockConfig(t, router, "image")
	videoConfigID := createMockConfig(t, router, "video")
	audioConfigID := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"资产模板短剧","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	episode := requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"title":"第一集","image_config_id":`+itoa(imageConfigID)+`,"video_config_id":`+itoa(videoConfigID)+`,"audio_config_id":`+itoa(audioConfigID)+`}`)
	episodeID := uintField(t, episode, "id")

	upsertPromptTemplate(t, "character_image", "image", "CHAR::{{character_name}}::{{character_appearance}}", `["character_name","character_appearance"]`)
	upsertPromptTemplate(t, "scene_image", "image", "SCENE::{{scene_location}}::{{scene_prompt}}", `["scene_location","scene_prompt"]`)
	upsertPromptTemplate(t, "prop_image", "image", "PROP::{{prop_name}}::{{prop_prompt}}", `["prop_name","prop_prompt"]`)

	character := requestData(t, router, http.MethodPost, "/api/v1/characters", `{"drama_id":`+itoa(dramaID)+`,"name":"阿宁","appearance":"白外套"}`)
	characterID := uintField(t, character, "id")
	scene := requestData(t, router, http.MethodPost, "/api/v1/scenes", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"location":"站台","time":"夜","prompt":"雨夜"}`)
	sceneID := uintField(t, scene, "id")
	prop := requestData(t, router, http.MethodPost, "/api/v1/props", `{"drama_id":`+itoa(dramaID)+`,"name":"旧提箱","prompt":"皮革"}`)
	propID := uintField(t, prop, "id")

	charGen := requestData(t, router, http.MethodPost, "/api/v1/characters/"+itoa(characterID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`)
	sceneGen := requestData(t, router, http.MethodPost, "/api/v1/scenes/"+itoa(sceneID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`)
	propGen := requestData(t, router, http.MethodPost, "/api/v1/props/"+itoa(propID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`)

	assertImagePrompt := func(id uint, want string) {
		t.Helper()
		var image models.ImageGeneration
		if err := db.DB.First(&image, id).Error; err != nil {
			t.Fatalf("load image %d: %v body char=%v scene=%v prop=%v", id, err, charGen, sceneGen, propGen)
		}
		if image.Prompt != want {
			t.Fatalf("prompt=%q want=%q", image.Prompt, want)
		}
	}
	assertImagePrompt(uintField(t, charGen, "image_generation_id"), "CHAR::阿宁::白外套")
	assertImagePrompt(uintField(t, sceneGen, "image_generation_id"), "SCENE::站台::雨夜")
	assertImagePrompt(uintField(t, propGen, "image_generation_id"), "PROP::旧提箱::皮革")
}

func TestCharacterBatchImagesSurfacesPerItemErrors(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfigID := createMockConfig(t, router, "image")
	videoConfigID := createMockConfig(t, router, "video")
	audioConfigID := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"批量角色图","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	episode := requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"title":"第一集","image_config_id":`+itoa(imageConfigID)+`,"video_config_id":`+itoa(videoConfigID)+`,"audio_config_id":`+itoa(audioConfigID)+`}`)
	episodeID := uintField(t, episode, "id")
	ready := requestData(t, router, http.MethodPost, "/api/v1/characters", `{"drama_id":`+itoa(dramaID)+`,"name":"阿宁","appearance":"白外套"}`)
	blank := requestData(t, router, http.MethodPost, "/api/v1/characters", `{"drama_id":`+itoa(dramaID)+`,"name":"空白角色","appearance":"x"}`)
	blankID := uintField(t, blank, "id")
	if err := db.DB.Model(&models.Character{}).Where("id = ?", blankID).Updates(map[string]any{"name": " ", "appearance": "", "description": "", "updated_at": "now"}).Error; err != nil {
		t.Fatal(err)
	}
	result := requestData(t, router, http.MethodPost, "/api/v1/characters/batch-generate-images", `{"episode_id":`+itoa(episodeID)+`,"character_ids":[`+itoa(uintField(t, ready, "id"))+`,`+itoa(blankID)+`,99999]}`)
	if int(result["count"].(float64)) != 1 {
		t.Fatalf("count=%v body=%v", result["count"], result)
	}
	errs, ok := result["errors"].([]any)
	if !ok || len(errs) < 2 {
		t.Fatalf("errors=%v body=%v", result["errors"], result)
	}
	joined := fmt.Sprint(errs)
	if !strings.Contains(joined, "empty prompt") || !strings.Contains(joined, "not found") {
		t.Fatalf("unexpected errors %s", joined)
	}
}
