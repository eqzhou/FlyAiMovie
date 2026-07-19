package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestManagementAndProductionReadSurfaces(t *testing.T) {
	server, router := testServerRouter(t)
	now := response.Now()
	textConfig := models.AIServiceConfig{ServiceType: "text", Provider: "mock", Name: "text", BaseURL: "http://localhost", Model: "mock", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	imageConfig := models.AIServiceConfig{ServiceType: "image", Provider: "mock", Name: "image", IsActive: true, CreatedAt: now, UpdatedAt: now}
	videoConfig := models.AIServiceConfig{ServiceType: "video", Provider: "mock", Name: "video", IsActive: true, CreatedAt: now, UpdatedAt: now}
	audioConfig := models.AIServiceConfig{ServiceType: "audio", Provider: "mock", Name: "audio", IsActive: true, CreatedAt: now, UpdatedAt: now}
	provider := models.AIServiceProvider{Name: "mock-image", DisplayName: "Mock image", ServiceType: "image", Provider: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&textConfig, &imageConfig, &videoConfig, &audioConfig, &provider} {
		if err := db.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	createdDrama := mustRequestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"Surface Drama","total_episodes":2,"tags":["test"]}`, http.StatusCreated)
	dramaID := uint(createdDrama["id"].(float64))
	var episodes []models.Episode
	if err := db.DB.Where("drama_id = ?", dramaID).Order("episode_number").Find(&episodes).Error; err != nil || len(episodes) != 2 {
		t.Fatalf("episodes=%+v err=%v", episodes, err)
	}
	for index := range episodes {
		if err := db.DB.Model(&episodes[index]).Updates(map[string]any{
			"image_config_id": imageConfig.ID, "video_config_id": videoConfig.ID, "audio_config_id": audioConfig.ID,
			"content": "source", "script_content": "## S01 | 内景 · Room | Day",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	episode := episodes[0]
	if err := db.DB.First(&episode, episode.ID).Error; err != nil {
		t.Fatal(err)
	}

	character := models.Character{DramaID: dramaID, Name: "Lin", CreatedAt: now, UpdatedAt: now}
	scene := models.Scene{DramaID: dramaID, EpisodeID: &episode.ID, Location: "Room", Time: "Day", Prompt: "room", CreatedAt: now, UpdatedAt: now}
	prop := models.Prop{DramaID: dramaID, Name: "Key", Prompt: "key", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&character, &scene, &prop} {
		if err := db.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	storyboard := models.Storyboard{EpisodeID: episode.ID, SceneID: &scene.ID, StoryboardNumber: 1, Title: "Shot", ImagePrompt: "frame", VideoPrompt: "motion", Dialogue: "Lin: go", Duration: 12, FirstFrameImage: "/static/frame.png", VideoURL: "/static/video.mp4", TTSAudioURL: "/static/voice.mp3", ComposedVideoURL: "/static/composed.mp4", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&models.EpisodeCharacter{EpisodeID: episode.ID, CharacterID: character.ID, CreatedAt: now},
		&models.EpisodeScene{EpisodeID: episode.ID, SceneID: scene.ID, CreatedAt: now},
		&models.StoryboardCharacter{StoryboardID: storyboard.ID, CharacterID: character.ID},
	} {
		if err := db.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	image := models.ImageGeneration{DramaID: &dramaID, StoryboardID: &storyboard.ID, Prompt: "frame", Status: "completed", ImageURL: "/static/frame.png", CreatedAt: now, UpdatedAt: now}
	video := models.VideoGeneration{DramaID: &dramaID, StoryboardID: &storyboard.ID, Prompt: "motion", Status: "completed", VideoURL: "/static/video.mp4", CreatedAt: now, UpdatedAt: now}
	history := models.GridHistory{DramaID: &dramaID, EpisodeID: &episode.ID, Mode: "first_frame", Rows: 1, Cols: 1, Prompt: "grid", Status: "completed", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&image, &video, &history} {
		if err := db.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	paths := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodGet, "/api/v1/health", "", http.StatusOK},
		{http.MethodGet, "/api/v1/dramas?status=draft&keyword=Surface", "", http.StatusOK},
		{http.MethodGet, "/api/v1/dramas/stats", "", http.StatusOK},
		{http.MethodGet, "/api/v1/dramas/" + idText(dramaID), "", http.StatusOK},
		{http.MethodPut, "/api/v1/dramas/" + idText(dramaID), `{"description":"updated","tags":["one","two"]}`, http.StatusOK},
		{http.MethodGet, "/api/v1/episodes/" + idText(episode.ID) + "/characters", "", http.StatusOK},
		{http.MethodGet, "/api/v1/episodes/" + idText(episode.ID) + "/scenes", "", http.StatusOK},
		{http.MethodGet, "/api/v1/episodes/" + idText(episode.ID) + "/storyboards", "", http.StatusOK},
		{http.MethodGet, "/api/v1/episodes/" + idText(episode.ID) + "/pipeline-status", "", http.StatusOK},
		{http.MethodGet, "/api/v1/images?drama_id=" + idText(dramaID) + "&status=completed", "", http.StatusOK},
		{http.MethodGet, "/api/v1/images/" + idText(image.ID), "", http.StatusOK},
		{http.MethodPost, "/api/v1/images", `{"prompt":"new frame","drama_id":` + idText(dramaID) + `,"episode_id":` + idText(episode.ID) + `,"config_id":` + idText(imageConfig.ID) + `}`, http.StatusOK},
		{http.MethodGet, "/api/v1/videos?drama_id=" + idText(dramaID) + "&status=completed", "", http.StatusOK},
		{http.MethodGet, "/api/v1/videos/" + idText(video.ID), "", http.StatusOK},
		{http.MethodPost, "/api/v1/videos", `{"prompt":"new motion","drama_id":` + idText(dramaID) + `,"episode_id":` + idText(episode.ID) + `,"config_id":` + idText(videoConfig.ID) + `}`, http.StatusOK},
		{http.MethodPost, "/api/v1/characters/batch-generate-images", `{"character_ids":[` + idText(character.ID) + `],"episode_id":` + idText(episode.ID) + `}`, http.StatusOK},
		{http.MethodPost, "/api/v1/characters/" + idText(character.ID) + "/generate-image", `{"episode_id":` + idText(episode.ID) + `}`, http.StatusOK},
		{http.MethodPost, "/api/v1/scenes/" + idText(scene.ID) + "/generate-image", `{"episode_id":` + idText(episode.ID) + `}`, http.StatusOK},
		{http.MethodPost, "/api/v1/storyboards/" + idText(storyboard.ID) + "/generate-frame", `{"frame_type":"composed","config_id":` + idText(imageConfig.ID) + `}`, http.StatusOK},
		{http.MethodPost, "/api/v1/storyboards/batch-generate-frames", `{"episode_id":` + idText(episode.ID) + `,"frame_type":"last_frame","config_id":` + idText(imageConfig.ID) + `}`, http.StatusOK},
		{http.MethodPost, "/api/v1/storyboards/batch-generate-videos", `{"episode_id":` + idText(episode.ID) + `,"config_id":` + idText(videoConfig.ID) + `}`, http.StatusOK},
		{http.MethodPost, "/api/v1/storyboards/" + idText(storyboard.ID) + "/generate-tts", `{}`, http.StatusOK},
		{http.MethodGet, "/api/v1/grid/history?episode_id=" + idText(episode.ID), "", http.StatusOK},
		{http.MethodGet, "/api/v1/grid/history/" + idText(history.ID), "", http.StatusOK},
		{http.MethodGet, "/api/v1/grid/status/" + idText(image.ID), "", http.StatusOK},
		{http.MethodPost, "/api/v1/grid/prompt", `{"rows":1,"cols":1,"mode":"first_frame","drama_id":` + idText(dramaID) + `,"episode_id":` + idText(episode.ID) + `}`, http.StatusOK},
		{http.MethodPost, "/api/v1/grid/generate", `{"prompt":"grid frame","rows":1,"cols":1,"drama_id":` + idText(dramaID) + `,"episode_id":` + idText(episode.ID) + `,"config_id":` + idText(imageConfig.ID) + `}`, http.StatusOK},
		{http.MethodGet, "/api/v1/compose/episodes/" + idText(episode.ID) + "/compose-status", "", http.StatusOK},
		{http.MethodPost, "/api/v1/compose/episodes/" + idText(episode.ID) + "/compose-all", `{}`, http.StatusOK},
		{http.MethodGet, "/api/v1/merge/episodes/" + idText(episode.ID) + "/merge", "", http.StatusOK},
		{http.MethodGet, "/api/v1/ai-providers?service_type=image", "", http.StatusOK},
		{http.MethodGet, "/api/v1/ai-voices", "", http.StatusOK},
		{http.MethodPost, "/api/v1/ai-voices/sync", `{}`, http.StatusOK},
	}
	for _, request := range paths {
		assertRequestStatus(t, router, request.method, request.path, request.body, request.status)
	}
	var generatedVideo models.VideoGeneration
	if err := db.DB.Where("video_url <> ''").Order("id desc").First(&generatedVideo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Model(&storyboard).Update("composed_video_url", generatedVideo.VideoURL).Error; err != nil {
		t.Fatal(err)
	}
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/merge/episodes/"+idText(episode.ID)+"/merge", `{}`, http.StatusOK)

	if got := mustRequestData(t, router, http.MethodPost, "/api/v1/scenes/"+idText(scene.ID)+"/copy", `{"episode_id":`+idText(episodes[1].ID)+`}`, http.StatusOK); got["id"] == nil {
		t.Fatalf("copy response=%#v", got)
	}

	config := models.AgentConfig{AgentType: "script_rewriter", Name: "Script", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/agent-configs", "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/agent-configs/"+idText(config.ID), "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/agent-configs", `{"agent_type":"script_rewriter","name":"Updated","max_iterations":2}`, http.StatusOK)
	assertRequestStatus(t, router, http.MethodPut, "/api/v1/agent-configs/"+idText(config.ID), `{"model":"local-model","temperature":0.2,"is_active":true}`, http.StatusOK)
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/agent/script_rewriter/debug", "", http.StatusOK)

	skillDir := filepath.Join(server.Agents.SkillsDir, "script_rewriter")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("test skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/skills", "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/skills/script_rewriter", "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/agent/script_rewriter/chat", `{"drama_id":`+idText(dramaID)+`,"episode_id":`+idText(episode.ID)+`,"message":"rewrite"}`, http.StatusOK)
	assertRequestStatus(t, router, http.MethodDelete, "/api/v1/agent-configs/"+idText(config.ID), "", http.StatusOK)

	run := models.AgentRun{AgentType: "script_rewriter", DramaID: dramaID, EpisodeID: episode.ID, Status: "running", Input: "test", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.AgentRunEvent{AgentRunID: run.ID, Sequence: 1, EventType: "started", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/agent-runs?status=running&agent_type=script_rewriter&episode_id="+idText(episode.ID), "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/agent-runs/"+idText(run.ID), "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/agent-runs/"+idText(run.ID)+"/cancel", `{}`, http.StatusOK)
	completedRun := models.AgentRun{AgentType: "extractor", DramaID: dramaID, EpisodeID: episode.ID, Status: "completed", Input: "done", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&completedRun).Error; err != nil {
		t.Fatal(err)
	}
	legacySucceededRun := models.AgentRun{AgentType: "voice_assigner", DramaID: dramaID, EpisodeID: episode.ID, Status: "succeeded", Input: "legacy", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&legacySucceededRun).Error; err != nil {
		t.Fatal(err)
	}
	completedResponse := performRequest(router, http.MethodGet, "/api/v1/agent-runs?status=completed", "", nil)
	if completedResponse.Code != http.StatusOK {
		t.Fatalf("completed agent runs status=%d body=%s", completedResponse.Code, completedResponse.Body.String())
	}
	completedPayload := decodeResponse(t, completedResponse)
	completedRows, ok := completedPayload["data"].([]any)
	if !ok || len(completedRows) < 2 {
		t.Fatalf("completed agent runs should include legacy succeeded rows: %#v", completedPayload["data"])
	}
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/agent-runs?status=unknown", "", http.StatusBadRequest)
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/agent-runs?agent_type=unknown", "", http.StatusBadRequest)
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/agent-runs/"+idText(completedRun.ID)+"/cancel", `{}`, http.StatusConflict)

	failedJob, err := server.Jobs.CreateForTarget("test", "other", 77, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Jobs.SetFailed(failedJob.ID, "retry me"); err != nil {
		t.Fatal(err)
	}
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/jobs?status=failed&kind=test", "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/jobs/"+idText(failedJob.ID), "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/jobs/"+idText(failedJob.ID)+"/events", "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/jobs/"+idText(failedJob.ID)+"/retry", `{}`, http.StatusOK)
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/jobs/"+idText(failedJob.ID)+"/cancel", `{}`, http.StatusOK)
	failedImage := models.ImageGeneration{ConfigID: &imageConfig.ID, DramaID: &dramaID, Prompt: "retry image", Provider: "mock", Status: "failed", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&failedImage).Error; err != nil {
		t.Fatal(err)
	}
	imageJob, err := server.Jobs.CreateForTarget("image.generate", "image_generation", failedImage.ID, "mock", &imageConfig.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Jobs.SetFailed(imageJob.ID, "retry image"); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Model(&failedImage).Update("job_id", imageJob.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/jobs/"+idText(imageJob.ID)+"/retry", `{}`, http.StatusOK)
	failedVideo := models.VideoGeneration{ConfigID: &videoConfig.ID, DramaID: &dramaID, Prompt: "retry video", Provider: "mock", Status: "failed", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&failedVideo).Error; err != nil {
		t.Fatal(err)
	}
	videoJob, err := server.Jobs.CreateForTarget("video.generate", "video_generation", failedVideo.ID, "mock", &videoConfig.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Jobs.SetFailed(videoJob.ID, "retry video"); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Model(&failedVideo).Update("job_id", videoJob.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/jobs/"+idText(videoJob.ID)+"/retry", `{}`, http.StatusOK)
	terminalJob, err := server.Jobs.CreateForTarget("terminal", "other", 88, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Jobs.SetSucceeded(terminalJob.ID, `{}`); err != nil {
		t.Fatal(err)
	}
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/jobs/"+idText(terminalJob.ID)+"/cancel", `{}`, http.StatusConflict)
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/jobs/"+idText(terminalJob.ID)+"/retry", `{}`, http.StatusConflict)
	for index, targetType := range []string{"image_generation", "video_generation"} {
		missingJob, createErr := server.Jobs.CreateForTarget("missing", targetType, uint(9000+index), "mock", nil)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if err := server.Jobs.SetFailed(missingJob.ID, "target deleted"); err != nil {
			t.Fatal(err)
		}
		assertRequestStatus(t, router, http.MethodPost, "/api/v1/jobs/"+idText(missingJob.ID)+"/retry", `{}`, http.StatusNotFound)
	}

	template := mustRequestData(t, router, http.MethodPost, "/api/v1/character-library", `{"name":"Template","appearance":"blue coat"}`, http.StatusCreated)
	templateID := uint(template["id"].(float64))
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/character-library", "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodPut, "/api/v1/character-library/"+idText(templateID), `{"appearance":"red coat"}`, http.StatusOK)
	assertRequestStatus(t, router, http.MethodDelete, "/api/v1/character-library/"+idText(templateID), "", http.StatusOK)

	assertRequestStatus(t, router, http.MethodDelete, "/api/v1/props/"+idText(prop.ID), "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodDelete, "/api/v1/storyboards/"+idText(storyboard.ID), "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodDelete, "/api/v1/scenes/"+idText(scene.ID), "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodDelete, "/api/v1/characters/"+idText(character.ID), "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodDelete, "/api/v1/ai-configs/"+idText(imageConfig.ID), "", http.StatusOK)
}

func assertRequestStatus(t *testing.T, router http.Handler, method, path, body string, want int) {
	t.Helper()
	result := performRequest(router, method, path, body, nil)
	if result.Code != want {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, result.Code, want, result.Body.String())
	}
}

func mustRequestData(t *testing.T, router http.Handler, method, path, body string, want int) map[string]any {
	t.Helper()
	result := performRequest(router, method, path, body, nil)
	if result.Code != want {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, result.Code, want, result.Body.String())
	}
	payload := decodeResponse(t, result)
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("response data=%#v", payload["data"])
	}
	return data
}

func idText(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}
