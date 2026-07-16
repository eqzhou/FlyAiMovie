package generation

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"gorm.io/gorm"
)

func generationDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/generation.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestImageServiceMockGenerationPersistsCompleteChain(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	config := models.AIServiceConfig{OrganizationID: 1, ServiceType: "image", Provider: "mock", Name: "mock", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{OrganizationID: 1, DramaID: 3, Name: "测试角色", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	store := storage.NewLocal(t.TempDir())
	service := &ImageService{Store: store, Jobs: jobs.New(database)}
	dramaID := uint(3)
	record := &models.ImageGeneration{OrganizationID: 1, DramaID: &dramaID, CharacterID: &character.ID, Prompt: "independent clean-room test image", ImageType: "character"}
	if err := service.Generate(context.Background(), record, &config.ID); err != nil {
		t.Fatal(err)
	}
	if record.Status != "completed" || record.ImageURL == "" || record.LocalPath == "" || record.JobID == nil {
		t.Fatalf("incomplete generation: %+v", record)
	}
	if _, err := os.Stat(store.Root + "/" + record.LocalPath); err != nil {
		t.Fatalf("generated file missing: %v", err)
	}
	var updated models.Character
	if err := database.First(&updated, character.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.ImageURL != record.ImageURL {
		t.Fatalf("character image=%q want %q", updated.ImageURL, record.ImageURL)
	}
	var asset models.Asset
	if err := database.Where("organization_id = ? AND image_gen_id = ?", 1, record.ID).First(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if asset.URL != record.ImageURL {
		t.Fatalf("asset=%+v", asset)
	}
	job, err := service.Jobs.Get(*record.JobID)
	if err != nil || job.Status != jobs.StatusSucceeded {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}

func TestImageSideEffectsRejectForeignOrganizationTarget(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	foreign := models.Character{OrganizationID: 2, DramaID: 2, Name: "foreign", ImageURL: "original", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	service := &ImageService{}
	record := &models.ImageGeneration{OrganizationID: 1, CharacterID: &foreign.ID, ImageURL: "/static/forbidden.png", LocalPath: "forbidden.png"}
	service.ApplySideEffects(record)
	if err := database.First(&foreign, foreign.ID).Error; err != nil {
		t.Fatal(err)
	}
	if foreign.ImageURL != "original" {
		t.Fatalf("foreign character was modified: %+v", foreign)
	}
}

func TestRegisterAssetDeduplicatesWithinOrganization(t *testing.T) {
	database := generationDatabase(t)
	registerAsset(models.Asset{OrganizationID: 1, Name: "first", Type: "image", URL: "/static/a.png"})
	registerAsset(models.Asset{OrganizationID: 1, Name: "duplicate", Type: "image", URL: "/static/a.png"})
	registerAsset(models.Asset{OrganizationID: 2, Name: "other tenant", Type: "image", URL: "/static/a.png"})
	var count int64
	if err := database.Model(&models.Asset{}).Where("url = ?", "/static/a.png").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("asset count=%d want 2", count)
	}
}

func TestParseDialogue(t *testing.T) {
	tests := []struct {
		input, speaker, text string
		ignored              bool
	}{
		{input: "林岚：我们出发。", speaker: "林岚", text: "我们出发。"},
		{input: "旁白（低声）：夜色降临", speaker: "旁白", text: "夜色降临"},
		{input: "环境音：风声", speaker: "环境音", text: "风声", ignored: true},
		{input: "无对白", text: "无对白", ignored: true},
		{input: "  ", ignored: true},
	}
	for _, test := range tests {
		speaker, text, ignored := parseDialogue(test.input)
		if speaker != test.speaker || text != test.text || ignored != test.ignored {
			t.Fatalf("parse %q = (%q,%q,%v)", test.input, speaker, text, ignored)
		}
	}
}

func TestVideoServiceMockGenerationPersistsCompleteChain(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for mock video")
	}
	database := generationDatabase(t)
	now := response.Now()
	config := models.AIServiceConfig{OrganizationID: 1, ServiceType: "video", Provider: "mock", Name: "mock-video", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{OrganizationID: 1, DramaID: 3, EpisodeNumber: 1, Title: "episode", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{OrganizationID: 1, EpisodeID: episode.ID, StoryboardNumber: 1, Title: "shot", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	store := storage.NewLocal(t.TempDir())
	service := &VideoService{Store: store, Jobs: jobs.New(database)}
	dramaID := uint(3)
	record := &models.VideoGeneration{OrganizationID: 1, DramaID: &dramaID, StoryboardID: &storyboard.ID, Prompt: "independent clean-room test video", Duration: 1}
	if err := service.Generate(context.Background(), record, &config.ID); err != nil {
		t.Fatal(err)
	}
	if record.Status != "completed" || record.VideoURL == "" || record.JobID == nil {
		t.Fatalf("incomplete video: %+v", record)
	}
	if _, err := os.Stat(store.Root + "/" + record.LocalPath); err != nil {
		t.Fatalf("video file missing: %v", err)
	}
	if err := database.First(&storyboard, storyboard.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storyboard.VideoURL != record.VideoURL || storyboard.Status != "video_ready" {
		t.Fatalf("storyboard not updated: %+v", storyboard)
	}
	var asset models.Asset
	if err := database.Where("organization_id = ? AND video_gen_id = ?", 1, record.ID).First(&asset).Error; err != nil {
		t.Fatal(err)
	}
}

func TestTTSServiceMockGenerationPersistsAudioAndAsset(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for mock tts")
	}
	database := generationDatabase(t)
	now := response.Now()
	config := models.AIServiceConfig{OrganizationID: 1, ServiceType: "audio", Provider: "mock", Name: "mock-audio", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{OrganizationID: 1, DramaID: 3, EpisodeNumber: 1, Title: "episode", AudioConfigID: &config.ID, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{OrganizationID: 1, EpisodeID: episode.ID, StoryboardNumber: 1, Title: "shot", Dialogue: "林岚：我们出发。", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{OrganizationID: 1, DramaID: 3, Name: "林岚", VoiceStyle: "mock-voice", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	link := models.StoryboardCharacter{OrganizationID: 1, StoryboardID: storyboard.ID, CharacterID: character.ID}
	if err := database.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	store := storage.NewLocal(t.TempDir())
	service := &TTSService{Store: store}
	url, err := service.GenerateForStoryboardOrganization(context.Background(), 1, storyboard.ID, &config.ID)
	if err != nil {
		t.Fatal(err)
	}
	if url == "" {
		t.Fatal("empty tts url")
	}
	if err := database.First(&storyboard, storyboard.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storyboard.TTSAudioURL != url {
		t.Fatalf("tts url=%q want %q", storyboard.TTSAudioURL, url)
	}
	var asset models.Asset
	if err := database.Where("organization_id = ? AND storyboard_id = ? AND type = ?", 1, storyboard.ID, "audio").First(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if asset.URL != url {
		t.Fatalf("asset=%+v", asset)
	}
}

func TestAsyncImagePollingCompletesPersistentJob(t *testing.T) {
	database := generationDatabase(t)
	var imageData bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 40, G: 80, B: 120, A: 255})
		}
	}
	if err := png.Encode(&imageData, img); err != nil {
		t.Fatal(err)
	}
	mockDir := filepath.Join(os.TempDir(), "flyaimovie-mock")
	if err := os.MkdirAll(mockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	providerFile, err := os.CreateTemp(mockDir, "provider-*.png")
	if err != nil {
		t.Fatal(err)
	}
	providerImage := providerFile.Name()
	if _, err := providerFile.Write(imageData.Bytes()); err != nil {
		providerFile.Close()
		t.Fatal(err)
	}
	if err := providerFile.Close(); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(providerImage)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.Method + " " + request.URL.Path {
		case "POST /api/v3/images/generations":
			_, _ = w.Write([]byte(`{"id":"async-image-1"}`))
		case "GET /api/v3/images/generations/async-image-1":
			_, _ = w.Write([]byte(`{"status":"succeeded","data":[{"url":"file://` + providerImage + `"}]}`))
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	now := response.Now()
	config := models.AIServiceConfig{OrganizationID: 1, ServiceType: "image", Provider: "volcengine", Name: "async-image", BaseURL: server.URL, APIKey: "test-key", Model: "seedream", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	store := storage.NewLocal(t.TempDir())
	jobService := jobs.New(database)
	imageService := &ImageService{Store: store, Jobs: jobService}
	record := &models.ImageGeneration{OrganizationID: 1, Prompt: "async image", ImageType: "storyboard_frame"}
	if err := imageService.Generate(context.Background(), record, &config.ID); err != nil {
		t.Fatal(err)
	}
	if record.TaskID != "async-image-1" || record.JobID == nil {
		t.Fatalf("submission=%+v", record)
	}
	claimed, err := jobService.ClaimWaiting("test-worker", 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	runner := &AsyncRunner{Images: imageService, Jobs: jobService, Store: store}
	runner.pollImageJob(claimed[0])
	if err := database.First(record, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if record.Status != "completed" || record.ImageURL == "" {
		t.Fatalf("polled record=%+v", record)
	}
	job, err := jobService.Get(*record.JobID)
	if err != nil || job.Status != jobs.StatusSucceeded {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	var asset models.Asset
	if err := database.Where("organization_id = ? AND image_gen_id = ?", 1, record.ID).First(&asset).Error; err != nil {
		t.Fatal(err)
	}
}

func TestAsyncVideoPollingCompletesPersistentJob(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for video polling")
	}
	database := generationDatabase(t)
	mockDir := filepath.Join(os.TempDir(), "flyaimovie-mock")
	if err := os.MkdirAll(mockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tempFile, err := os.CreateTemp(mockDir, "provider-*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	tempVideo := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tempVideo); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempVideo)
	output, err := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "color=c=navy:s=160x90:d=0.2", "-c:v", "libx264", "-pix_fmt", "yuv420p", tempVideo).CombinedOutput()
	if err != nil {
		t.Fatalf("create provider video: %v: %s", err, output)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.Method + " " + request.URL.Path {
		case "POST /api/v3/contents/generations/tasks":
			_, _ = w.Write([]byte(`{"id":"async-video-1"}`))
		case "GET /api/v3/contents/generations/tasks/async-video-1":
			_, _ = w.Write([]byte(`{"status":"succeeded","content":{"video_url":"file://` + tempVideo + `"}}`))
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	now := response.Now()
	config := models.AIServiceConfig{OrganizationID: 1, ServiceType: "video", Provider: "volcengine", Name: "async-video", BaseURL: server.URL, APIKey: "test-key", Model: "seedance", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	store := storage.NewLocal(t.TempDir())
	jobService := jobs.New(database)
	videoService := &VideoService{Store: store, Jobs: jobService}
	record := &models.VideoGeneration{OrganizationID: 1, Prompt: "async video", FirstFrameURL: "https://example.test/frame.png"}
	if err := videoService.Generate(context.Background(), record, &config.ID); err != nil {
		t.Fatal(err)
	}
	claimed, err := jobService.ClaimWaiting("test-worker", 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	runner := &AsyncRunner{Videos: videoService, Jobs: jobService, Store: store}
	runner.pollVideoJob(claimed[0])
	if err := database.First(record, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if record.Status != "completed" || record.VideoURL == "" {
		t.Fatalf("polled record=%+v", record)
	}
	job, err := jobService.Get(*record.JobID)
	if err != nil || job.Status != jobs.StatusSucceeded {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}
