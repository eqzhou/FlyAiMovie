package httpapi

import (
	"net/http"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
)

func TestBatchGenerateTTSSkipsStoryboardsWithoutDialogue(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfig := createMockConfig(t, router, "image")
	videoConfig := createMockConfig(t, router, "video")
	audioConfig := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"batch tts","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	episode := requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"image_config_id":`+itoa(imageConfig)+`,"video_config_id":`+itoa(videoConfig)+`,"audio_config_id":`+itoa(audioConfig)+`}`)
	episodeID := uintField(t, episode, "id")
	spoken := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"title":"spoken","dialogue":"林夏：你好"}`)
	silent := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"title":"silent","dialogue":"（无）"}`)

	result := requestData(t, router, http.MethodPost, "/api/v1/storyboards/batch-generate-tts", `{"storyboard_ids":[`+itoa(uintField(t, spoken, "id"))+`,`+itoa(uintField(t, silent, "id"))+`]}`)
	if result["ok"] != float64(1) || result["skipped"] != float64(1) || result["fail"] != float64(0) {
		t.Fatalf("unexpected batch result: %#v", result)
	}
	var jobs int64
	if err := db.DB.Model(&models.GenerationJob{}).Where("kind = ?", "tts.generate").Count(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if jobs != 1 {
		t.Fatalf("tts jobs=%d want 1", jobs)
	}
}
