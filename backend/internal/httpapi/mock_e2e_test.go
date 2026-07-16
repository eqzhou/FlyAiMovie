package httpapi

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/services/generation"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
)

// TestMockPipelineEndToEnd exercises the durable HTTP/worker path with only
// local mock providers. It intentionally uses the public routes so regressions
// in request validation, persistence, or worker hand-off are observable.
func TestMockPipelineEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required by the mock video, TTS, and compose providers")
	}
	server, router := testServerRouter(t)
	worker := &generation.AsyncRunner{
		Images: server.Images,
		Videos: server.Videos,
		TTS:    server.TTS,
		Jobs:   server.Jobs,
		Store:  server.Store,
	}
	worker.Start()
	t.Cleanup(worker.Stop)

	imageConfig := createMockConfig(t, router, "image")
	videoConfig := createMockConfig(t, router, "video")
	audioConfig := createMockConfig(t, router, "audio")

	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"mock e2e","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	episodeData := requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"title":"mock episode","image_config_id":`+itoa(imageConfig)+`,"video_config_id":`+itoa(videoConfig)+`,"audio_config_id":`+itoa(audioConfig)+`}`)
	episodeID := uintField(t, episodeData, "id")

	storyboard := requestData(t, router, http.MethodPost, "/api/v1/storyboards", `{"episode_id":`+itoa(episodeID)+`,"title":"opening","image_prompt":"a quiet room","video_prompt":"a quiet room","dialogue":"阿宁：你好，世界","duration":1}`)
	storyboardID := uintField(t, storyboard, "id")

	frame := requestData(t, router, http.MethodPost, "/api/v1/storyboards/"+itoa(storyboardID)+"/generate-frame", `{"frame_type":"first_frame","config_id":`+itoa(imageConfig)+`}`)
	if stringField(t, frame, "image_url") == "" {
		t.Fatalf("mock frame did not return an image URL: %#v", frame)
	}
	var sb models.Storyboard
	if err := db.DB.First(&sb, storyboardID).Error; err != nil {
		t.Fatal(err)
	}
	if sb.FirstFrameImage == "" {
		t.Fatalf("mock frame did not persist first_frame_image")
	}
	assertStoredFile(t, server.Store.Root, sb.FirstFrameImage)

	video := requestData(t, router, http.MethodPost, "/api/v1/storyboards/"+itoa(storyboardID)+"/generate-video", `{"config_id":`+itoa(videoConfig)+`}`)
	if stringField(t, video, "video_url") == "" {
		t.Fatalf("mock video did not return a video URL: %#v", video)
	}
	if err := db.DB.First(&sb, storyboardID).Error; err != nil {
		t.Fatal(err)
	}
	if sb.VideoURL == "" {
		t.Fatalf("mock video did not persist video_url")
	}
	assertStoredFile(t, server.Store.Root, sb.VideoURL)

	tts := requestData(t, router, http.MethodPost, "/api/v1/storyboards/"+itoa(storyboardID)+"/generate-tts", `{}`)
	ttsJobID := uintField(t, tts, "job_id")
	waitJob(t, server.Jobs, ttsJobID)
	if err := db.DB.First(&sb, storyboardID).Error; err != nil {
		t.Fatal(err)
	}
	if sb.TTSAudioURL == "" {
		t.Fatalf("TTS worker succeeded without persisting audio URL")
	}
	assertStoredFile(t, server.Store.Root, sb.TTSAudioURL)

	compose := requestData(t, router, http.MethodPost, "/api/v1/compose/episodes/"+itoa(episodeID)+"/compose-all", `{}`)
	composeJobID := uintField(t, compose, "job_id")
	waitJob(t, server.Jobs, composeJobID)
	if err := db.DB.First(&sb, storyboardID).Error; err != nil {
		t.Fatal(err)
	}
	if sb.ComposedVideoURL == "" {
		t.Fatalf("compose worker succeeded without persisting composed video URL")
	}
	assertStoredFile(t, server.Store.Root, sb.ComposedVideoURL)

	merge := requestData(t, router, http.MethodPost, "/api/v1/merge/episodes/"+itoa(episodeID)+"/merge", `{}`)
	mergeJobID := uintField(t, merge, "job_id")
	waitJob(t, server.Jobs, mergeJobID)
	var episodeRow models.Episode
	if err := db.DB.First(&episodeRow, episodeID).Error; err != nil {
		t.Fatal(err)
	}
	if episodeRow.VideoURL == "" || episodeRow.Status != "completed" {
		t.Fatalf("merge worker did not complete episode: url=%q status=%q", episodeRow.VideoURL, episodeRow.Status)
	}
	assertStoredFile(t, server.Store.Root, episodeRow.VideoURL)
}

func createMockConfig(t *testing.T, router http.Handler, serviceType string) uint {
	t.Helper()
	data := requestData(t, router, http.MethodPost, "/api/v1/ai-configs", `{"service_type":"`+serviceType+`","provider":"mock","name":"e2e-`+serviceType+`","base_url":"http://localhost","api_key":"mock","model":"mock","is_active":true}`)
	return uintField(t, data, "id")
}

func waitJob(t *testing.T, service *jobs.Service, id uint) *models.GenerationJob {
	t.Helper()
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		job, err := service.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		switch job.Status {
		case jobs.StatusSucceeded:
			return job
		case jobs.StatusFailed, jobs.StatusCanceled:
			t.Fatalf("job %d ended %s: %s", id, job.Status, job.LastError)
		}
		time.Sleep(100 * time.Millisecond)
	}
	job, _ := service.Get(id)
	t.Fatalf("job %d did not finish before timeout: %#v", id, job)
	return nil
}

func requestData(t *testing.T, router http.Handler, method, path, body string) map[string]any {
	t.Helper()
	res := performRequest(router, method, path, body, nil)
	if res.Code < 200 || res.Code >= 300 {
		t.Fatalf("%s %s: status=%d body=%s", method, path, res.Code, res.Body.String())
	}
	payload := decodeResponse(t, res)
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("%s %s: response data is not an object: %#v", method, path, payload)
	}
	return data
}

func uintField(t *testing.T, data map[string]any, field string) uint {
	t.Helper()
	v, ok := data[field].(float64)
	if !ok || v < 1 {
		t.Fatalf("missing positive numeric field %q in %#v", field, data)
	}
	return uint(v)
}

func stringField(t *testing.T, data map[string]any, field string) string {
	t.Helper()
	v, _ := data[field].(string)
	return v
}

func assertStoredFile(t *testing.T, root, publicURL string) {
	t.Helper()
	const prefix = "/static/"
	if len(publicURL) <= len(prefix) || publicURL[:len(prefix)] != prefix {
		t.Fatalf("expected local static URL, got %q", publicURL)
	}
	path := filepath.Join(root, filepath.FromSlash(publicURL[len(prefix):]))
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("stored file is missing or empty: %s (%v)", path, err)
	}
}

func itoa(v uint) string { return strconv.FormatUint(uint64(v), 10) }
