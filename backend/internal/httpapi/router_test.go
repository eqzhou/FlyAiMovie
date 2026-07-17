package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/storage"
)

func testRouter(t *testing.T) http.Handler {
	t.Helper()
	_, router := testServerRouter(t)
	return router
}

func TestParseReferenceMediaValues(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		want      []string
		wantError bool
	}{
		{name: "empty", value: "  ", want: []string{}},
		{name: "data uri", value: "data:image/png;base64,AAAA", want: []string{"data:image/png;base64,AAAA"}},
		{name: "json array", value: `["https://example.com/a.png", " /static/b.png "]`, want: []string{"https://example.com/a.png", "/static/b.png"}},
		{name: "comma separated", value: "https://example.com/a.png, /static/b.png", want: []string{"https://example.com/a.png", "/static/b.png"}},
		{name: "malformed json", value: `["https://example.com/a.png"`, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseReferenceMediaValues(tt.value)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for index := range tt.want {
				if got[index] != tt.want[index] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestValidateReferenceMediaOwnershipRejectsMoreThanEightImages(t *testing.T) {
	items := make([]string, 9)
	for index := range items {
		items[index] = "https://example.com/reference-" + strconv.Itoa(index) + ".png"
	}
	if err := validateReferenceMediaOwnership(nil, strings.Join(items, ",")); err == nil {
		t.Fatal("expected more than eight reference images to be rejected")
	}
}

func testServerRouter(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	gdb, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(gdb); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	cfg := &config.Config{
		App: config.AppConfig{Debug: true},
		Server: config.ServerConfig{
			CORSOrigins:   []string{"http://allowed.example"},
			WebhookSecret: "test-webhook-secret",
		},
	}
	server := NewServer(cfg, storage.NewLocal(t.TempDir()), t.TempDir(), "")
	return server, server.Router()
}

func signedWebhookHeaders(body, eventID string, timestamp time.Time) map[string]string {
	ts := strconv.FormatInt(timestamp.Unix(), 10)
	mac := hmac.New(sha256.New, []byte("test-webhook-secret"))
	_, _ = mac.Write([]byte(ts + "." + body))
	return map[string]string{
		"X-Webhook-Timestamp": ts,
		"X-Webhook-Id":        eventID,
		"X-Webhook-Signature": "sha256=" + hex.EncodeToString(mac.Sum(nil)),
	}
}

func performRequest(handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
	return payload
}

func TestAIConfigResponsesNeverExposeAPIKey(t *testing.T) {
	router := testRouter(t)
	created := performRequest(router, http.MethodPost, "/api/v1/ai-configs", `{
		"service_type":"image",
		"provider":"openai",
		"name":"private-provider",
		"base_url":"https://api.example.com",
		"api_key":"test-api-key-one",
		"model":"image-model"
	}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body=%s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), "sk-super-secret") {
		t.Fatalf("create response leaked API key: %s", created.Body.String())
	}

	payload := decodeResponse(t, created)
	data := payload["data"].(map[string]any)
	if _, exists := data["api_key"]; exists {
		t.Fatalf("create response included API key field: %s", created.Body.String())
	}
	id := data["id"].(float64)
	updated := performRequest(router, http.MethodPut, "/api/v1/ai-configs/"+jsonNumber(id), `{
		"api_key":"test-api-key-two"
	}`, nil)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d; body=%s", updated.Code, updated.Body.String())
	}
	if strings.Contains(updated.Body.String(), "sk-replacement-secret") {
		t.Fatalf("update response leaked API key: %s", updated.Body.String())
	}
	updatedData := decodeResponse(t, updated)["data"].(map[string]any)
	if _, exists := updatedData["api_key"]; exists {
		t.Fatalf("update response included API key field: %s", updated.Body.String())
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/ai-configs", "", nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("list status = %d; body=%s", listed.Code, listed.Body.String())
	}
	if strings.Contains(listed.Body.String(), "sk-") || strings.Contains(listed.Body.String(), "secret") {
		t.Fatalf("list response leaked API key material: %s", listed.Body.String())
	}
}

func TestUpdateAIConfigReturnsNotFound(t *testing.T) {
	router := testRouter(t)
	response := performRequest(router, http.MethodPut, "/api/v1/ai-configs/999", `{"name":"missing"}`, nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", response.Code, response.Body.String())
	}
}

func TestAIConfigRejectsUnsupportedProviderPair(t *testing.T) {
	router := testRouter(t)
	response := performRequest(router, http.MethodPost, "/api/v1/ai-configs", `{
		"service_type":"video","provider":"gemini","name":"wrong pair",
		"base_url":"https://generativelanguage.googleapis.com","api_key":"secret"
	}`, nil)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "does not support") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAgentConfigRejectsUnsupportedRuntimeLimits(t *testing.T) {
	router := testRouter(t)
	response := performRequest(router, http.MethodPost, "/api/v1/agent-configs", `{
		"agent_type":"script_rewriter","name":"invalid","max_tokens":0,"max_iterations":3
	}`, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400; body=%s", response.Code, response.Body.String())
	}
}

func TestAgentRejectsCrossDramaEpisodeAndSkillsHidePaths(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	dramaA := models.Drama{Title: "A", CreatedAt: now, UpdatedAt: now}
	dramaB := models.Drama{Title: "B", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&dramaA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&dramaB).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: dramaA.ID, EpisodeNumber: 1, Title: "A1", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}

	response := performRequest(router, http.MethodPost, "/api/v1/agent/script_rewriter/chat", `{
		"drama_id":`+itoa(dramaB.ID)+`,"episode_id":`+itoa(episode.ID)+`,"message":"rewrite"
	}`, nil)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "episode not found") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	listed := performRequest(router, http.MethodGet, "/api/v1/skills", "", nil)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), `"path"`) || strings.Contains(listed.Body.String(), t.TempDir()) {
		t.Fatalf("skills leaked filesystem path: status=%d body=%s", listed.Code, listed.Body.String())
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/skills/not-real", "", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unsupported skill status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestDramaPaginationClampsInvalidPageSize(t *testing.T) {
	router := testRouter(t)
	tests := []struct {
		query string
		want  float64
	}{
		{query: "page_size=0", want: 20},
		{query: "page_size=-1", want: 20},
		{query: "page_size=invalid", want: 20},
		{query: "page_size=1000000", want: 100},
		{query: "page=999999999999999999999&page_size=1", want: 1},
	}
	for _, test := range tests {
		response := performRequest(router, http.MethodGet, "/api/v1/dramas?"+test.query, "", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("query %q: status = %d; body=%s", test.query, response.Code, response.Body.String())
		}
		payload := decodeResponse(t, response)
		data := payload["data"].(map[string]any)
		pagination := data["pagination"].(map[string]any)
		if pagination["page_size"] != test.want {
			t.Fatalf("query %q: page_size = %v, want %v", test.query, pagination["page_size"], test.want)
		}
	}
}

func TestCreateDramaRejectsUnboundedEpisodeCount(t *testing.T) {
	router := testRouter(t)
	response := performRequest(router, http.MethodPost, "/api/v1/dramas", `{
		"title":"too large",
		"total_episodes":1001
	}`, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", response.Code, response.Body.String())
	}
}

func TestJobEventsAndBatchCancelAPI(t *testing.T) {
	server, router := testServerRouter(t)
	first, err := server.Jobs.CreateQueued("tts.generate", "storyboard_tts", 801, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := server.Jobs.CreateQueued("tts.generate", "storyboard_tts", 802, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}

	events := performRequest(router, http.MethodGet, "/api/v1/jobs/"+itoa(first.ID)+"/events", "", nil)
	if events.Code != http.StatusOK || !strings.Contains(events.Body.String(), `"stage":"queued"`) {
		t.Fatalf("events status=%d body=%s", events.Code, events.Body.String())
	}

	batch := performRequest(router, http.MethodPost, "/api/v1/jobs/batch-cancel", `{"job_ids":[`+itoa(first.ID)+`,`+itoa(second.ID)+`,99999]}`, nil)
	if batch.Code != http.StatusOK {
		t.Fatalf("batch status=%d body=%s", batch.Code, batch.Body.String())
	}
	payload := decodeResponse(t, batch)["data"].(map[string]any)
	if len(payload["canceled"].([]any)) != 2 {
		t.Fatalf("unexpected canceled response: %#v", payload)
	}
	for _, id := range []uint{first.ID, second.ID} {
		job, err := server.Jobs.Get(id)
		if err != nil || job.Status != jobs.StatusCanceled {
			t.Fatalf("job %d status=%v err=%v", id, job, err)
		}
	}
}

func TestAssetCannotClaimUnownedPrivateStaticPath(t *testing.T) {
	server, router := testServerRouter(t)
	rel, _, err := server.Store.SaveBytes("uploads", "private.mp4", []byte("not public"))
	if err != nil {
		t.Fatal(err)
	}
	result := performRequest(router, http.MethodPost, "/api/v1/assets", `{"name":"claim","type":"video","url":"`+server.Store.PublicURL(rel)+`","local_path":"`+rel+`"}`, nil)
	if result.Code != http.StatusBadRequest || !strings.Contains(result.Body.String(), "not owned") {
		t.Fatalf("status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestEpisodeRejectsMismatchedAIConfigTypes(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "config ownership", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	imageConfig := models.AIServiceConfig{ServiceType: "image", Provider: "mock", Name: "image", BaseURL: "http://localhost", APIKey: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now}
	audioConfig := models.AIServiceConfig{ServiceType: "audio", Provider: "mock", Name: "audio", BaseURL: "http://localhost", APIKey: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&imageConfig).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&audioConfig).Error; err != nil {
		t.Fatal(err)
	}

	created := performRequest(router, http.MethodPost, "/api/v1/episodes", `{
		"drama_id":`+itoa(drama.ID)+`,"title":"bad config",
		"image_config_id":`+itoa(imageConfig.ID)+`,"video_config_id":`+itoa(imageConfig.ID)+`,
		"audio_config_id":`+itoa(audioConfig.ID)+`
	}`, nil)
	if created.Code != http.StatusBadRequest || !strings.Contains(created.Body.String(), "video AI config") {
		t.Fatalf("status=%d body=%s", created.Code, created.Body.String())
	}
}

func TestCreateCharacterLinksItToEpisode(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "manual cast", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "episode", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}

	created := performRequest(router, http.MethodPost, "/api/v1/characters", `{
		"drama_id":`+strconv.Itoa(int(drama.ID))+`,
		"episode_id":`+strconv.Itoa(int(episode.ID))+`,
		"name":"林岚","role":"主角","appearance":"黑色短发"
	}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body=%s", created.Code, created.Body.String())
	}
	payload := decodeResponse(t, created)
	data := payload["data"].(map[string]any)
	characterID := uint(data["id"].(float64))
	var link models.EpisodeCharacter
	if err := db.DB.Where("episode_id = ? AND character_id = ?", episode.ID, characterID).First(&link).Error; err != nil {
		t.Fatalf("episode-character link missing: %v", err)
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/episodes/"+strconv.Itoa(int(episode.ID))+"/characters", "", nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "林岚") {
		t.Fatalf("linked character not listed: status=%d body=%s", listed.Code, listed.Body.String())
	}
}

func TestComposeShotQueuesDurableJob(t *testing.T) {
	server, router := testServerRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "queued compose", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "episode", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	rel, _, err := server.Store.SaveBytes("videos", "input.mp4", []byte("test video placeholder"))
	if err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{
		EpisodeID: episode.ID, StoryboardNumber: 1, VideoURL: server.Store.PublicURL(rel),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.DB.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}

	queued := performRequest(router, http.MethodPost, "/api/v1/compose/storyboards/"+strconv.Itoa(int(storyboard.ID))+"/compose", `{}`, nil)
	if queued.Code != http.StatusOK {
		t.Fatalf("queue status=%d body=%s", queued.Code, queued.Body.String())
	}
	data := decodeResponse(t, queued)["data"].(map[string]any)
	jobID := uint(data["job_id"].(float64))
	job, err := server.Jobs.Get(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != jobs.StatusQueued || job.TargetType != "storyboard_compose" {
		t.Fatalf("unexpected compose job: %+v", job)
	}
	if !strings.Contains(job.PayloadJSON, `"storyboard_id":`+strconv.Itoa(int(storyboard.ID))) || !strings.Contains(job.PayloadJSON, storyboard.VideoURL) {
		t.Fatalf("compose payload does not preserve inputs: %s", job.PayloadJSON)
	}
	if storyboard.ComposedVideoURL != "" {
		t.Fatalf("HTTP handler composed synchronously: %+v", storyboard)
	}
}

func TestStoryboardRejectsCrossDramaResources(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	dramaA := models.Drama{Title: "A", CreatedAt: now, UpdatedAt: now}
	dramaB := models.Drama{Title: "B", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&dramaA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&dramaB).Error; err != nil {
		t.Fatal(err)
	}
	episodeA := models.Episode{DramaID: dramaA.ID, EpisodeNumber: 1, Title: "A1", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episodeA).Error; err != nil {
		t.Fatal(err)
	}
	foreignScene := models.Scene{DramaID: dramaB.ID, Location: "foreign", Time: "day", Prompt: "foreign", CreatedAt: now, UpdatedAt: now}
	foreignCharacter := models.Character{DramaID: dramaB.ID, Name: "foreign", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&foreignScene).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&foreignCharacter).Error; err != nil {
		t.Fatal(err)
	}

	created := performRequest(router, http.MethodPost, "/api/v1/storyboards", `{
		"episode_id":`+itoa(episodeA.ID)+`,"title":"invalid",
		"scene_id":`+itoa(foreignScene.ID)+`,"character_ids":[`+itoa(foreignCharacter.ID)+`]
	}`, nil)
	if created.Code != http.StatusConflict {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var count int64
	db.DB.Model(&models.Storyboard{}).Where("episode_id = ?", episodeA.ID).Count(&count)
	if count != 0 {
		t.Fatalf("cross-drama storyboard was persisted")
	}

	valid := models.Storyboard{EpisodeID: episodeA.ID, StoryboardNumber: 1, Title: "valid", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&valid).Error; err != nil {
		t.Fatal(err)
	}
	updated := performRequest(router, http.MethodPut, "/api/v1/storyboards/"+itoa(valid.ID), `{
		"scene_id":`+itoa(foreignScene.ID)+`,"character_ids":[`+itoa(foreignCharacter.ID)+`]
	}`, nil)
	if updated.Code != http.StatusConflict {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
}

func TestSceneAndPropRejectCrossDramaEpisode(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	dramaA := models.Drama{Title: "A", CreatedAt: now, UpdatedAt: now}
	dramaB := models.Drama{Title: "B", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&dramaA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&dramaB).Error; err != nil {
		t.Fatal(err)
	}
	episodeA := models.Episode{DramaID: dramaA.ID, EpisodeNumber: 1, Title: "A1", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episodeA).Error; err != nil {
		t.Fatal(err)
	}

	scene := performRequest(router, http.MethodPost, "/api/v1/scenes", `{
		"drama_id":`+itoa(dramaB.ID)+`,"episode_id":`+itoa(episodeA.ID)+`,"location":"invalid"
	}`, nil)
	if scene.Code != http.StatusConflict {
		t.Fatalf("scene status=%d body=%s", scene.Code, scene.Body.String())
	}
	prop := models.Prop{DramaID: dramaB.ID, Name: "foreign prop", Prompt: "prop", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&prop).Error; err != nil {
		t.Fatal(err)
	}
	generated := performRequest(router, http.MethodPost, "/api/v1/props/"+itoa(prop.ID)+"/generate-image", `{"episode_id":`+itoa(episodeA.ID)+`}`, nil)
	if generated.Code != http.StatusConflict {
		t.Fatalf("prop generate status=%d body=%s", generated.Code, generated.Body.String())
	}
}

func TestGenerationEndpointsRejectCrossDramaTargets(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	dramaA := models.Drama{Title: "A", CreatedAt: now, UpdatedAt: now}
	dramaB := models.Drama{Title: "B", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&dramaA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&dramaB).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: dramaA.ID, EpisodeNumber: 1, Title: "A1", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{EpisodeID: episode.ID, StoryboardNumber: 1, Title: "A shot", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}

	image := performRequest(router, http.MethodPost, "/api/v1/images", `{
		"storyboard_id":`+itoa(storyboard.ID)+`,"drama_id":`+itoa(dramaB.ID)+`,"prompt":"invalid"
	}`, nil)
	if image.Code != http.StatusConflict {
		t.Fatalf("image status=%d body=%s", image.Code, image.Body.String())
	}
	video := performRequest(router, http.MethodPost, "/api/v1/videos", `{
		"storyboard_id":`+itoa(storyboard.ID)+`,"drama_id":`+itoa(dramaB.ID)+`,"prompt":"invalid"
	}`, nil)
	if video.Code != http.StatusConflict {
		t.Fatalf("video status=%d body=%s", video.Code, video.Body.String())
	}
	var imageCount, videoCount int64
	db.DB.Model(&models.ImageGeneration{}).Count(&imageCount)
	db.DB.Model(&models.VideoGeneration{}).Count(&videoCount)
	if imageCount != 0 || videoCount != 0 {
		t.Fatalf("invalid generations persisted: images=%d videos=%d", imageCount, videoCount)
	}
}

func TestGridRejectsOversizedAndCrossEpisodeAssignments(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	dramaA := models.Drama{Title: "A", CreatedAt: now, UpdatedAt: now}
	dramaB := models.Drama{Title: "B", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&dramaA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&dramaB).Error; err != nil {
		t.Fatal(err)
	}
	episodeA := models.Episode{DramaID: dramaA.ID, EpisodeNumber: 1, Title: "A1", CreatedAt: now, UpdatedAt: now}
	episodeB := models.Episode{DramaID: dramaB.ID, EpisodeNumber: 1, Title: "B1", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episodeA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&episodeB).Error; err != nil {
		t.Fatal(err)
	}
	foreignStoryboard := models.Storyboard{EpisodeID: episodeB.ID, StoryboardNumber: 1, Title: "foreign", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&foreignStoryboard).Error; err != nil {
		t.Fatal(err)
	}
	history := models.GridHistory{DramaID: &dramaA.ID, EpisodeID: &episodeA.ID, Rows: 2, Cols: 2, Status: "completed", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&history).Error; err != nil {
		t.Fatal(err)
	}

	oversized := performRequest(router, http.MethodPost, "/api/v1/grid/split", `{"rows":6,"cols":6,"image_url":"/static/missing.png"}`, nil)
	if oversized.Code != http.StatusBadRequest {
		t.Fatalf("oversized status=%d body=%s", oversized.Code, oversized.Body.String())
	}
	cross := performRequest(router, http.MethodPost, "/api/v1/grid/split", `{
		"rows":1,"cols":1,"history_id":`+itoa(history.ID)+`,
		"storyboard_ids":[`+itoa(foreignStoryboard.ID)+`],"image_url":"/static/missing.png"
	}`, nil)
	if cross.Code != http.StatusConflict {
		t.Fatalf("cross status=%d body=%s", cross.Code, cross.Body.String())
	}
}

func TestCORSRejectsUntrustedOrigin(t *testing.T) {
	router := testRouter(t)
	response := performRequest(router, http.MethodGet, "/api/v1/health", "", map[string]string{
		"Origin": "https://attacker.example",
	})
	if origin := response.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Fatalf("untrusted origin was allowed: %q", origin)
	}
}

func TestCORSAllowsConfiguredOrigin(t *testing.T) {
	router := testRouter(t)
	response := performRequest(router, http.MethodGet, "/api/v1/health", "", map[string]string{
		"Origin": "http://allowed.example",
	})
	if origin := response.Header().Get("Access-Control-Allow-Origin"); origin != "http://allowed.example" {
		t.Fatalf("configured origin = %q", origin)
	}
	if credentials := response.Header().Get("Access-Control-Allow-Credentials"); credentials != "true" {
		t.Fatalf("credentials header = %q", credentials)
	}
	if vary := response.Header().Get("Vary"); vary != "Origin" {
		t.Fatalf("vary header = %q", vary)
	}
}

func TestWebhookRequiresValidSignatureAndRejectsReplay(t *testing.T) {
	router := testRouter(t)
	ts := response.Now()
	record := models.VideoGeneration{
		TaskID: "provider-task-1", Status: "processing", Provider: "mock",
		CreatedAt: ts, UpdatedAt: ts,
	}
	if err := db.DB.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	body := `{"task_id":"provider-task-1","status":"failed","err_msg":"provider failed"}`

	unsigned := performRequest(router, http.MethodPost, "/api/v1/webhooks/generic", body, nil)
	if unsigned.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned status = %d, want 401; body=%s", unsigned.Code, unsigned.Body.String())
	}
	var unchanged models.VideoGeneration
	if err := db.DB.First(&unchanged, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != "processing" {
		t.Fatalf("unsigned webhook changed status to %q", unchanged.Status)
	}

	headers := signedWebhookHeaders(body, "event-1", time.Now())
	signed := performRequest(router, http.MethodPost, "/api/v1/webhooks/generic", body, headers)
	if signed.Code != http.StatusOK {
		t.Fatalf("signed status = %d; body=%s", signed.Code, signed.Body.String())
	}
	var failed models.VideoGeneration
	if err := db.DB.First(&failed, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" {
		t.Fatalf("signed webhook status = %q, want failed", failed.Status)
	}

	replayed := performRequest(router, http.MethodPost, "/api/v1/webhooks/generic", body, headers)
	if replayed.Code != http.StatusOK || !strings.Contains(replayed.Body.String(), "duplicate") {
		t.Fatalf("replay was not handled idempotently: status=%d body=%s", replayed.Code, replayed.Body.String())
	}
	conflictingBody := `{"task_id":"provider-task-1","status":"completed","url":"https://cdn.example/video.mp4"}`
	conflicting := performRequest(router, http.MethodPost, "/api/v1/webhooks/generic", conflictingBody, signedWebhookHeaders(conflictingBody, "event-1", time.Now()))
	if conflicting.Code != http.StatusConflict {
		t.Fatalf("conflicting event id status=%d want 409 body=%s", conflicting.Code, conflicting.Body.String())
	}

	expiredHeaders := signedWebhookHeaders(body, "event-2", time.Now().Add(-10*time.Minute))
	expired := performRequest(router, http.MethodPost, "/api/v1/webhooks/generic", body, expiredHeaders)
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expired status = %d, want 401", expired.Code)
	}
}

func TestJobEndpointsListAndCancel(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	video := models.VideoGeneration{Status: "processing", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&video).Error; err != nil {
		t.Fatal(err)
	}
	job, err := sJobForTest(video.ID)
	if err != nil {
		t.Fatal(err)
	}
	list := performRequest(router, http.MethodGet, "/api/v1/jobs?status=running", "", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"id":`+strconv.FormatUint(uint64(job.ID), 10)) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	canceled := performRequest(router, http.MethodPost, "/api/v1/jobs/"+strconv.FormatUint(uint64(job.ID), 10)+"/cancel", "", nil)
	if canceled.Code != http.StatusOK || !strings.Contains(canceled.Body.String(), `"status":"canceled"`) {
		t.Fatalf("cancel status=%d body=%s", canceled.Code, canceled.Body.String())
	}
}

func sJobForTest(targetID uint) (*models.GenerationJob, error) {
	service := jobs.New(db.DB)
	return service.CreateForTarget("video.generate", "video_generation", targetID, "mock", nil)
}

func TestImageUploadRejectsFakePayload(t *testing.T) {
	router := testRouter(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "payload.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("<html>not an image</html>"))
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/upload/image", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	responseRecorder := httptest.NewRecorder()
	router.ServeHTTP(responseRecorder, request)
	if responseRecorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("fake image status=%d body=%s", responseRecorder.Code, responseRecorder.Body.String())
	}

	var valid bytes.Buffer
	if err := png.Encode(&valid, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	body.Reset()
	writer = multipart.NewWriter(&body)
	part, err = writer.CreateFormFile("file", "valid.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(valid.Bytes())
	_ = writer.Close()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/upload/image", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	responseRecorder = httptest.NewRecorder()
	router.ServeHTTP(responseRecorder, request)
	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("valid image status=%d body=%s", responseRecorder.Code, responseRecorder.Body.String())
	}
}

func TestImageUploadRejectsMultipleBindingTargets(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "upload drama", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{DramaID: drama.ID, Name: "person", CreatedAt: now, UpdatedAt: now}
	scene := models.Scene{DramaID: drama.ID, Location: "room", Time: "day", Prompt: "room", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}

	responseRecorder := performImageUpload(t, router, map[string]string{
		"drama_id":     itoa(drama.ID),
		"character_id": itoa(character.ID),
		"scene_id":     itoa(scene.ID),
	})
	if responseRecorder.Code != http.StatusBadRequest || !strings.Contains(responseRecorder.Body.String(), "one binding target") {
		t.Fatalf("status=%d body=%s", responseRecorder.Code, responseRecorder.Body.String())
	}
}

func TestImageUploadBindsPropAndRegistersAsset(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "prop upload", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	prop := models.Prop{DramaID: drama.ID, Name: "watch", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&prop).Error; err != nil {
		t.Fatal(err)
	}

	responseRecorder := performImageUpload(t, router, map[string]string{
		"drama_id": itoa(drama.ID), "prop_id": itoa(prop.ID), "name": "watch reference",
	})
	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", responseRecorder.Code, responseRecorder.Body.String())
	}
	if err := db.DB.First(&prop, prop.ID).Error; err != nil {
		t.Fatal(err)
	}
	if prop.ImageURL == "" {
		t.Fatal("prop image was not bound")
	}
	var asset models.Asset
	if err := db.DB.Where("drama_id = ? AND category = ?", drama.ID, "prop").First(&asset).Error; err != nil {
		t.Fatalf("registered asset: %v", err)
	}
}

func TestImageUploadRejectsCharacterFromAnotherDrama(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	dramaA := models.Drama{Title: "character A", Status: "draft", CreatedAt: now, UpdatedAt: now}
	dramaB := models.Drama{Title: "character B", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&dramaA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&dramaB).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{DramaID: dramaA.ID, Name: "person", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&character).Error; err != nil {
		t.Fatal(err)
	}

	responseRecorder := performImageUpload(t, router, map[string]string{
		"drama_id": itoa(dramaB.ID), "character_id": itoa(character.ID),
	})
	if responseRecorder.Code != http.StatusBadRequest || !strings.Contains(responseRecorder.Body.String(), "character does not belong to drama") {
		t.Fatalf("status=%d body=%s", responseRecorder.Code, responseRecorder.Body.String())
	}
}

func TestCreateAssetRejectsMissingAndCrossDramaOwnership(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	dramaA := models.Drama{Title: "A", Status: "draft", CreatedAt: now, UpdatedAt: now}
	dramaB := models.Drama{Title: "B", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&dramaA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&dramaB).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: dramaA.ID, EpisodeNumber: 1, Title: "A1", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{EpisodeID: episode.ID, StoryboardNumber: 1, Status: "pending", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}

	cross := performRequest(router, http.MethodPost, "/api/v1/assets", `{
		"drama_id":`+itoa(dramaB.ID)+`,"storyboard_id":`+itoa(storyboard.ID)+`,
		"name":"cross","type":"image","url":"/static/cross.png"
	}`, nil)
	if cross.Code != http.StatusBadRequest || !strings.Contains(cross.Body.String(), "storyboard does not belong to drama") {
		t.Fatalf("cross status=%d body=%s", cross.Code, cross.Body.String())
	}

	missing := performRequest(router, http.MethodPost, "/api/v1/assets", `{
		"drama_id":999999,"name":"orphan","type":"image","url":"/static/orphan.png"
	}`, nil)
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), "drama not found") {
		t.Fatalf("missing status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func performImageUpload(t *testing.T, handler http.Handler, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var imageBody bytes.Buffer
	if err := png.Encode(&imageBody, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "valid.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(imageBody.Bytes())
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/upload/image", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	responseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(responseRecorder, request)
	return responseRecorder
}

func TestAssetLibraryWorkflow(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "asset drama", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "episode", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{EpisodeID: episode.ID, StoryboardNumber: 1, Title: "shot", Status: "pending", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	ownedImage := models.ImageGeneration{ImageURL: "/static/reference.png", LocalPath: "reference.png", Status: "completed", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&ownedImage).Error; err != nil {
		t.Fatal(err)
	}

	created := performRequest(router, http.MethodPost, "/api/v1/assets", `{
		"drama_id":`+itoa(drama.ID)+`,"episode_id":`+itoa(episode.ID)+`,
		"name":"reference","type":"image","category":"reference",
		"url":"/static/reference.png","mime_type":"image/png"
	}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	assetID := uint(decodeResponse(t, created)["data"].(map[string]any)["id"].(float64))

	listed := performRequest(router, http.MethodGet, "/api/v1/assets?drama_id="+itoa(drama.ID)+"&episode_id="+itoa(episode.ID)+"&type=image&category=reference", "", nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"name":"reference"`) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}

	updated := performRequest(router, http.MethodPut, "/api/v1/assets/"+itoa(assetID), `{
		"name":"favorite reference","description":"reusable frame","is_favorite":true
	}`, nil)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"is_favorite":true`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}

	applied := performRequest(router, http.MethodPost, "/api/v1/assets/"+itoa(assetID)+"/apply", `{
		"storyboard_id":`+itoa(storyboard.ID)+`,"frame_type":"last_frame"
	}`, nil)
	if applied.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", applied.Code, applied.Body.String())
	}
	if err := db.DB.First(&storyboard, storyboard.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storyboard.LastFrameImage != "/static/reference.png" {
		t.Fatalf("last frame=%q", storyboard.LastFrameImage)
	}

	deleted := performRequest(router, http.MethodDelete, "/api/v1/assets/"+itoa(assetID), "", nil)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	listed = performRequest(router, http.MethodGet, "/api/v1/assets?drama_id="+itoa(drama.ID), "", nil)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), `"id":`+itoa(assetID)) {
		t.Fatalf("deleted asset remained in list: %s", listed.Body.String())
	}
}

func jsonNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
