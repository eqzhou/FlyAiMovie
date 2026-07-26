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

func TestSoftDeletedResourcesRejectGeneration(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfigID := createMockConfig(t, router, "image")
	videoConfigID := createMockConfig(t, router, "video")
	audioConfigID := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"软删除保护","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	episode := requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"title":"第一集","image_config_id":`+itoa(imageConfigID)+`,"video_config_id":`+itoa(videoConfigID)+`,"audio_config_id":`+itoa(audioConfigID)+`}`)
	episodeID := uintField(t, episode, "id")

	character := requestData(t, router, http.MethodPost, "/api/v1/characters", `{"drama_id":`+itoa(dramaID)+`,"name":"阿宁","appearance":"白外套"}`)
	characterID := uintField(t, character, "id")
	scene := requestData(t, router, http.MethodPost, "/api/v1/scenes", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"location":"站台","time":"夜","prompt":"雨夜"}`)
	sceneID := uintField(t, scene, "id")
	prop := requestData(t, router, http.MethodPost, "/api/v1/props", `{"drama_id":`+itoa(dramaID)+`,"name":"旧提箱","prompt":"皮革"}`)
	propID := uintField(t, prop, "id")
	storyboard := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"title":"开场","image_prompt":"quiet room","video_prompt":"quiet room","dialogue":"阿宁：你好"}`)
	storyboardID := uintField(t, storyboard, "id")

	if resp := performRequest(router, http.MethodDelete, "/api/v1/characters/"+itoa(characterID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete character status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodDelete, "/api/v1/scenes/"+itoa(sceneID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete scene status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodDelete, "/api/v1/props/"+itoa(propID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete prop status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodDelete, "/api/v1/storyboards/"+itoa(storyboardID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete storyboard status=%d body=%s", resp.Code, resp.Body.String())
	}

	charGen := performRequest(router, http.MethodPost, "/api/v1/characters/"+itoa(characterID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`, nil)
	if charGen.Code != http.StatusBadRequest {
		t.Fatalf("deleted character generate status=%d body=%s", charGen.Code, charGen.Body.String())
	}
	sceneGen := performRequest(router, http.MethodPost, "/api/v1/scenes/"+itoa(sceneID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`, nil)
	if sceneGen.Code != http.StatusBadRequest {
		t.Fatalf("deleted scene generate status=%d body=%s", sceneGen.Code, sceneGen.Body.String())
	}
	propGen := performRequest(router, http.MethodPost, "/api/v1/props/"+itoa(propID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`, nil)
	if propGen.Code != http.StatusNotFound {
		t.Fatalf("deleted prop generate status=%d body=%s", propGen.Code, propGen.Body.String())
	}
	frameGen := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(storyboardID)+"/generate-frame", `{"frame_type":"first_frame"}`, nil)
	if frameGen.Code != http.StatusNotFound {
		t.Fatalf("deleted storyboard frame status=%d body=%s", frameGen.Code, frameGen.Body.String())
	}
	videoGen := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(storyboardID)+"/generate-video", `{}`, nil)
	if videoGen.Code != http.StatusNotFound {
		t.Fatalf("deleted storyboard video status=%d body=%s", videoGen.Code, videoGen.Body.String())
	}
	ttsGen := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(storyboardID)+"/generate-tts", `{}`, nil)
	if ttsGen.Code != http.StatusBadRequest {
		t.Fatalf("deleted storyboard tts status=%d body=%s", ttsGen.Code, ttsGen.Body.String())
	}
	batch := requestData(t, router, http.MethodPost, "/api/v1/characters/batch-generate-images", `{"episode_id":`+itoa(episodeID)+`,"character_ids":[`+itoa(characterID)+`]}`)
	if int(batch["count"].(float64)) != 0 {
		t.Fatalf("batch count=%v body=%v", batch["count"], batch)
	}
	errs, ok := batch["errors"].([]any)
	if !ok || len(errs) == 0 || !strings.Contains(fmt.Sprint(errs), "not found") {
		t.Fatalf("batch errors=%v", batch["errors"])
	}
}

func TestSoftDeletedResourcesRejectMutations(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfigID := createMockConfig(t, router, "image")
	videoConfigID := createMockConfig(t, router, "video")
	audioConfigID := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"软删除写入保护","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	episode := requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"title":"第一集","image_config_id":`+itoa(imageConfigID)+`,"video_config_id":`+itoa(videoConfigID)+`,"audio_config_id":`+itoa(audioConfigID)+`}`)
	episodeID := uintField(t, episode, "id")
	storyboard := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"title":"开场","image_prompt":"quiet room"}`)
	storyboardID := uintField(t, storyboard, "id")

	if resp := performRequest(router, http.MethodDelete, "/api/v1/storyboards/"+itoa(storyboardID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete storyboard status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPut, "/api/v1/storyboards/"+itoa(storyboardID), `{"title":"已删除仍写入"}`, nil); resp.Code != http.StatusNotFound && resp.Code != http.StatusBadRequest {
		t.Fatalf("update deleted storyboard status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPut, "/api/v1/episodes/"+itoa(episodeID), `{"content":"before delete"}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("update active episode status=%d body=%s", resp.Code, resp.Body.String())
	}

	// Soft-delete the episode via API and ensure further writes are rejected.
	if resp := performRequest(router, http.MethodDelete, "/api/v1/episodes/"+itoa(episodeID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete episode status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodDelete, "/api/v1/episodes/"+itoa(episodeID), "", nil); resp.Code != http.StatusNotFound {
		t.Fatalf("delete episode twice status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPut, "/api/v1/episodes/"+itoa(episodeID), `{"content":"after delete"}`, nil); resp.Code != http.StatusNotFound {
		t.Fatalf("update deleted episode status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"title":"新镜头"}`, nil); resp.Code != http.StatusBadRequest && resp.Code != http.StatusNotFound {
		t.Fatalf("create storyboard on deleted episode status=%d body=%s", resp.Code, resp.Body.String())
	}

	// Soft-delete the drama and ensure dependent creates reject it.
	if resp := performRequest(router, http.MethodDelete, "/api/v1/dramas/"+itoa(dramaID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete drama status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/characters", `{"drama_id":`+itoa(dramaID)+`,"name":"阿宁"}`, nil); resp.Code != http.StatusBadRequest {
		t.Fatalf("create character on deleted drama status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/scenes", `{"drama_id":`+itoa(dramaID)+`,"location":"站台","time":"夜"}`, nil); resp.Code != http.StatusBadRequest {
		t.Fatalf("create scene on deleted drama status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/props", `{"drama_id":`+itoa(dramaID)+`,"name":"旧提箱"}`, nil); resp.Code != http.StatusBadRequest {
		t.Fatalf("create prop on deleted drama status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestCopyEpisodeDuplicatesScriptScenesAndStoryboards(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfigID := createMockConfig(t, router, "image")
	videoConfigID := createMockConfig(t, router, "video")
	audioConfigID := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"剧集复制","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	detail := requestData(t, router, http.MethodGet, "/api/v1/dramas/"+itoa(dramaID), "")
	episodes, _ := detail["episodes"].([]any)
	if len(episodes) == 0 {
		t.Fatal("drama should auto-create at least one episode")
	}
	episode := episodes[0].(map[string]any)
	episodeID := uintField(t, episode, "id")
	if resp := performRequest(router, http.MethodPut, "/api/v1/episodes/"+itoa(episodeID), `{"title":"第一集","image_config_id":`+itoa(imageConfigID)+`,"video_config_id":`+itoa(videoConfigID)+`,"audio_config_id":`+itoa(audioConfigID)+`}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("seed episode configs status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPut, "/api/v1/episodes/"+itoa(episodeID), `{"content":"雨夜重逢","script_content":"## S01 | 内景 · 客厅 | 夜\n林舟：你终于来了。"}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("seed episode content status=%d body=%s", resp.Code, resp.Body.String())
	}
	character := requestData(t, router, http.MethodPost, "/api/v1/characters", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"name":"林舟","role":"男主"}`)
	characterID := uintField(t, character, "id")
	scene := requestData(t, router, http.MethodPost, "/api/v1/scenes", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"location":"客厅","time":"夜","prompt":"warm interior"}`)
	sceneID := uintField(t, scene, "id")
	storyboard := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"scene_id":`+itoa(sceneID)+`,"title":"开场","image_prompt":"quiet room","character_ids":[`+itoa(characterID)+`]}`)
	storyboardID := uintField(t, storyboard, "id")

	sourceNumber := int(episode["episode_number"].(float64))
	copied := requestData(t, router, http.MethodPost, "/api/v1/episodes/"+itoa(episodeID)+"/copy", `{}`)
	copiedID := uintField(t, copied, "id")
	if copiedID == episodeID {
		t.Fatal("copy returned same episode id")
	}
	copiedNumber := int(copied["episode_number"].(float64))
	if copiedNumber <= sourceNumber {
		t.Fatalf("episode_number=%v want > %d", copied["episode_number"], sourceNumber)
	}
	if title, _ := copied["title"].(string); !strings.Contains(title, "副本") {
		t.Fatalf("title=%q want 副本 suffix", title)
	}

	detail = requestData(t, router, http.MethodGet, "/api/v1/dramas/"+itoa(dramaID), "")
	episodes, _ = detail["episodes"].([]any)
	if len(episodes) != 2 {
		t.Fatalf("drama episodes=%d want 2", len(episodes))
	}

	charList := requestSlice(t, router, "/api/v1/episodes/"+itoa(copiedID)+"/characters")
	if len(charList) != 1 {
		t.Fatalf("copied characters=%d want 1 (%v)", len(charList), charList)
	}
	if uintField(t, charList[0], "id") != characterID {
		t.Fatalf("copied character should reuse original id")
	}

	sceneList := requestSlice(t, router, "/api/v1/episodes/"+itoa(copiedID)+"/scenes")
	if len(sceneList) != 1 {
		t.Fatalf("copied scenes=%d want 1", len(sceneList))
	}
	copiedSceneID := uintField(t, sceneList[0], "id")
	if copiedSceneID == sceneID {
		t.Fatal("owned scene should be duplicated, not shared by id")
	}

	shotList := requestSlice(t, router, "/api/v1/episodes/"+itoa(copiedID)+"/storyboards")
	if len(shotList) != 1 {
		t.Fatalf("copied storyboards=%d want 1", len(shotList))
	}
	shot := shotList[0]
	if uintField(t, shot, "id") == storyboardID {
		t.Fatal("storyboard should be duplicated")
	}
	if shot["title"] != "开场" || shot["image_prompt"] != "quiet room" {
		t.Fatalf("storyboard fields not copied: %+v", shot)
	}
	if videoURL, _ := shot["video_url"].(string); videoURL != "" {
		t.Fatalf("generated video_url should not copy: %v", videoURL)
	}
	cids, _ := shot["character_ids"].([]any)
	if len(cids) != 1 || uint(cids[0].(float64)) != characterID {
		t.Fatalf("character_ids=%v want [%d]", cids, characterID)
	}

	// Deleted episode cannot be copied.
	if resp := performRequest(router, http.MethodDelete, "/api/v1/episodes/"+itoa(episodeID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete source status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/episodes/"+itoa(episodeID)+"/copy", `{}`, nil); resp.Code != http.StatusNotFound {
		t.Fatalf("copy deleted episode status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func requestSlice(t *testing.T, router http.Handler, path string) []map[string]any {
	t.Helper()
	res := performRequest(router, http.MethodGet, path, "", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, res.Code, res.Body.String())
	}
	payload := decodeResponse(t, res)
	raw, ok := payload["data"].([]any)
	if !ok {
		t.Fatalf("GET %s: expected array data, got %#v", path, payload["data"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("GET %s: item is not object: %#v", path, item)
		}
		out = append(out, m)
	}
	return out
}

func TestMoveEpisodeReordersWithinDrama(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfigID := createMockConfig(t, router, "image")
	videoConfigID := createMockConfig(t, router, "video")
	audioConfigID := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"剧集重排","total_episodes":2}`)
	dramaID := uintField(t, drama, "id")
	detail := requestData(t, router, http.MethodGet, "/api/v1/dramas/"+itoa(dramaID), "")
	episodes, _ := detail["episodes"].([]any)
	if len(episodes) < 2 {
		// ensure second episode exists
		_ = requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"title":"第二集","image_config_id":`+itoa(imageConfigID)+`,"video_config_id":`+itoa(videoConfigID)+`,"audio_config_id":`+itoa(audioConfigID)+`}`)
		detail = requestData(t, router, http.MethodGet, "/api/v1/dramas/"+itoa(dramaID), "")
		episodes, _ = detail["episodes"].([]any)
	}
	if len(episodes) < 2 {
		t.Fatalf("need at least 2 episodes, got %d", len(episodes))
	}
	// normalize titles/configs for stability
	first := episodes[0].(map[string]any)
	second := episodes[1].(map[string]any)
	firstID := uintField(t, first, "id")
	secondID := uintField(t, second, "id")
	firstNum := int(first["episode_number"].(float64))
	secondNum := int(second["episode_number"].(float64))
	if firstNum >= secondNum {
		t.Fatalf("expected ordered episodes, got %d then %d", firstNum, secondNum)
	}
	for _, item := range []struct {
		id    uint
		title string
	}{{firstID, "第一集"}, {secondID, "第二集"}} {
		if resp := performRequest(router, http.MethodPut, "/api/v1/episodes/"+itoa(item.id), `{"title":"`+item.title+`","image_config_id":`+itoa(imageConfigID)+`,"video_config_id":`+itoa(videoConfigID)+`,"audio_config_id":`+itoa(audioConfigID)+`}`, nil); resp.Code != http.StatusOK {
			t.Fatalf("seed episode %d status=%d body=%s", item.id, resp.Code, resp.Body.String())
		}
	}

	// move second episode up -> should swap with first
	if resp := performRequest(router, http.MethodPost, "/api/v1/episodes/"+itoa(secondID)+"/move", `{"direction":"up"}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("move up status=%d body=%s", resp.Code, resp.Body.String())
	}
	detail = requestData(t, router, http.MethodGet, "/api/v1/dramas/"+itoa(dramaID), "")
	episodes, _ = detail["episodes"].([]any)
	if len(episodes) < 2 {
		t.Fatalf("episodes missing after move: %d", len(episodes))
	}
	byID := map[uint]int{}
	for _, raw := range episodes {
		ep := raw.(map[string]any)
		byID[uintField(t, ep, "id")] = int(ep["episode_number"].(float64))
	}
	if byID[secondID] != firstNum || byID[firstID] != secondNum {
		t.Fatalf("after move up: first=%d second=%d want first=%d second=%d", byID[firstID], byID[secondID], secondNum, firstNum)
	}

	// move back down
	if resp := performRequest(router, http.MethodPost, "/api/v1/episodes/"+itoa(secondID)+"/move", `{"direction":"down"}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("move down status=%d body=%s", resp.Code, resp.Body.String())
	}
	detail = requestData(t, router, http.MethodGet, "/api/v1/dramas/"+itoa(dramaID), "")
	episodes, _ = detail["episodes"].([]any)
	byID = map[uint]int{}
	for _, raw := range episodes {
		ep := raw.(map[string]any)
		byID[uintField(t, ep, "id")] = int(ep["episode_number"].(float64))
	}
	if byID[firstID] != firstNum || byID[secondID] != secondNum {
		t.Fatalf("after move down: first=%d second=%d want first=%d second=%d", byID[firstID], byID[secondID], firstNum, secondNum)
	}

	// boundary: first episode cannot move up
	if resp := performRequest(router, http.MethodPost, "/api/v1/episodes/"+itoa(firstID)+"/move", `{"direction":"up"}`, nil); resp.Code != http.StatusBadRequest {
		t.Fatalf("boundary up status=%d body=%s", resp.Code, resp.Body.String())
	}
	// invalid direction
	if resp := performRequest(router, http.MethodPost, "/api/v1/episodes/"+itoa(firstID)+"/move", `{"direction":"sideways"}`, nil); resp.Code != http.StatusBadRequest {
		t.Fatalf("invalid direction status=%d body=%s", resp.Code, resp.Body.String())
	}
	// deleted episode cannot move
	if resp := performRequest(router, http.MethodDelete, "/api/v1/episodes/"+itoa(secondID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete episode status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/episodes/"+itoa(secondID)+"/move", `{"direction":"up"}`, nil); resp.Code != http.StatusNotFound {
		t.Fatalf("move deleted status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestCopyStoryboardDuplicatesContentAndLinks(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfigID := createMockConfig(t, router, "image")
	videoConfigID := createMockConfig(t, router, "video")
	audioConfigID := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"分镜复制","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	detail := requestData(t, router, http.MethodGet, "/api/v1/dramas/"+itoa(dramaID), "")
	episodes, _ := detail["episodes"].([]any)
	if len(episodes) == 0 {
		t.Fatal("drama should auto-create at least one episode")
	}
	episode := episodes[0].(map[string]any)
	episodeID := uintField(t, episode, "id")
	if resp := performRequest(router, http.MethodPut, "/api/v1/episodes/"+itoa(episodeID), `{"title":"第一集","image_config_id":`+itoa(imageConfigID)+`,"video_config_id":`+itoa(videoConfigID)+`,"audio_config_id":`+itoa(audioConfigID)+`}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("seed episode configs status=%d body=%s", resp.Code, resp.Body.String())
	}
	character := requestData(t, router, http.MethodPost, "/api/v1/characters", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"name":"林舟","role":"男主"}`)
	characterID := uintField(t, character, "id")
	scene := requestData(t, router, http.MethodPost, "/api/v1/scenes", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"location":"客厅","time":"夜","prompt":"warm interior"}`)
	sceneID := uintField(t, scene, "id")
	storyboard := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"scene_id":`+itoa(sceneID)+`,"title":"开场","image_prompt":"quiet room","video_prompt":"slow push","dialogue":"林舟：你来了","duration":8,"character_ids":[`+itoa(characterID)+`]}`)
	storyboardID := uintField(t, storyboard, "id")
	if resp := performRequest(router, http.MethodPut, "/api/v1/storyboards/"+itoa(storyboardID), `{"first_frame_image":"/static/owned-frame.png","video_url":"/static/owned-video.mp4"}`, nil); resp.Code != http.StatusOK && resp.Code != http.StatusBadRequest {
		// media ownership may reject synthetic static paths; seed via DB-free path is optional
		t.Logf("optional media seed skipped status=%d body=%s", resp.Code, resp.Body.String())
	}

	copied := requestData(t, router, http.MethodPost, "/api/v1/storyboards/"+itoa(storyboardID)+"/copy", `{}`)
	copiedID := uintField(t, copied, "id")
	if copiedID == storyboardID {
		t.Fatal("copy returned same storyboard id")
	}
	if title, _ := copied["title"].(string); !strings.Contains(title, "副本") {
		t.Fatalf("title=%q want 副本 suffix", title)
	}
	if int(copied["storyboard_number"].(float64)) != 2 {
		t.Fatalf("storyboard_number=%v want 2", copied["storyboard_number"])
	}
	if copied["image_prompt"] != "quiet room" || copied["dialogue"] != "林舟：你来了" {
		t.Fatalf("content not copied: %+v", copied)
	}
	if videoURL, _ := copied["video_url"].(string); videoURL != "" {
		t.Fatalf("generated video_url should not copy: %v", videoURL)
	}
	if firstFrame, _ := copied["first_frame_image"].(string); firstFrame != "" {
		t.Fatalf("generated first_frame_image should not copy: %v", firstFrame)
	}

	list := requestSlice(t, router, "/api/v1/episodes/"+itoa(episodeID)+"/storyboards")
	if len(list) != 2 {
		t.Fatalf("storyboards=%d want 2", len(list))
	}
	var found map[string]any
	for _, item := range list {
		if uintField(t, item, "id") == copiedID {
			found = item
			break
		}
	}
	if found == nil {
		t.Fatal("copied storyboard missing from episode list")
	}
	cids, _ := found["character_ids"].([]any)
	if len(cids) != 1 || uint(cids[0].(float64)) != characterID {
		t.Fatalf("character_ids=%v want [%d]", cids, characterID)
	}
	if scene, _ := found["scene_id"].(float64); uint(scene) != sceneID {
		t.Fatalf("scene_id=%v want %d", found["scene_id"], sceneID)
	}

	// deleted storyboard cannot be copied
	if resp := performRequest(router, http.MethodDelete, "/api/v1/storyboards/"+itoa(storyboardID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete source status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(storyboardID)+"/copy", `{}`, nil); resp.Code != http.StatusNotFound {
		t.Fatalf("copy deleted storyboard status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestMoveStoryboardReordersWithinEpisode(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfigID := createMockConfig(t, router, "image")
	videoConfigID := createMockConfig(t, router, "video")
	audioConfigID := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"分镜重排","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	detail := requestData(t, router, http.MethodGet, "/api/v1/dramas/"+itoa(dramaID), "")
	episodes, _ := detail["episodes"].([]any)
	if len(episodes) == 0 {
		t.Fatal("drama should auto-create at least one episode")
	}
	episodeID := uintField(t, episodes[0].(map[string]any), "id")
	if resp := performRequest(router, http.MethodPut, "/api/v1/episodes/"+itoa(episodeID), `{"title":"第一集","image_config_id":`+itoa(imageConfigID)+`,"video_config_id":`+itoa(videoConfigID)+`,"audio_config_id":`+itoa(audioConfigID)+`}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("seed episode configs status=%d body=%s", resp.Code, resp.Body.String())
	}
	first := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"title":"镜头A","image_prompt":"a"}`)
	second := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"title":"镜头B","image_prompt":"b"}`)
	firstID := uintField(t, first, "id")
	secondID := uintField(t, second, "id")
	firstNum := int(first["storyboard_number"].(float64))
	secondNum := int(second["storyboard_number"].(float64))
	if firstNum >= secondNum {
		t.Fatalf("expected ordered storyboards, got %d then %d", firstNum, secondNum)
	}

	if resp := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(secondID)+"/move", `{"direction":"up"}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("move up status=%d body=%s", resp.Code, resp.Body.String())
	}
	list := requestSlice(t, router, "/api/v1/episodes/"+itoa(episodeID)+"/storyboards")
	byID := map[uint]int{}
	for _, item := range list {
		byID[uintField(t, item, "id")] = int(item["storyboard_number"].(float64))
	}
	if byID[secondID] != firstNum || byID[firstID] != secondNum {
		t.Fatalf("after move up: first=%d second=%d want first=%d second=%d", byID[firstID], byID[secondID], secondNum, firstNum)
	}

	if resp := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(secondID)+"/move", `{"direction":"down"}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("move down status=%d body=%s", resp.Code, resp.Body.String())
	}
	list = requestSlice(t, router, "/api/v1/episodes/"+itoa(episodeID)+"/storyboards")
	byID = map[uint]int{}
	for _, item := range list {
		byID[uintField(t, item, "id")] = int(item["storyboard_number"].(float64))
	}
	if byID[firstID] != firstNum || byID[secondID] != secondNum {
		t.Fatalf("after move down: first=%d second=%d want first=%d second=%d", byID[firstID], byID[secondID], firstNum, secondNum)
	}

	if resp := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(firstID)+"/move", `{"direction":"up"}`, nil); resp.Code != http.StatusBadRequest {
		t.Fatalf("boundary up status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(firstID)+"/move", `{"direction":"sideways"}`, nil); resp.Code != http.StatusBadRequest {
		t.Fatalf("invalid direction status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodDelete, "/api/v1/storyboards/"+itoa(secondID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete storyboard status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(secondID)+"/move", `{"direction":"up"}`, nil); resp.Code != http.StatusNotFound {
		t.Fatalf("move deleted status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestDeleteEpisodeCascadesSoftDeleteToOwnedDependents(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfigID := createMockConfig(t, router, "image")
	videoConfigID := createMockConfig(t, router, "video")
	audioConfigID := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"剧集级联删除","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	detail := requestData(t, router, http.MethodGet, "/api/v1/dramas/"+itoa(dramaID), "")
	episodes, _ := detail["episodes"].([]any)
	if len(episodes) == 0 {
		t.Fatal("drama should auto-create at least one episode")
	}
	episodeID := uintField(t, episodes[0].(map[string]any), "id")
	if resp := performRequest(router, http.MethodPut, "/api/v1/episodes/"+itoa(episodeID), `{"title":"第一集","image_config_id":`+itoa(imageConfigID)+`,"video_config_id":`+itoa(videoConfigID)+`,"audio_config_id":`+itoa(audioConfigID)+`}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("seed episode configs status=%d body=%s", resp.Code, resp.Body.String())
	}

	// Shared drama-level scene should remain after episode delete.
	sharedScene := requestData(t, router, http.MethodPost, "/api/v1/scenes", `{"drama_id":`+itoa(dramaID)+`,"location":"共享场景","time":"日","prompt":"shared"}`)
	sharedSceneID := uintField(t, sharedScene, "id")

	ownedScene := requestData(t, router, http.MethodPost, "/api/v1/scenes", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"location":"本集场景","time":"夜","prompt":"owned"}`)
	ownedSceneID := uintField(t, ownedScene, "id")
	storyboard := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"scene_id":`+itoa(ownedSceneID)+`,"title":"本集镜头","image_prompt":"quiet room"}`)
	storyboardID := uintField(t, storyboard, "id")

	run := requestData(t, router, http.MethodPost, "/api/v1/productions", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`}`)
	runID := uintField(t, run, "id")

	if resp := performRequest(router, http.MethodDelete, "/api/v1/episodes/"+itoa(episodeID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete episode status=%d body=%s", resp.Code, resp.Body.String())
	}

	// Owned dependents must become unavailable for mutation/generation.
	if resp := performRequest(router, http.MethodPut, "/api/v1/storyboards/"+itoa(storyboardID), `{"title":"删除后仍改"}`, nil); resp.Code != http.StatusNotFound && resp.Code != http.StatusBadRequest {
		t.Fatalf("update cascaded storyboard status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(storyboardID)+"/generate-frame", `{"frame_type":"first_frame"}`, nil); resp.Code != http.StatusNotFound && resp.Code != http.StatusBadRequest {
		t.Fatalf("generate cascaded storyboard status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPut, "/api/v1/scenes/"+itoa(ownedSceneID), `{"location":"删除后仍改"}`, nil); resp.Code != http.StatusNotFound && resp.Code != http.StatusBadRequest {
		t.Fatalf("update cascaded owned scene status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/scenes/"+itoa(ownedSceneID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`, nil); resp.Code != http.StatusNotFound && resp.Code != http.StatusBadRequest {
		t.Fatalf("generate cascaded owned scene status=%d body=%s", resp.Code, resp.Body.String())
	}

	// Shared drama scene remains editable.
	if resp := performRequest(router, http.MethodPut, "/api/v1/scenes/"+itoa(sharedSceneID), `{"location":"共享仍可改"}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("update shared scene status=%d body=%s", resp.Code, resp.Body.String())
	}

	// Active production run for the deleted episode should be canceled.
	runDetail := requestData(t, router, http.MethodGet, "/api/v1/productions/"+itoa(runID), "")
	if status, _ := runDetail["status"].(string); status != "canceled" {
		t.Fatalf("production status=%q want canceled body=%v", status, runDetail)
	}
}

func TestDeleteDramaCascadesSoftDeleteToOwnedDependents(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfigID := createMockConfig(t, router, "image")
	videoConfigID := createMockConfig(t, router, "video")
	audioConfigID := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"项目级联删除","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	detail := requestData(t, router, http.MethodGet, "/api/v1/dramas/"+itoa(dramaID), "")
	episodes, _ := detail["episodes"].([]any)
	if len(episodes) == 0 {
		t.Fatal("drama should auto-create at least one episode")
	}
	episodeID := uintField(t, episodes[0].(map[string]any), "id")
	if resp := performRequest(router, http.MethodPut, "/api/v1/episodes/"+itoa(episodeID), `{"title":"第一集","image_config_id":`+itoa(imageConfigID)+`,"video_config_id":`+itoa(videoConfigID)+`,"audio_config_id":`+itoa(audioConfigID)+`}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("seed episode configs status=%d body=%s", resp.Code, resp.Body.String())
	}

	character := requestData(t, router, http.MethodPost, "/api/v1/characters", `{"drama_id":`+itoa(dramaID)+`,"name":"林舟","role":"男主"}`)
	characterID := uintField(t, character, "id")
	scene := requestData(t, router, http.MethodPost, "/api/v1/scenes", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"location":"客厅","time":"夜","prompt":"owned"}`)
	sceneID := uintField(t, scene, "id")
	prop := requestData(t, router, http.MethodPost, "/api/v1/props", `{"drama_id":`+itoa(dramaID)+`,"name":"旧提箱","prompt":"leather"}`)
	propID := uintField(t, prop, "id")
	storyboard := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"scene_id":`+itoa(sceneID)+`,"title":"开场","image_prompt":"quiet room"}`)
	storyboardID := uintField(t, storyboard, "id")
	run := requestData(t, router, http.MethodPost, "/api/v1/productions", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`}`)
	runID := uintField(t, run, "id")

	if resp := performRequest(router, http.MethodDelete, "/api/v1/dramas/"+itoa(dramaID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete drama status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodDelete, "/api/v1/dramas/"+itoa(dramaID), "", nil); resp.Code != http.StatusNotFound {
		t.Fatalf("delete drama twice status=%d body=%s", resp.Code, resp.Body.String())
	}

	// Drama dependents must become unavailable for mutation/generation.
	if resp := performRequest(router, http.MethodPut, "/api/v1/episodes/"+itoa(episodeID), `{"content":"deleted drama"}`, nil); resp.Code != http.StatusNotFound && resp.Code != http.StatusBadRequest {
		t.Fatalf("update cascaded episode status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPut, "/api/v1/characters/"+itoa(characterID), `{"name":"删除后仍改"}`, nil); resp.Code != http.StatusNotFound && resp.Code != http.StatusBadRequest {
		t.Fatalf("update cascaded character status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPut, "/api/v1/scenes/"+itoa(sceneID), `{"location":"删除后仍改"}`, nil); resp.Code != http.StatusNotFound && resp.Code != http.StatusBadRequest {
		t.Fatalf("update cascaded scene status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPut, "/api/v1/props/"+itoa(propID), `{"name":"删除后仍改"}`, nil); resp.Code != http.StatusNotFound && resp.Code != http.StatusBadRequest {
		t.Fatalf("update cascaded prop status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPut, "/api/v1/storyboards/"+itoa(storyboardID), `{"title":"删除后仍改"}`, nil); resp.Code != http.StatusNotFound && resp.Code != http.StatusBadRequest {
		t.Fatalf("update cascaded storyboard status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/storyboards/"+itoa(storyboardID)+"/generate-frame", `{"frame_type":"first_frame"}`, nil); resp.Code != http.StatusNotFound && resp.Code != http.StatusBadRequest {
		t.Fatalf("generate cascaded storyboard status=%d body=%s", resp.Code, resp.Body.String())
	}

	// Active production run for the deleted drama should be canceled.
	runDetail := requestData(t, router, http.MethodGet, "/api/v1/productions/"+itoa(runID), "")
	if status, _ := runDetail["status"].(string); status != "canceled" {
		t.Fatalf("production status=%q want canceled body=%v", status, runDetail)
	}
}
