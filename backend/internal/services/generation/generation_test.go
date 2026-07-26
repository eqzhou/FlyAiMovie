package generation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"github.com/eqzhou/flyaimovie/internal/testsupport"
	"gorm.io/gorm"
)

func newTrustedProviderTLSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	certificate := server.Certificate()
	caPath := filepath.Join(t.TempDir(), "provider-ca.pem")
	payload := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
	if err := os.WriteFile(caPath, payload, 0o600); err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Setenv("AI_PROVIDER_CA_FILE", caPath)
	t.Setenv("AI_PROVIDER_PRIVATE_HOSTS", "127.0.0.1")
	return server
}

func TestVideoServiceForwardsStructuredReferenceImages(t *testing.T) {
	database := generationDatabase(t)
	server := newTrustedProviderTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		images, _ := body["images"].([]any)
		if len(images) != 2 || images[0] != "https://cdn.example/a.png" || images[1] != "https://cdn.example/b.png" {
			t.Errorf("images=%#v", images)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_id":"vidu-multi-ref"}`))
	}))
	defer server.Close()
	now := response.Now()
	config := models.AIServiceConfig{OrganizationID: 1, ServiceType: "video", Provider: "vidu", Name: "vidu", BaseURL: server.URL, APIKey: "secret", Model: "vidu2.0", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	references, _ := json.Marshal([]string{"https://cdn.example/a.png", "https://cdn.example/b.png"})
	record := &models.VideoGeneration{OrganizationID: 1, Prompt: "consistent subject", ReferenceMode: "multi_ref", ReferenceImageURLs: string(references)}
	service := &VideoService{Store: storage.NewLocal(t.TempDir()), Jobs: jobs.New(database)}
	if err := service.Generate(context.Background(), record, &config.ID); err != nil {
		t.Fatal(err)
	}
	if record.TaskID != "vidu-multi-ref" || record.Status != "processing" {
		t.Fatalf("record=%+v", record)
	}
}

func TestParseVideoReferenceURLsRejectsInvalidOrExcessiveInput(t *testing.T) {
	dataURI := "data:image/png;base64,AAAA"
	parsed, err := parseVideoReferenceURLs(dataURI)
	if err != nil || len(parsed) != 1 || parsed[0] != dataURI {
		t.Fatalf("data URI parsed as %#v with error %v", parsed, err)
	}
	if _, err := parseVideoReferenceURLs(`["https://cdn.example/a.png",3]`); err == nil {
		t.Fatal("mixed JSON reference array accepted")
	}
	values := make([]string, 9)
	for i := range values {
		values[i] = "https://cdn.example/reference.png"
	}
	raw, _ := json.Marshal(values)
	if _, err := parseVideoReferenceURLs(string(raw)); err == nil {
		t.Fatal("excessive reference array accepted")
	}
}

func TestParseImageReferenceURLsSupportsDataJSONAndLegacyLists(t *testing.T) {
	dataURI := "data:image/png;base64,AAAA"
	for _, tc := range []struct {
		raw  string
		want []string
	}{
		{"", []string{}},
		{dataURI, []string{dataURI}},
		{`["https://cdn.example/a.png", "https://cdn.example/b.png"]`, []string{"https://cdn.example/a.png", "https://cdn.example/b.png"}},
		{" https://cdn.example/a.png, https://cdn.example/b.png ", []string{"https://cdn.example/a.png", "https://cdn.example/b.png"}},
	} {
		got, err := parseImageReferenceURLs(tc.raw)
		if err != nil || len(got) != len(tc.want) {
			t.Fatalf("parse %q = %#v, %v", tc.raw, got, err)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("parse %q = %#v", tc.raw, got)
			}
		}
	}
	if _, err := parseImageReferenceURLs(`["https://cdn.example/a.png", 3]`); err == nil {
		t.Fatal("mixed reference array accepted")
	}
	if _, err := parseImageReferenceURLs(strings.Repeat("https://cdn.example/a.png,", 8) + "https://cdn.example/a.png"); err == nil {
		t.Fatal("excessive reference list accepted")
	}
}

func TestImageBase64ValidationUsesDetectedContentType(t *testing.T) {
	service := &ImageService{Store: storage.NewLocal(t.TempDir())}
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	rel, err := service.saveBase64(base64.StdEncoding.EncodeToString(imageData.Bytes()), "image/jpeg", "images")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(rel) != ".png" {
		t.Fatalf("extension=%q", filepath.Ext(rel))
	}
	if _, err := service.saveBase64(base64.StdEncoding.EncodeToString([]byte("<script>alert(1)</script>")), "image/png", "images"); err == nil {
		t.Fatal("non-image provider payload accepted")
	}
	if _, err := service.saveBase64("not-base64", "image/png", "images"); err == nil {
		t.Fatal("invalid base64 accepted")
	}
}

// A failed disk write must surface. Swallowing it returns a path to a file that
// does not exist, so the failure only shows up later during hashing or caching.
func TestImageBase64ReportsStorageFailure(t *testing.T) {
	root := t.TempDir()
	service := &ImageService{Store: storage.NewLocal(root)}
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	payload := base64.StdEncoding.EncodeToString(imageData.Bytes())

	// Occupy the target subdirectory name with a regular file so creating the
	// directory, and therefore the write, cannot succeed.
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	rel, err := service.saveBase64(payload, "image/png", "blocked")
	if err == nil {
		t.Fatalf("expected a storage failure to be reported, got rel=%q", rel)
	}
	if rel != "" {
		t.Fatalf("failed save must not return a path, got %q", rel)
	}
}

func generationDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	database := testsupport.OpenDatabase(t)
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

func TestImageSideEffectsUpdateScenePropAndStoryboardFrames(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	scene := models.Scene{OrganizationID: 1, DramaID: 1, Location: "scene", CreatedAt: now, UpdatedAt: now}
	prop := models.Prop{OrganizationID: 1, DramaID: 1, Name: "prop", CreatedAt: now, UpdatedAt: now}
	storyboard := models.Storyboard{OrganizationID: 1, EpisodeID: 1, StoryboardNumber: 1, CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&scene, &prop, &storyboard} {
		if err := database.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := &ImageService{}
	service.ApplySideEffects(&models.ImageGeneration{OrganizationID: 1, SceneID: &scene.ID, PropID: &prop.ID, StoryboardID: &storyboard.ID, FrameType: "last_frame", ImageURL: "/static/last.png", LocalPath: "last.png"})
	if err := database.First(&scene, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.First(&prop, prop.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.First(&storyboard, storyboard.ID).Error; err != nil {
		t.Fatal(err)
	}
	if scene.ImageURL != "/static/last.png" || scene.Status != "completed" || prop.ImageURL != "/static/last.png" || storyboard.LastFrameImage != "/static/last.png" {
		t.Fatalf("scene=%+v prop=%+v storyboard=%+v", scene, prop, storyboard)
	}
	service.ApplySideEffects(&models.ImageGeneration{OrganizationID: 1, StoryboardID: &storyboard.ID, FrameType: "composed", ImageURL: "/static/composed.png"})
	if err := database.First(&storyboard, storyboard.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storyboard.ComposedImage != "/static/composed.png" {
		t.Fatalf("composed image=%q", storyboard.ComposedImage)
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

func TestCacheGeneratedFileHashesAndDeduplicates(t *testing.T) {
	database := generationDatabase(t)
	store := storage.NewLocal(t.TempDir())
	if _, _, _, _, err := cacheGeneratedFile(nil, nil, 1, "image_generation", 1, "image", "missing", "", "image/png"); err == nil {
		t.Fatal("missing storage accepted")
	}
	first, _, err := store.SaveBytes("images", "first.png", []byte("identical"))
	if err != nil {
		t.Fatal(err)
	}
	firstPath, firstURL, hash, size, err := cacheGeneratedFile(nil, store, 1, "image_generation", 1, "image", first, store.PublicURL(first), "image/png")
	if err != nil || firstPath != first || firstURL != store.PublicURL(first) || hash == "" || size != 9 {
		t.Fatalf("path=%q url=%q hash=%q size=%d err=%v", firstPath, firstURL, hash, size, err)
	}
	cache := mediacache.New(database, store)
	if _, _, _, _, err := cacheGeneratedFile(cache, store, 1, "image_generation", 1, "image", first, store.PublicURL(first), "image/png"); err != nil {
		t.Fatal(err)
	}
	second, secondAbsolute, err := store.SaveBytes("images", "second.png", []byte("identical"))
	if err != nil {
		t.Fatal(err)
	}
	canonical, _, _, _, err := cacheGeneratedFile(cache, store, 1, "image_generation", 2, "image", second, store.PublicURL(second), "image/png")
	if err != nil || canonical != first {
		t.Fatalf("canonical=%q first=%q err=%v", canonical, first, err)
	}
	if _, err := os.Stat(secondAbsolute); !os.IsNotExist(err) {
		t.Fatalf("duplicate file still exists: %v", err)
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
	legacyURL, err := service.GenerateForStoryboard(context.Background(), storyboard.ID, &config.ID)
	if err != nil || legacyURL == "" {
		t.Fatalf("legacy storyboard TTS=%q err=%v", legacyURL, err)
	}
}

func TestTTSVoiceSampleAndLocalFileHelpers(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for mock tts")
	}
	database := generationDatabase(t)
	now := response.Now()
	config := models.AIServiceConfig{OrganizationID: 6, ServiceType: "audio", Provider: "mock", Name: "mock-audio", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	legacyConfig := config
	legacyConfig.ID = 0
	legacyConfig.OrganizationID = 0
	legacyConfig.Name = "legacy-mock-audio"
	if err := database.Create(&legacyConfig).Error; err != nil {
		t.Fatal(err)
	}
	store := storage.NewLocal(t.TempDir())
	service := &TTSService{Store: store}
	url, err := service.GenerateVoiceSampleOrganization(context.Background(), 6, "林岚", "mock-voice", &config.ID)
	if err != nil {
		t.Fatal(err)
	}
	abs, err := EnsureLocalFile(store, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatal(err)
	}
	previewURL, err := service.GenerateVoicePreviewOrganization(context.Background(), 6, "自定义试听文本", "mock-voice", &config.ID)
	if err != nil || previewURL == "" {
		t.Fatalf("voice preview=%q err=%v", previewURL, err)
	}
	if !HasTTSContent("林岚：出发") || HasTTSContent("环境音：风声") {
		t.Fatal("unexpected TTS content classification")
	}
	if _, err := EnsureLocalFile(store, ""); err == nil {
		t.Fatal("empty local path accepted")
	}
	if _, err := service.generateForStoryboard(context.Background(), nil, &config.ID, 6, nil, 0, ""); err == nil {
		t.Fatal("nil storyboard accepted")
	}
	if _, err := service.GenerateForStoryboardOrganization(context.Background(), 6, 999, &config.ID); err == nil {
		t.Fatal("missing storyboard accepted")
	}
	if _, err := service.GenerateForStoryboard(context.Background(), 999, &config.ID); err == nil {
		t.Fatal("legacy entrypoint accepted missing storyboard")
	}
	legacyURL, err := service.GenerateVoiceSample(context.Background(), "旧角色", "mock-voice", &legacyConfig.ID)
	if err != nil || legacyURL == "" {
		t.Fatalf("legacy voice sample=%q err=%v", legacyURL, err)
	}
}

func TestImageSaveUpload(t *testing.T) {
	service := &ImageService{Store: storage.NewLocal(t.TempDir())}
	rel, err := service.SaveUpload(strings.NewReader("upload"), "reference.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rel, "uploads/") {
		t.Fatalf("relative path=%q", rel)
	}
}

func TestGenerationRejectsMalformedReferenceArraysAndFailsJobs(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	imageConfig := models.AIServiceConfig{OrganizationID: 14, ServiceType: "image", Provider: "mock", Name: "mock-image", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now}
	videoConfig := models.AIServiceConfig{OrganizationID: 14, ServiceType: "video", Provider: "mock", Name: "mock-video", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&imageConfig).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&videoConfig).Error; err != nil {
		t.Fatal(err)
	}
	jobService := jobs.New(database)
	imageRecord := &models.ImageGeneration{OrganizationID: 14, Prompt: "image", ReferenceImages: `["valid", 3]`}
	imageService := &ImageService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}
	if err := imageService.Generate(context.Background(), imageRecord, &imageConfig.ID); err == nil {
		t.Fatal("malformed image references accepted")
	}
	videoRecord := &models.VideoGeneration{OrganizationID: 14, Prompt: "video", ReferenceImageURLs: `["valid", 3]`}
	videoService := &VideoService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}
	if err := videoService.Generate(context.Background(), videoRecord, &videoConfig.ID); err == nil {
		t.Fatal("malformed video references accepted")
	}
	for _, jobID := range []*uint{imageRecord.JobID, videoRecord.JobID} {
		if jobID == nil {
			t.Fatal("failed generation did not create a job")
		}
		job, err := jobService.Get(*jobID)
		if err != nil || job.Status != jobs.StatusFailed || job.LastError == "" {
			t.Fatalf("job=%+v err=%v", job, err)
		}
	}
}

func TestFinalizeRejectsUnsafeProviderFilesAndFailsJobs(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	now := response.Now()
	imageRecord := &models.ImageGeneration{OrganizationID: 15, Status: "processing", CreatedAt: now, UpdatedAt: now}
	videoRecord := &models.VideoGeneration{OrganizationID: 15, Status: "processing", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(imageRecord).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(videoRecord).Error; err != nil {
		t.Fatal(err)
	}
	imageJob, err := jobService.CreateForTargetOrganization(15, "image.generate", "image_generation", imageRecord.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	videoJob, err := jobService.CreateForTargetOrganization(15, "video.generate", "video_generation", videoRecord.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	imageRecord.JobID = &imageJob.ID
	videoRecord.JobID = &videoJob.ID
	if err := (&ImageService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}).Finalize(context.Background(), imageRecord, "file:///etc/hosts"); err == nil {
		t.Fatal("unsafe image file accepted")
	}
	if err := (&VideoService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}).Finalize(context.Background(), videoRecord, "file:///etc/hosts"); err == nil {
		t.Fatal("unsafe video file accepted")
	}
	for _, id := range []uint{imageJob.ID, videoJob.ID} {
		job, err := jobService.Get(id)
		if err != nil || job.Status != jobs.StatusFailed {
			t.Fatalf("job=%+v err=%v", job, err)
		}
	}
}

func TestGenerationRequiresExistingOrganizationConfig(t *testing.T) {
	generationDatabase(t)
	missing := uint(999)
	imageRecord := &models.ImageGeneration{OrganizationID: 20, Prompt: "image"}
	if err := (&ImageService{Store: storage.NewLocal(t.TempDir())}).Generate(context.Background(), imageRecord, &missing); err == nil {
		t.Fatal("image generation accepted a missing config")
	}
	videoRecord := &models.VideoGeneration{OrganizationID: 20, Prompt: "video"}
	if err := (&VideoService{Store: storage.NewLocal(t.TempDir())}).Generate(context.Background(), videoRecord, &missing); err == nil {
		t.Fatal("video generation accepted a missing config")
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
	server := newTrustedProviderTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
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
	server := newTrustedProviderTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
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
