package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/gin-gonic/gin"
)

func TestUpdateAndBindingValidationBranches(t *testing.T) {
	_, router := testServerRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "Validation", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "Episode", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	otherDrama := models.Drama{Title: "Other", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&otherDrama).Error; err != nil {
		t.Fatal(err)
	}
	otherEpisode := models.Episode{DramaID: otherDrama.ID, EpisodeNumber: 1, Title: "Other Episode", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&otherEpisode).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{DramaID: drama.ID, Name: "Character", CreatedAt: now, UpdatedAt: now}
	scene := models.Scene{DramaID: drama.ID, Location: "Room", Time: "Day", Prompt: "room", CreatedAt: now, UpdatedAt: now}
	prop := models.Prop{DramaID: drama.ID, Name: "Prop", Prompt: "prop", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&character, &scene, &prop} {
		if err := db.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	storyboard := models.Storyboard{EpisodeID: episode.ID, StoryboardNumber: 1, Title: "Shot", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	asset := models.Asset{DramaID: &drama.ID, EpisodeID: &episode.ID, Name: "Video", Type: "video", URL: "https://example.com/video.mp4", MimeType: "video/mp4", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	aiConfig := models.AIServiceConfig{ServiceType: "image", Provider: "mock", Name: "Mock", IsActive: true, CreatedAt: now, UpdatedAt: now}
	agentConfig := models.AgentConfig{AgentType: "extractor", Name: "Extractor", IsActive: true, CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&aiConfig, &agentConfig} {
		if err := db.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodPut, "/api/v1/dramas/" + idText(drama.ID), `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/dramas/" + idText(drama.ID), `{"title":" "}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/dramas/" + idText(drama.ID), `{"tags":"bad"}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/dramas/" + idText(drama.ID), `{"tags":[1]}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/episodes/" + idText(episode.ID), `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/episodes/" + idText(episode.ID), `{"title":" "}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/episodes/" + idText(episode.ID), `{"image_config_id":0}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/characters/" + idText(character.ID), `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/characters/" + idText(character.ID), `{"name":" "}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/characters/" + idText(character.ID), `{"sort_order":-1}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/scenes/" + idText(scene.ID), `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/scenes/" + idText(scene.ID), `{"location":" "}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/props/" + idText(prop.ID), `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/props/" + idText(prop.ID), `{"name":" "}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/storyboards/" + idText(storyboard.ID), `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/storyboards/" + idText(storyboard.ID), `{"duration":0}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/storyboards/" + idText(storyboard.ID), `{"character_ids":"bad"}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/storyboards/" + idText(storyboard.ID), `{"scene_id":0}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/assets/" + idText(asset.ID), `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/assets/" + idText(asset.ID), `{"name":" "}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/assets/" + idText(asset.ID), `{"width":-1}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/assets/" + idText(asset.ID), `{"is_favorite":"yes"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/assets/" + idText(asset.ID) + "/apply", `{"storyboard_id":` + idText(storyboard.ID) + `,"frame_type":"bad"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/assets/" + idText(asset.ID) + "/apply", `{"storyboard_id":` + idText(storyboard.ID) + `}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/ai-configs/bad", `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/ai-configs/" + idText(aiConfig.ID), `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/ai-configs/" + idText(aiConfig.ID), `{"priority":1.5}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/ai-configs/" + idText(aiConfig.ID), `{"api_key":1}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/ai-configs/" + idText(aiConfig.ID), `{"is_active":"yes"}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/ai-configs/" + idText(aiConfig.ID), `{"name":" "}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/ai-configs/" + idText(aiConfig.ID), `{"provider":"unknown"}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/agent-configs/" + idText(agentConfig.ID), `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/agent-configs/" + idText(agentConfig.ID), `{"temperature":3}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/agent-configs/" + idText(agentConfig.ID), `{"max_tokens":0}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/agent-configs/" + idText(agentConfig.ID), `{"max_iterations":3}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/agent-configs/" + idText(agentConfig.ID), `{"is_active":"yes"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/agent-configs", `{"agent_type":"extractor","temperature":3}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/agent-configs", `{"agent_type":"extractor","max_tokens":0}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/agent-configs", `{"agent_type":"extractor","max_iterations":3}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/agent-configs", `{"agent_type":"voice_assigner","name":"Voice"}`, http.StatusOK},
		{http.MethodPost, "/api/v1/grid/generate", `{"prompt":"x","rows":6,"cols":1}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/videos", `{"prompt":"x","reference_image_urls":["1","2","3","4","5","6","7","8","9"]}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/characters", `{"drama_id":999,"name":"Missing"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/characters", `{"drama_id":` + idText(drama.ID) + `,"episode_id":` + idText(otherEpisode.ID) + `,"name":"Cross"}`, http.StatusConflict},
		{http.MethodPost, "/api/v1/characters", `{"drama_id":` + idText(drama.ID) + `,"name":"` + strings.Repeat("x", 201) + `"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/scenes", `{"drama_id":` + idText(drama.ID) + `,"episode_id":` + idText(otherEpisode.ID) + `,"location":"Cross"}`, http.StatusConflict},
		{http.MethodPost, "/api/v1/character-library", `{"name":"` + strings.Repeat("x", maxNameRunes+1) + `"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/character-library", `{"name":"Local","image_url":"/static/unowned.png"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/character-library", `{"name":"Refs","reference_images":"[bad"}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/ai-configs/" + idText(aiConfig.ID), `{"service_type":"image","provider":"mock","name":"Updated Mock","base_url":"","api_key":"mock","model":"m","endpoint":"e","query_endpoint":"q","settings":"{}","priority":3,"is_default":true,"is_active":true}`, http.StatusOK},
		{http.MethodPut, "/api/v1/agent-configs/" + idText(agentConfig.ID), `{"name":"Updated","description":"desc","model":"m","system_prompt":"system","temperature":0.5,"max_tokens":100,"max_iterations":2,"is_active":true}`, http.StatusOK},
		{http.MethodPut, "/api/v1/characters/" + idText(character.ID), `{"name":"Updated Character","role":"lead","description":"desc","appearance":"look","personality":"calm","voice_style":"voice","voice_provider":"mock","reference_images":"https://example.com/ref.png","seed_value":"1","sort_order":2}`, http.StatusOK},
		{http.MethodPut, "/api/v1/scenes/" + idText(scene.ID), `{"location":"Hall","time":"Night","prompt":"hall"}`, http.StatusOK},
		{http.MethodPut, "/api/v1/props/" + idText(prop.ID), `{"name":"Updated Prop","type":"item","description":"desc","prompt":"prompt"}`, http.StatusOK},
		{http.MethodPut, "/api/v1/storyboards/" + idText(storyboard.ID), `{"title":"Updated Shot","duration":10,"scene_id":` + idText(scene.ID) + `,"character_ids":[` + idText(character.ID) + `]}`, http.StatusOK},
		{http.MethodPut, "/api/v1/assets/" + idText(asset.ID), `{"name":"Updated Asset","description":"desc","type":"video","category":"reference","url":"https://example.com/new.mp4","mime_type":"video/mp4","format":"mp4","file_size":1,"width":2,"height":3,"duration":4,"is_favorite":true}`, http.StatusOK},
	}
	template := models.CharacterTemplate{Name: "Template", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	tests = append(tests,
		struct {
			method string
			path   string
			body   string
			status int
		}{http.MethodPut, "/api/v1/character-library/" + idText(template.ID), `{}`, http.StatusBadRequest},
		struct {
			method string
			path   string
			body   string
			status int
		}{http.MethodPut, "/api/v1/character-library/" + idText(template.ID), `{"name":" "}`, http.StatusBadRequest},
		struct {
			method string
			path   string
			body   string
			status int
		}{http.MethodPut, "/api/v1/character-library/" + idText(template.ID), `{"local_path":"missing.png"}`, http.StatusBadRequest},
		struct {
			method string
			path   string
			body   string
			status int
		}{http.MethodPut, "/api/v1/character-library/" + idText(template.ID), `{"reference_images":"[bad"}`, http.StatusBadRequest},
	)
	for _, test := range tests {
		result := performRequest(router, test.method, test.path, test.body, nil)
		if result.Code != test.status {
			t.Fatalf("%s %s status=%d want=%d body=%s", test.method, test.path, result.Code, test.status, result.Body.String())
		}
	}
}

func TestGridAssignmentModesAndFailedProbe(t *testing.T) {
	server, router := testServerRouter(t)
	now := response.Now()
	shots := []models.Storyboard{
		{EpisodeID: 1, StoryboardNumber: 1, CreatedAt: now, UpdatedAt: now},
		{EpisodeID: 1, StoryboardNumber: 2, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.DB.Create(&shots).Error; err != nil {
		t.Fatal(err)
	}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	quotaRecorder := httptest.NewRecorder()
	quotaContext, _ := gin.CreateTestContext(quotaRecorder)
	respondGenerationError(quotaContext, jobs.ErrQuotaExceeded)
	if quotaRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("quota status=%d body=%s", quotaRecorder.Code, quotaRecorder.Body.String())
	}
	budgetRecorder := httptest.NewRecorder()
	budgetContext, _ := gin.CreateTestContext(budgetRecorder)
	respondGenerationError(budgetContext, jobs.ErrBudgetExceeded)
	if budgetRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("budget status=%d body=%s", budgetRecorder.Code, budgetRecorder.Body.String())
	}
	if err := server.assignGridCells(context, []uint{shots[0].ID, shots[1].ID, shots[0].ID, shots[1].ID}, []string{"f1", "f2", "l1", "l2"}, "first_last"); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"", "last_frame", "composed", "storyboard"} {
		if err := server.assignGridCells(context, []uint{shots[0].ID}, []string{mode + "-url"}, mode); err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
	}
	if err := server.assignGridCells(context, []uint{shots[0].ID}, []string{"only"}, "first_last"); err == nil {
		t.Fatal("invalid first_last assignment accepted")
	}
	if err := server.assignGridCells(context, []uint{999}, []string{"missing"}, "first_frame"); err == nil {
		t.Fatal("missing storyboard assignment accepted")
	}
	rollbackShot := models.Storyboard{EpisodeID: 1, StoryboardNumber: 3, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&rollbackShot).Error; err != nil {
		t.Fatal(err)
	}
	missingHistoryID := uint(99999)
	if err := server.assignGridCellsWithHistory(context, []uint{rollbackShot.ID}, []string{"must-rollback"}, "first_frame", &missingHistoryID, map[string]any{"status": "split"}); err == nil {
		t.Fatal("missing history did not roll back grid assignment")
	}
	if err := db.DB.First(&rollbackShot, rollbackShot.ID).Error; err != nil {
		t.Fatal(err)
	}
	if rollbackShot.FirstFrameImage != "" {
		t.Fatalf("grid frame survived failed history update: %q", rollbackShot.FirstFrameImage)
	}

	relative, _, err := server.Store.SaveBytes("uploads", "corrupt.mp4", []byte("not media"))
	if err != nil {
		t.Fatal(err)
	}
	asset := models.Asset{Name: "Corrupt", Type: "video", URL: server.Store.PublicURL(relative), LocalPath: relative, ProbeStatus: "pending", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	probe := performRequest(router, http.MethodPost, "/api/v1/assets/"+idText(asset.ID)+"/probe", `{}`, nil)
	if probe.Code != http.StatusUnprocessableEntity {
		t.Fatalf("probe status=%d body=%s", probe.Code, probe.Body.String())
	}
	if err := db.DB.First(&asset, asset.ID).Error; err != nil {
		t.Fatal(err)
	}
	if asset.ProbeStatus != "failed" || asset.ProbeError == "" || asset.DurationSeconds != 0 {
		t.Fatalf("asset=%+v", asset)
	}
	path, err := server.Store.Resolve(relative)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
}
