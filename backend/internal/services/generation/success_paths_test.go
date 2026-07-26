package generation

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	"github.com/eqzhou/flyaimovie/internal/storage"
)

// mockProviderPNG writes a real PNG into the allow-listed mock directory and returns a
// file:// URL the media fetcher will accept.
func mockProviderPNG(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "flyaimovie-mock")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var payload bytes.Buffer
	if err := png.Encode(&payload, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, payload.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	return "file://" + path
}

func mockProviderMP4(t *testing.T, name string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for video fixtures")
	}
	dir := filepath.Join(os.TempDir(), "flyaimovie-mock")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	output, err := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "color=c=navy:s=160x90:d=0.2", "-c:v", "libx264", "-pix_fmt", "yuv420p", path).CombinedOutput()
	if err != nil {
		t.Fatalf("create provider video: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	return "file://" + path
}

// A successful owned finalize must complete the record, succeed the leased job, run the
// storyboard side effects, and register the asset — all inside the job transaction.
func TestImageFinalizeOwnedCompletesJobAndSideEffects(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	store := storage.NewLocal(t.TempDir())
	now := response.Now()
	storyboard := models.Storyboard{OrganizationID: 1, EpisodeID: 1, StoryboardNumber: 1, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	record := &models.ImageGeneration{OrganizationID: 1, StoryboardID: &storyboard.ID, FrameType: "last_frame", Prompt: "owned finalize", ImageType: "storyboard_frame", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	job, err := jobService.CreateQueuedOrganization(1, "image.generate", "image_generation", record.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := jobService.ClaimWaiting("worker", 1); err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	service := &ImageService{Store: store, Jobs: jobService, Cache: mediacache.New(database, store)}

	if err := service.FinalizeOwned(context.Background(), record, mockProviderPNG(t, "owned-finalize.png"), job.ID, "worker"); err != nil {
		t.Fatal(err)
	}

	if record.Status != "completed" || record.LocalPath == "" || record.CompletedAt == nil {
		t.Fatalf("record=%+v, want a completed record", record)
	}
	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusSucceeded {
		t.Fatalf("job=%+v, want the owned job succeeded", current)
	}
	var storedShot models.Storyboard
	if err := database.First(&storedShot, storyboard.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedShot.LastFrameImage != record.ImageURL {
		t.Fatalf("storyboard=%+v, want last_frame_image set to the generated URL", storedShot)
	}
	var asset models.Asset
	if err := database.Where("organization_id = ? AND image_gen_id = ?", 1, record.ID).First(&asset).Error; err != nil {
		t.Fatalf("asset not registered: %v", err)
	}
}

// The ownerless finalize path still has to complete the record, apply side effects and
// close out the job that tracks the target.
func TestImageFinalizeWithoutOwnerCompletesTrackedJob(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	store := storage.NewLocal(t.TempDir())
	now := response.Now()
	character := models.Character{OrganizationID: 1, DramaID: 2, Name: "阿宁", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	record := &models.ImageGeneration{OrganizationID: 1, CharacterID: &character.ID, Prompt: "portrait", ImageType: "character", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	job, err := jobService.CreateForTargetOrganization(1, "image.generate", "image_generation", record.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	service := &ImageService{Store: store, Jobs: jobService}

	if err := service.Finalize(context.Background(), record, mockProviderPNG(t, "unowned-finalize.png")); err != nil {
		t.Fatal(err)
	}

	if record.Status != "completed" {
		t.Fatalf("record=%+v, want a completed record", record)
	}
	var storedCharacter models.Character
	if err := database.First(&storedCharacter, character.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedCharacter.ImageURL != record.ImageURL || storedCharacter.LocalPath != record.LocalPath {
		t.Fatalf("character=%+v, want the portrait applied", storedCharacter)
	}
	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusSucceeded {
		t.Fatalf("job=%+v, want the tracked job succeeded", current)
	}
}

// A successful owned video finalize must complete the record, mark the storyboard
// video-ready and succeed the leased job.
func TestVideoFinalizeOwnedCompletesJobAndStoryboard(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	store := storage.NewLocal(t.TempDir())
	now := response.Now()
	storyboard := models.Storyboard{OrganizationID: 1, EpisodeID: 1, StoryboardNumber: 1, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	record := &models.VideoGeneration{OrganizationID: 1, StoryboardID: &storyboard.ID, Prompt: "owned video", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	job, err := jobService.CreateQueuedOrganization(1, "video.generate", "video_generation", record.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := jobService.ClaimWaiting("worker", 1); err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	service := &VideoService{Store: store, Jobs: jobService, Cache: mediacache.New(database, store)}

	if err := service.FinalizeAuthorizedOwned(context.Background(), record, mockProviderMP4(t, "owned-finalize.mp4"), "", job.ID, "worker"); err != nil {
		t.Fatal(err)
	}

	if record.Status != "completed" || record.LocalPath == "" {
		t.Fatalf("record=%+v, want a completed record", record)
	}
	var storedShot models.Storyboard
	if err := database.First(&storedShot, storyboard.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedShot.VideoURL != record.VideoURL || storedShot.Status != "video_ready" {
		t.Fatalf("storyboard=%+v, want the shot marked video_ready", storedShot)
	}
	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusSucceeded {
		t.Fatalf("job=%+v, want the owned job succeeded", current)
	}
	var asset models.Asset
	if err := database.Where("organization_id = ? AND video_gen_id = ?", 1, record.ID).First(&asset).Error; err != nil {
		t.Fatalf("asset not registered: %v", err)
	}
}

// Finalizing without any job must still persist the record and register the asset.
func TestVideoFinalizeWithoutJobPersistsRecord(t *testing.T) {
	database := generationDatabase(t)
	store := storage.NewLocal(t.TempDir())
	now := response.Now()
	record := &models.VideoGeneration{OrganizationID: 1, Prompt: "plain video", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	service := &VideoService{Store: store}

	if err := service.Finalize(context.Background(), record, mockProviderMP4(t, "plain-finalize.mp4")); err != nil {
		t.Fatal(err)
	}

	var stored models.VideoGeneration
	if err := database.First(&stored, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "completed" || stored.LocalPath == "" {
		t.Fatalf("stored=%+v, want the completed record persisted", stored)
	}
}

// TTS must pick the voice of the character linked to the shot, and must persist the
// audio URL plus asset for the storyboard.
func TestTTSUsesLinkedCharacterVoiceAndPersistsAudio(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for mock tts")
	}
	database := generationDatabase(t)
	mockAudioConfig(t, 1)
	now := response.Now()
	episode := models.Episode{OrganizationID: 1, DramaID: 5, EpisodeNumber: 1, Title: "第一集", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{OrganizationID: 1, DramaID: 5, Name: "阿宁", VoiceStyle: "voice-anning", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{OrganizationID: 1, EpisodeID: episode.ID, StoryboardNumber: 1, Dialogue: "阿宁：我回来了", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&models.StoryboardCharacter{OrganizationID: 1, StoryboardID: storyboard.ID, CharacterID: character.ID}).Error; err != nil {
		t.Fatal(err)
	}
	store := storage.NewLocal(t.TempDir())
	service := &TTSService{Store: store, Cache: mediacache.New(database, store)}

	url, err := service.GenerateForStoryboardOrganization(context.Background(), 1, storyboard.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if url == "" {
		t.Fatal("empty audio URL")
	}
	var storedShot models.Storyboard
	if err := database.First(&storedShot, storyboard.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedShot.TTSAudioURL != url {
		t.Fatalf("storyboard=%+v, want tts_audio_url set to %q", storedShot, url)
	}
	var asset models.Asset
	if err := database.Where("organization_id = ? AND storyboard_id = ? AND category = ?", 1, storyboard.ID, "tts").First(&asset).Error; err != nil {
		t.Fatalf("tts asset not registered: %v", err)
	}
}

// When the shot has no character links, the speaker is resolved against the drama roster
// reached through the episode.
func TestTTSFallsBackToDramaRosterForVoice(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for mock tts")
	}
	database := generationDatabase(t)
	mockAudioConfig(t, 1)
	now := response.Now()
	episode := models.Episode{OrganizationID: 1, DramaID: 9, EpisodeNumber: 1, Title: "第一集", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{OrganizationID: 1, DramaID: 9, Name: "老周", VoiceStyle: "voice-laozhou", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{OrganizationID: 1, EpisodeID: episode.ID, StoryboardNumber: 1, Dialogue: "老周：走吧", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	store := storage.NewLocal(t.TempDir())
	service := &TTSService{Store: store}

	if _, err := service.GenerateForStoryboardOrganization(context.Background(), 1, storyboard.ID, nil); err != nil {
		t.Fatal(err)
	}
	var storedShot models.Storyboard
	if err := database.First(&storedShot, storyboard.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedShot.TTSAudioURL == "" {
		t.Fatalf("storyboard=%+v, want the audio URL persisted", storedShot)
	}
}

// An owned TTS job must succeed the leased job when the audio is produced.
func TestTTSOwnedJobSucceedsLeasedJob(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for mock tts")
	}
	database := generationDatabase(t)
	mockAudioConfig(t, 1)
	jobService := jobs.New(database)
	now := response.Now()
	storyboard := models.Storyboard{OrganizationID: 1, EpisodeID: 1, StoryboardNumber: 1, Dialogue: "阿宁：你好", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	job, err := jobService.CreateQueuedOrganization(1, "tts.generate", "storyboard_tts", storyboard.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := jobService.ClaimWaiting("tts-worker", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	runner := &AsyncRunner{Jobs: jobService, TTS: &TTSService{Store: storage.NewLocal(t.TempDir())}}

	runner.runTTSJob(claimed[0])

	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusSucceeded {
		t.Fatalf("job=%+v, want the TTS job succeeded", current)
	}
}

// A TTS job whose storyboard was deleted must fail rather than hang the lease.
func TestRunTTSJobFailsWhenStoryboardMissing(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	job, err := jobService.CreateQueuedOrganization(1, "tts.generate", "storyboard_tts", 4242, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := jobService.ClaimWaiting("tts-worker", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	runner := &AsyncRunner{Jobs: jobService, TTS: &TTSService{Store: storage.NewLocal(t.TempDir())}}

	runner.runTTSJob(claimed[0])

	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusFailed || current.LastError == "" {
		t.Fatalf("job=%+v, want the job failed for a missing storyboard", current)
	}
}

// cacheGeneratedFile must surface a hashing failure for a file that is not on disk.
func TestCacheGeneratedFileReportsMissingFile(t *testing.T) {
	store := storage.NewLocal(t.TempDir())

	if _, _, _, _, err := cacheGeneratedFile(nil, store, 1, "image_generation", 1, "image", "images/missing.png", "/static/images/missing.png", "image/png"); err == nil {
		t.Fatal("hashing a missing file was accepted")
	}
}

// The async image submission path must persist the provider task id and park the job in
// waiting_provider rather than completing it.
func TestImageGenerateAsyncParksJobForPolling(t *testing.T) {
	database := generationDatabase(t)
	server := newTrustedProviderTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = w.Write([]byte(`{"id":"async-parked-1"}`))
	}))
	defer server.Close()
	now := response.Now()
	config := models.AIServiceConfig{OrganizationID: 1, ServiceType: "image", Provider: "volcengine", Name: "async", BaseURL: server.URL, APIKey: "key", Model: "seedream", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	jobService := jobs.New(database)
	service := &ImageService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}
	record := &models.ImageGeneration{OrganizationID: 1, Prompt: "async submit", ImageType: "storyboard_frame"}

	if err := service.Generate(context.Background(), record, &config.ID); err != nil {
		t.Fatal(err)
	}

	if record.TaskID != "async-parked-1" || record.Status != "processing" {
		t.Fatalf("record=%+v, want the provider task id recorded", record)
	}
	job, err := jobService.Get(*record.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != jobs.StatusWaitingProvider || job.ProviderTaskID != "async-parked-1" {
		t.Fatalf("job=%+v, want the job parked for polling", job)
	}
}

// applyImageSideEffects must refuse to touch a scene owned by another organization.
func TestApplyImageSideEffectsIsScopedForScenesAndProps(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	scene := models.Scene{OrganizationID: 2, DramaID: 1, Location: "站台", CreatedAt: now, UpdatedAt: now}
	prop := models.Prop{OrganizationID: 2, DramaID: 1, Name: "提箱", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&scene, &prop} {
		if err := database.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	record := &models.ImageGeneration{OrganizationID: 1, SceneID: &scene.ID, PropID: &prop.ID, ImageURL: "/static/images/new.png", LocalPath: "images/new.png"}

	if err := applyImageSideEffects(database, record); err != nil {
		t.Fatal(err)
	}

	var storedScene models.Scene
	if err := database.First(&storedScene, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	var storedProp models.Prop
	if err := database.First(&storedProp, prop.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedScene.ImageURL != "" || storedProp.ImageURL != "" {
		t.Fatalf("scene=%+v prop=%+v, want cross-organization rows untouched", storedScene, storedProp)
	}
}

// A scene and prop in the caller's organization must be updated, including the scene
// status transition.
func TestApplyImageSideEffectsUpdatesOwnedSceneAndProp(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	scene := models.Scene{OrganizationID: 1, DramaID: 1, Location: "站台", CreatedAt: now, UpdatedAt: now}
	prop := models.Prop{OrganizationID: 1, DramaID: 1, Name: "提箱", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&scene, &prop} {
		if err := database.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	record := &models.ImageGeneration{OrganizationID: 1, SceneID: &scene.ID, PropID: &prop.ID, ImageURL: "/static/images/new.png", LocalPath: "images/new.png"}

	if err := applyImageSideEffects(database, record); err != nil {
		t.Fatal(err)
	}

	var storedScene models.Scene
	if err := database.First(&storedScene, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	var storedProp models.Prop
	if err := database.First(&storedProp, prop.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedScene.ImageURL != record.ImageURL || storedScene.Status != "completed" {
		t.Fatalf("scene=%+v, want the scene image and status applied", storedScene)
	}
	if storedProp.ImageURL != record.ImageURL || storedProp.LocalPath != record.LocalPath {
		t.Fatalf("prop=%+v, want the prop image applied", storedProp)
	}
}
