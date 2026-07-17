package generation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/storage"
)

func TestValidateOutputRel(t *testing.T) {
	valid := []string{"composed/shot_1.mp4", "merged/episode_2.mp4"}
	for _, rel := range valid {
		if err := validateOutputRel(rel); err != nil {
			t.Fatalf("valid %q rejected: %v", rel, err)
		}
	}
	invalid := []string{"../escape.mp4", "composed/../../escape.mp4", "/tmp/out.mp4", "other/out.mp4", "merged\\..\\escape.mp4"}
	for _, rel := range invalid {
		if err := validateOutputRel(rel); err == nil {
			t.Fatalf("unsafe %q accepted", rel)
		}
	}
}

func TestAsyncRunnerStartAndStopAreIdempotent(t *testing.T) {
	runner := &AsyncRunner{}
	runner.Stop()
	runner.Start()
	runner.Start()
	runner.Stop()
	runner.Stop()
	select {
	case <-runner.stop:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop")
	}
	recoveryDB := generationDatabase(t)
	recoveryJobs := jobs.New(recoveryDB)
	recoverable, err := recoveryJobs.CreateForTargetOrganization(21, "tts.generate", "storyboard_tts", 1, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if err := recoveryDB.Model(recoverable).Update("lease_expires_at", expired).Error; err != nil {
		t.Fatal(err)
	}
	runnerWithRecovery := &AsyncRunner{Jobs: recoveryJobs}
	runnerWithRecovery.Start()
	runnerWithRecovery.Stop()
}

func TestRunComposeJobFailsUnavailableWorkerAndInvalidPayload(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	for index, tc := range []struct {
		store   *storage.LocalStorage
		payload string
	}{
		{nil, `{}`},
		{storage.NewLocal(t.TempDir()), `{not-json`},
	} {
		job, err := jobService.CreateQueuedPayloadOrganization(1, "episode.merge", "episode_merge", uint(index+1), "ffmpeg", nil, tc.payload)
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := jobService.ClaimWaiting("compose-test", 1)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("claim=%v err=%v", claimed, err)
		}
		(&AsyncRunner{Jobs: jobService, Store: tc.store}).runComposeJob(claimed[0], "compose-test")
		current, err := jobService.Get(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status != jobs.StatusFailed || current.LastError == "" {
			t.Fatalf("job=%+v", current)
		}
	}
}

func TestRunTTSJobFailsWithoutWorker(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	job, err := jobService.CreateQueuedOrganization(2, "tts.generate", "storyboard_tts", 17, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := jobService.ClaimWaiting("tts-test", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	(&AsyncRunner{Jobs: jobService}).runTTSJob(claimed[0])
	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusFailed || current.LastError != "tts worker unavailable" {
		t.Fatalf("job=%+v", current)
	}
}

func TestPollClaimedRunsQueuedTTSJob(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for mock tts")
	}
	database := generationDatabase(t)
	now := response.Now()
	config := models.AIServiceConfig{OrganizationID: 12, ServiceType: "audio", Provider: "mock", Name: "mock-audio", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{OrganizationID: 12, DramaID: 4, EpisodeNumber: 1, AudioConfigID: &config.ID, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{OrganizationID: 12, EpisodeID: episode.ID, StoryboardNumber: 1, Dialogue: "旁白：开始", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	jobService := jobs.New(database)
	job, err := jobService.CreateQueuedOrganization(12, "tts.generate", "storyboard_tts", storyboard.ID, "mock", &config.ID)
	if err != nil {
		t.Fatal(err)
	}
	runner := &AsyncRunner{Jobs: jobService, TTS: &TTSService{Store: storage.NewLocal(t.TempDir())}}
	runner.pollClaimed()
	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusSucceeded {
		t.Fatalf("job=%+v", current)
	}
}

func TestPollClaimedFailsMissingAndUnsupportedTargets(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	imageJob, err := jobService.CreateQueuedOrganization(16, "image.generate", "image_generation", 901, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	videoJob, err := jobService.CreateQueuedOrganization(16, "video.generate", "video_generation", 902, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	unknownJob, err := jobService.CreateQueuedOrganization(16, "unknown", "unknown_target", 903, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	runner := &AsyncRunner{Jobs: jobService, Images: &ImageService{Jobs: jobService}, Videos: &VideoService{Jobs: jobService}}
	runner.pollClaimed()
	for _, id := range []uint{imageJob.ID, videoJob.ID, unknownJob.ID} {
		current, err := jobService.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status != jobs.StatusFailed || current.LastError == "" {
			t.Fatalf("job=%+v", current)
		}
	}
}

func TestPollClaimedFailsUnavailableImageAndVideoWorkers(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	imageJob, err := jobService.CreateQueuedOrganization(17, "image.generate", "image_generation", 1, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	videoJob, err := jobService.CreateQueuedOrganization(17, "video.generate", "video_generation", 2, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	(&AsyncRunner{Jobs: jobService}).pollClaimed()
	for _, id := range []uint{imageJob.ID, videoJob.ID} {
		current, err := jobService.Get(id)
		if err != nil || current.Status != jobs.StatusFailed || !strings.Contains(current.LastError, "worker unavailable") {
			t.Fatalf("job=%+v err=%v", current, err)
		}
	}
}

func TestRunTTSJobFailsWhenEpisodeIsMissing(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	storyboard := models.Storyboard{OrganizationID: 18, EpisodeID: 999, StoryboardNumber: 1, Dialogue: "旁白：开始", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	jobService := jobs.New(database)
	job, err := jobService.CreateQueuedOrganization(18, "tts.generate", "storyboard_tts", storyboard.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := jobService.ClaimWaiting("tts-missing-episode", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	(&AsyncRunner{Jobs: jobService, TTS: &TTSService{Store: storage.NewLocal(t.TempDir())}}).runTTSJob(claimed[0])
	current, err := jobService.Get(job.ID)
	if err != nil || current.Status != jobs.StatusFailed || current.LastError == "" {
		t.Fatalf("job=%+v err=%v", current, err)
	}
}

func TestPollJobsFailWhenProviderTaskIDIsMissing(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	now := response.Now()
	image := models.ImageGeneration{OrganizationID: 3, Status: "processing", CreatedAt: now, UpdatedAt: now}
	video := models.VideoGeneration{OrganizationID: 3, Status: "processing", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&image).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&video).Error; err != nil {
		t.Fatal(err)
	}
	imageJob, err := jobService.CreateForTargetOrganization(3, "image.generate", "image_generation", image.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	videoJob, err := jobService.CreateForTargetOrganization(3, "video.generate", "video_generation", video.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	runner := &AsyncRunner{Images: &ImageService{Jobs: jobService}, Videos: &VideoService{Jobs: jobService}, Jobs: jobService}
	runner.pollImageJob(*imageJob)
	runner.pollVideoJob(*videoJob)
	for _, id := range []uint{imageJob.ID, videoJob.ID} {
		current, err := jobService.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status != jobs.StatusFailed || current.LastError == "" {
			t.Fatalf("job=%+v", current)
		}
	}
}

func TestRequeueJobReturnsRunningJobToProviderWait(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	job, err := jobService.CreateForTargetOrganization(4, "video.generate", "video_generation", 22, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	(&AsyncRunner{}).requeueJob(job.ID, errors.New("temporary provider error"))
	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusWaitingProvider || current.LastError != "temporary provider error" || current.LeaseExpiresAt != nil {
		t.Fatalf("job=%+v", current)
	}
}

func TestComposeAndMergeWorkersPersistOutputs(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for compose worker")
	}
	database := generationDatabase(t)
	store := storage.NewLocal(t.TempDir())
	inputRel := filepath.ToSlash(filepath.Join("videos", "input.mp4"))
	inputAbs := store.Abs(inputRel)
	if err := os.MkdirAll(filepath.Dir(inputAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "color=c=black:s=160x90:d=0.2", "-c:v", "libx264", "-pix_fmt", "yuv420p", inputAbs).CombinedOutput()
	if err != nil {
		t.Fatalf("create video: %v: %s", err, output)
	}
	now := response.Now()
	episode := models.Episode{OrganizationID: 8, DramaID: 2, EpisodeNumber: 1, Title: "episode", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{OrganizationID: 8, EpisodeID: episode.ID, StoryboardNumber: 1, VideoURL: store.PublicURL(inputRel), CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	runner := &AsyncRunner{Store: store}
	composed, err := runner.composeStoryboard(context.Background(), 8, composeShotPayload{StoryboardID: storyboard.ID})
	if err != nil || !strings.Contains(composed, "composed_video_url") {
		t.Fatalf("compose result=%q err=%v", composed, err)
	}
	if err := database.First(&storyboard, storyboard.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storyboard.Status != "composed" || storyboard.ComposedVideoURL == "" {
		t.Fatalf("storyboard=%+v", storyboard)
	}
	if _, err := runner.composeStoryboard(context.Background(), 8, composeShotPayload{
		StoryboardID: storyboard.ID, VideoURL: store.PublicURL(inputRel), AudioURL: "/static/audio/missing.mp3",
	}); err == nil {
		t.Fatal("missing snapshot audio accepted")
	}
	if _, err := runner.composeEpisode(context.Background(), 8, episode.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.composeEpisode(context.Background(), 8, episode.ID, []composeShotPayload{{
		StoryboardID: storyboard.ID, VideoURL: store.PublicURL(inputRel), OutputRel: "composed/snapshot.mp4",
	}}); err != nil {
		t.Fatalf("snapshot compose: %v", err)
	}
	merged, err := runner.mergeEpisode(context.Background(), 8, episode.ID, nil, "")
	if err != nil || !strings.Contains(merged, "merged_url") {
		t.Fatalf("merge result=%q err=%v", merged, err)
	}
	if err := database.First(&episode, episode.ID).Error; err != nil {
		t.Fatal(err)
	}
	if episode.Status != "completed" || episode.VideoURL == "" {
		t.Fatalf("episode=%+v", episode)
	}
	var merge models.VideoMerge
	if err := database.Where("organization_id = ? AND episode_id = ?", 8, episode.ID).First(&merge).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := runner.composeStoryboard(context.Background(), 9, composeShotPayload{StoryboardID: storyboard.ID}); err == nil {
		t.Fatal("foreign organization composed storyboard")
	}
	jobService := jobs.New(database)
	payload := `{"episode_id":` + itoaForTest(episode.ID) + `,"inputs":["` + storyboard.ComposedVideoURL + `"],"output_rel":"merged/worker.mp4"}`
	job, err := jobService.CreateQueuedPayloadOrganization(8, "episode.merge", "episode_merge", episode.ID, "ffmpeg", nil, payload)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := jobService.ClaimWaiting("merge-worker", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	(&AsyncRunner{Jobs: jobService, Store: store}).runComposeJob(claimed[0], "merge-worker")
	current, err := jobService.Get(job.ID)
	if err != nil || current.Status != jobs.StatusSucceeded {
		t.Fatalf("worker job=%+v err=%v", current, err)
	}
}

func TestComposeWorkersRejectMissingInputs(t *testing.T) {
	database := generationDatabase(t)
	store := storage.NewLocal(t.TempDir())
	now := response.Now()
	episode := models.Episode{OrganizationID: 9, DramaID: 3, EpisodeNumber: 1, Title: "empty", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{OrganizationID: 9, EpisodeID: episode.ID, StoryboardNumber: 1, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	runner := &AsyncRunner{Store: store}
	if _, err := runner.composeStoryboard(context.Background(), 9, composeShotPayload{StoryboardID: storyboard.ID}); err == nil || !strings.Contains(err.Error(), "no video") {
		t.Fatalf("compose error=%v", err)
	}
	if _, err := runner.mergeEpisode(context.Background(), 9, episode.ID, nil, ""); err == nil || !strings.Contains(err.Error(), "no video") {
		t.Fatalf("merge error=%v", err)
	}
	if _, err := runner.composeEpisode(context.Background(), 9, 999, nil); err == nil || !strings.Contains(err.Error(), "no storyboards") {
		t.Fatalf("empty episode error=%v", err)
	}
}

func TestLegacyPollingDelegatesToPersistentPollers(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	imageConfig := models.AIServiceConfig{OrganizationID: 13, ServiceType: "image", Provider: "mock", Name: "mock-image", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now}
	videoConfig := models.AIServiceConfig{OrganizationID: 13, ServiceType: "video", Provider: "mock", Name: "mock-video", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&imageConfig).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&videoConfig).Error; err != nil {
		t.Fatal(err)
	}
	imageRecord := models.ImageGeneration{OrganizationID: 13, ConfigID: &imageConfig.ID, Status: "processing", TaskID: "legacy-image", CreatedAt: now, UpdatedAt: now}
	videoRecord := models.VideoGeneration{OrganizationID: 13, ConfigID: &videoConfig.ID, Status: "processing", TaskID: "legacy-video", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&imageRecord).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&videoRecord).Error; err != nil {
		t.Fatal(err)
	}
	runner := &AsyncRunner{Images: &ImageService{}, Videos: &VideoService{}}
	runner.pollImages()
	runner.pollVideos()
	if err := database.First(&imageRecord, imageRecord.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.First(&videoRecord, videoRecord.ID).Error; err != nil {
		t.Fatal(err)
	}
	if imageRecord.Status != "failed" || videoRecord.Status != "failed" {
		t.Fatalf("image=%+v video=%+v", imageRecord, videoRecord)
	}
}

func itoaForTest(value uint) string {
	return fmt.Sprintf("%d", value)
}
