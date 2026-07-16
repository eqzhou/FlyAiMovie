package httpapi

import (
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
)

// TestMockAgentAndGridWorkflow verifies the editable story-production path
// from source text through persisted entities and assigned frame cells.
func TestMockAgentAndGridWorkflow(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required by grid splitting")
	}
	_, router := testServerRouter(t)
	textConfig := createMockConfig(t, router, "text")
	imageConfig := createMockConfig(t, router, "image")
	videoConfig := createMockConfig(t, router, "video")
	audioConfig := createMockConfig(t, router, "audio")
	_ = textConfig

	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"agent grid flow","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	episode := requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"title":"第一集","image_config_id":`+itoa(imageConfig)+`,"video_config_id":`+itoa(videoConfig)+`,"audio_config_id":`+itoa(audioConfig)+`}`)
	episodeID := uintField(t, episode, "id")
	update := performRequest(router, http.MethodPut, "/api/v1/episodes/"+itoa(episodeID), `{"content":"## S01 | 内景 · 客厅 | 夜\n林舟：你终于来了。\n苏梅：我一直在这里。"}`, nil)
	if update.Code != http.StatusOK {
		t.Fatalf("save source status=%d body=%s", update.Code, update.Body.String())
	}

	for _, agentType := range []string{"script_rewriter", "extractor", "storyboard_breaker"} {
		result := performRequest(router, http.MethodPost, "/api/v1/agent/"+agentType+"/chat", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"message":"执行当前制作步骤"}`, nil)
		if result.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", agentType, result.Code, result.Body.String())
		}
	}

	var characters []models.Character
	if err := db.DB.Where("drama_id = ?", dramaID).Find(&characters).Error; err != nil || len(characters) < 1 {
		t.Fatalf("agent did not persist characters: count=%d err=%v", len(characters), err)
	}
	var scenes []models.Scene
	if err := db.DB.Where("drama_id = ?", dramaID).Find(&scenes).Error; err != nil || len(scenes) < 1 {
		t.Fatalf("agent did not persist scenes: count=%d err=%v", len(scenes), err)
	}
	var storyboards []models.Storyboard
	if err := db.DB.Where("episode_id = ?", episodeID).Order("storyboard_number").Find(&storyboards).Error; err != nil || len(storyboards) < 1 {
		t.Fatalf("agent did not persist storyboards: count=%d err=%v", len(storyboards), err)
	}

	prompt := performRequest(router, http.MethodPost, "/api/v1/grid/prompt", `{"episode_id":`+itoa(episodeID)+`,"rows":2,"cols":2,"mode":"first_frame"}`, nil)
	if prompt.Code != http.StatusOK || !strings.Contains(prompt.Body.String(), "grid_prompt") {
		t.Fatalf("grid prompt status=%d body=%s", prompt.Code, prompt.Body.String())
	}
	generated := performRequest(router, http.MethodPost, "/api/v1/grid/generate", `{"episode_id":`+itoa(episodeID)+`,"config_id":`+itoa(imageConfig)+`,"rows":2,"cols":2,"mode":"first_frame","prompt":"cinematic storyboard grid"}`, nil)
	if generated.Code != http.StatusOK {
		t.Fatalf("grid generate status=%d body=%s", generated.Code, generated.Body.String())
	}
	data := decodeResponse(t, generated)["data"].(map[string]any)
	history := data["history"].(map[string]any)
	historyID := uint(history["id"].(float64))
	image := data["image"].(map[string]any)
	imageURL, _ := image["image_url"].(string)
	if imageURL == "" {
		t.Fatalf("grid image URL missing: %s", generated.Body.String())
	}

	ids := make([]string, 0, len(storyboards))
	for i, storyboard := range storyboards {
		if i == 4 {
			break
		}
		ids = append(ids, itoa(storyboard.ID))
	}
	for len(ids) < 4 {
		ids = append(ids, ids[len(ids)%len(ids)])
	}
	splitBody := `{"history_id":` + itoa(historyID) + `,"image_url":"` + imageURL + `","rows":2,"cols":2,"frame_type":"first_frame","storyboard_ids":[` + strings.Join(ids, ",") + `]}`
	split := performRequest(router, http.MethodPost, "/api/v1/grid/split", splitBody, nil)
	if split.Code != http.StatusOK {
		t.Fatalf("grid split status=%d body=%s", split.Code, split.Body.String())
	}
	var updated models.Storyboard
	if err := db.DB.First(&updated, storyboards[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.FirstFrameImage == "" {
		t.Fatalf("grid split did not assign first frame")
	}
	cells, ok := decodeResponse(t, split)["data"].(map[string]any)["cells"].([]any)
	if !ok || len(cells) != 4 {
		t.Fatalf("grid split cells=%#v", cells)
	}
}
