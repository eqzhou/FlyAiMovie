package httpapi

import (
	"net/http"
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
