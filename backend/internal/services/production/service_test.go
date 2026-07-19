package production

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/agents"
	"github.com/eqzhou/flyaimovie/internal/services/generation"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	"github.com/eqzhou/flyaimovie/internal/storage"
)

type productionFixture struct {
	service *Service
	worker  *generation.AsyncRunner
	drama   models.Drama
	episode models.Episode
}

func newProductionFixture(t *testing.T) productionFixture {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/production.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedOrganizationDefaults(database, 41); err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	for _, serviceType := range []string{"text", "image", "video", "audio"} {
		config := models.AIServiceConfig{OrganizationID: 41, ServiceType: serviceType, Provider: "mock", Name: "production-" + serviceType, BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now}
		if err := database.Create(&config).Error; err != nil {
			t.Fatal(err)
		}
	}
	drama := models.Drama{OrganizationID: 41, Title: "自动短剧", TotalEpisodes: 1, Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{OrganizationID: 41, DramaID: drama.ID, EpisodeNumber: 1, Title: "第一集", Content: "## S01 | 内景 · 车站 | 夜\n阿宁：我们回家。", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	store := storage.NewLocal(filepath.Join(t.TempDir(), "media"))
	jobService := jobs.New(database)
	cache := mediacache.New(database, store)
	images := &generation.ImageService{Store: store, Jobs: jobService, Cache: cache}
	videos := &generation.VideoService{Store: store, Jobs: jobService, Cache: cache}
	tts := &generation.TTSService{Store: store, Cache: cache}
	worker := &generation.AsyncRunner{Images: images, Videos: videos, TTS: tts, Jobs: jobService, Store: store, Cache: cache}
	service := New(database, agents.NewRunner(t.TempDir()), images, videos, jobService)
	return productionFixture{service: service, worker: worker, drama: drama, episode: episode}
}

func TestProductionRunCompletesMockEpisodeAndIsIdempotent(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg required")
	}
	fixture := newProductionFixture(t)
	fixture.worker.Start()
	t.Cleanup(fixture.worker.Stop)
	run, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := fixture.service.ProcessAvailable(context.Background(), "production-test", 1); err != nil {
			t.Fatal(err)
		}
		current, err := fixture.service.Get(41, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == StatusSucceeded {
			break
		}
		if current.Status == StatusFailed {
			t.Fatalf("production failed at %s: %s", current.Stage, current.LastError)
		}
		time.Sleep(150 * time.Millisecond)
	}
	current, _ := fixture.service.Get(41, run.ID)
	if current.Status != StatusSucceeded || current.Progress != 100 || current.Stage != StageCompleted {
		t.Fatalf("run=%+v", current)
	}
	var episode models.Episode
	if err := db.DB.First(&episode, fixture.episode.ID).Error; err != nil {
		t.Fatal(err)
	}
	if episode.VideoURL == "" || episode.Status != "completed" {
		t.Fatalf("episode=%+v", episode)
	}
	var agentRuns []models.AgentRun
	if err := db.DB.Where("organization_id = ? AND episode_id = ?", 41, fixture.episode.ID).Order("id").Find(&agentRuns).Error; err != nil || len(agentRuns) < 3 {
		t.Fatalf("agent runs=%d err=%v", len(agentRuns), err)
	}
	streamedTools := 0
	resolvedPrompts := 0
	for _, agentRun := range agentRuns {
		var runEvents []models.AgentRunEvent
		if err := db.DB.Where("organization_id = ? AND agent_run_id = ?", 41, agentRun.ID).Order("sequence").Find(&runEvents).Error; err != nil {
			t.Fatal(err)
		}
		if len(runEvents) < 2 || runEvents[0].EventType != "started" || runEvents[len(runEvents)-1].EventType != "completed" {
			t.Fatalf("agent run %d events=%+v", agentRun.ID, runEvents)
		}
		for _, event := range runEvents {
			if event.EventType == "prompt_resolved" {
				resolvedPrompts++
			}
			if event.EventType == "tool_call" || event.EventType == "tool_result" {
				streamedTools++
			}
		}
	}
	if streamedTools == 0 {
		t.Fatal("automatic production did not persist Agent tool events")
	}
	if resolvedPrompts != len(agentRuns) {
		t.Fatalf("resolved prompt events=%d agent runs=%d", resolvedPrompts, len(agentRuns))
	}
	var before int64
	db.DB.Model(&models.GenerationJob{}).Where("production_run_id = ?", run.ID).Count(&before)
	if _, err := fixture.service.ProcessAvailable(context.Background(), "production-test", 5); err != nil {
		t.Fatal(err)
	}
	var after int64
	db.DB.Model(&models.GenerationJob{}).Where("production_run_id = ?", run.ID).Count(&after)
	if after != before {
		t.Fatalf("completed run created more jobs: before=%d after=%d", before, after)
	}
}

func TestProductionRunCancellationRetryAndOrganizationIsolation(t *testing.T) {
	fixture := newProductionFixture(t)
	run, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID); err != ErrActiveRun {
		t.Fatalf("duplicate err=%v", err)
	}
	if err := fixture.service.Cancel(42, run.ID); err != ErrRunNotFound {
		t.Fatalf("cross-org cancel err=%v", err)
	}
	if err := fixture.service.Cancel(41, run.ID); err != nil {
		t.Fatal(err)
	}
	canceled, _ := fixture.service.Get(41, run.ID)
	if canceled.Status != StatusCanceled {
		t.Fatalf("status=%s", canceled.Status)
	}
	var canceledEpisode models.Episode
	if err := fixture.service.DB.First(&canceledEpisode, fixture.episode.ID).Error; err != nil {
		t.Fatal(err)
	}
	if canceledEpisode.Status != "draft" {
		t.Fatalf("canceled episode status=%s", canceledEpisode.Status)
	}
	if err := fixture.service.Retry(41, run.ID); err != nil {
		t.Fatal(err)
	}
	retried, _ := fixture.service.Get(41, run.ID)
	if retried.Status != StatusQueued || retried.Attempt != 2 {
		t.Fatalf("retried=%+v", retried)
	}
	if rows, err := fixture.service.List(42, fixture.episode.ID, 20); err != nil || len(rows) != 0 {
		t.Fatalf("cross-org rows=%d err=%v", len(rows), err)
	}
}

func TestCancelProductionSynchronizesMediaRecords(t *testing.T) {
	fixture := newProductionFixture(t)
	run, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	image := models.ImageGeneration{OrganizationID: 41, Status: "processing", CreatedAt: now, UpdatedAt: now}
	video := models.VideoGeneration{OrganizationID: 41, Status: "processing", CreatedAt: now, UpdatedAt: now}
	if err := fixture.service.DB.Create(&image).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DB.Create(&video).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Jobs.CreateQueuedOrganizationProduction(41, "image.generate", "image_generation", image.ID, "mock", nil, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Jobs.CreateQueuedOrganizationProduction(41, "video.generate", "video_generation", video.ID, "mock", nil, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Cancel(41, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DB.First(&image, image.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DB.First(&video, video.ID).Error; err != nil {
		t.Fatal(err)
	}
	if image.Status != "canceled" || video.Status != "canceled" {
		t.Fatalf("media not synchronized: image=%s video=%s", image.Status, video.Status)
	}
}

func TestCancelProductionInterruptsRunningAgentWithoutFallbackWrites(t *testing.T) {
	fixture := newProductionFixture(t)
	requestStarted := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-requestStarted:
		default:
			close(requestStarted)
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(1500 * time.Millisecond):
			http.Error(w, "slow provider", http.StatusServiceUnavailable)
		}
	}))
	defer provider.Close()
	if err := fixture.service.DB.Model(&models.AIServiceConfig{}).Where("organization_id = ? AND service_type = ?", 41, "text").Updates(map[string]any{"provider": "openai_local", "base_url": provider.URL, "model": "slow-model", "updated_at": response.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	originalContent := fixture.episode.Content
	run, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	processed := make(chan error, 1)
	go func() {
		_, processErr := fixture.service.ProcessAvailable(context.Background(), "cancel-agent", 1)
		processed <- processErr
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("Agent provider request did not start")
	}
	startedCancel := time.Now()
	if err := fixture.service.Cancel(41, run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-processed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("running Agent was not interrupted by production cancellation")
	}
	if elapsed := time.Since(startedCancel); elapsed > 500*time.Millisecond {
		t.Fatalf("cancel took %s", elapsed)
	}
	var episode models.Episode
	if err := fixture.service.DB.First(&episode, fixture.episode.ID).Error; err != nil {
		t.Fatal(err)
	}
	if episode.Content != originalContent || episode.Status != "draft" {
		t.Fatalf("canceled Agent mutated episode: %+v", episode)
	}
}

func TestProductionRetryRejectsNewerActiveRun(t *testing.T) {
	fixture := newProductionFixture(t)
	oldRun, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Cancel(41, oldRun.ID); err != nil {
		t.Fatal(err)
	}
	newRun, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID)
	if err != nil || newRun.ID == oldRun.ID {
		t.Fatalf("new run=%+v err=%v", newRun, err)
	}
	if err := fixture.service.Retry(41, oldRun.ID); err != ErrActiveRun {
		t.Fatalf("retry err=%v", err)
	}
}

func TestLateFailureCannotOverwriteCanceledRun(t *testing.T) {
	fixture := newProductionFixture(t)
	run, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Cancel(41, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.failRun(run, errors.New("late worker failure")); err != nil {
		t.Fatal(err)
	}
	canceled, _ := fixture.service.Get(41, run.ID)
	if canceled.Status != StatusCanceled {
		t.Fatalf("run=%+v", canceled)
	}
	var episode models.Episode
	if err := fixture.service.DB.First(&episode, fixture.episode.ID).Error; err != nil {
		t.Fatal(err)
	}
	if episode.Status != "draft" {
		t.Fatalf("episode status=%s", episode.Status)
	}
}

func TestProductionRunFailureAndTransitionGuards(t *testing.T) {
	fixture := newProductionFixture(t)
	run, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DB.Model(run).Update("stage", "unsupported").Error; err != nil {
		t.Fatal(err)
	}
	if processed, err := fixture.service.ProcessAvailable(context.Background(), "failure-test", 1); err != nil || processed != 1 {
		t.Fatalf("processed=%d err=%v", processed, err)
	}
	failed, err := fixture.service.Get(41, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != StatusFailed || failed.LastError == "" {
		t.Fatalf("failed=%+v", failed)
	}
	var episode models.Episode
	if err := fixture.service.DB.First(&episode, fixture.episode.ID).Error; err != nil {
		t.Fatal(err)
	}
	if episode.Status != "failed" {
		t.Fatalf("episode status=%s", episode.Status)
	}
	if err := fixture.service.Cancel(41, run.ID); err != ErrTerminalRun {
		t.Fatalf("terminal cancel=%v", err)
	}
	if err := fixture.service.Retry(42, run.ID); err != ErrRunNotFound {
		t.Fatalf("cross-org retry=%v", err)
	}
	if err := fixture.service.Retry(41, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Retry(41, run.ID); err != ErrTerminalRun {
		t.Fatalf("active retry=%v", err)
	}
	if _, err := fixture.service.Get(41, run.ID+999); err != ErrRunNotFound {
		t.Fatalf("missing get=%v", err)
	}
	if _, err := fixture.service.ProcessAvailable(context.Background(), "", 1); err == nil {
		t.Fatal("expected empty owner error")
	}
}

func TestProductionRunFailsWhenChildJobFails(t *testing.T) {
	fixture := newProductionFixture(t)
	run, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DB.Model(run).Updates(map[string]any{"stage": StageTTS, "progress": 66}).Error; err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	storyboard := models.Storyboard{OrganizationID: 41, EpisodeID: fixture.episode.ID, StoryboardNumber: 1, Title: "对白", Dialogue: "阿宁：回家", CreatedAt: now, UpdatedAt: now}
	if err := fixture.service.DB.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	child := models.GenerationJob{OrganizationID: 41, ProductionRunID: &run.ID, Kind: "tts.generate", Status: jobs.StatusFailed, TargetType: "storyboard_tts", TargetID: storyboard.ID, Attempt: 1, MaxAttempts: 3, AvailableAt: now, LastError: "provider rejected audio", CreatedAt: now, UpdatedAt: now}
	if err := fixture.service.DB.Create(&child).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.ProcessAvailable(context.Background(), "child-failure", 1); err != nil {
		t.Fatal(err)
	}
	failed, _ := fixture.service.Get(41, run.ID)
	if failed.Status != StatusFailed || failed.LastError != "provider rejected audio" {
		t.Fatalf("run=%+v", failed)
	}
}

func TestProductionStagesDoNotDuplicateActiveMediaJobs(t *testing.T) {
	fixture := newProductionFixture(t)
	now := response.Now()
	tests := []struct {
		name       string
		stage      string
		targetType string
		seed       func(*models.Storyboard) uint
		count      func() int64
	}{
		{
			name: "frames", stage: StageFrames, targetType: "image_generation",
			seed: func(storyboard *models.Storyboard) uint {
				row := models.ImageGeneration{OrganizationID: 41, DramaID: &fixture.drama.ID, StoryboardID: &storyboard.ID, Prompt: "active frame", Status: "processing", CreatedAt: now, UpdatedAt: now}
				if err := fixture.service.DB.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
				return row.ID
			},
			count: func() int64 {
				var count int64
				fixture.service.DB.Model(&models.ImageGeneration{}).Count(&count)
				return count
			},
		},
		{
			name: "videos", stage: StageVideos, targetType: "video_generation",
			seed: func(storyboard *models.Storyboard) uint {
				row := models.VideoGeneration{OrganizationID: 41, DramaID: &fixture.drama.ID, StoryboardID: &storyboard.ID, Prompt: "active video", ImageURL: "/static/frame.png", Status: "processing", CreatedAt: now, UpdatedAt: now}
				if err := fixture.service.DB.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
				return row.ID
			},
			count: func() int64 {
				var count int64
				fixture.service.DB.Model(&models.VideoGeneration{}).Count(&count)
				return count
			},
		},
	}
	for index, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			episode := models.Episode{OrganizationID: 41, DramaID: fixture.drama.ID, EpisodeNumber: 20 + index, Title: tc.name, Content: "content", Status: "draft", CreatedAt: now, UpdatedAt: now}
			if err := fixture.service.DB.Create(&episode).Error; err != nil {
				t.Fatal(err)
			}
			run, err := fixture.service.Create(41, fixture.drama.ID, episode.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.service.DB.Model(run).Update("stage", tc.stage).Error; err != nil {
				t.Fatal(err)
			}
			firstFrame := ""
			if tc.stage == StageVideos {
				firstFrame = "/static/frame.png"
			}
			storyboard := models.Storyboard{OrganizationID: 41, EpisodeID: episode.ID, StoryboardNumber: 1, Title: "shot", ImagePrompt: "frame prompt", VideoPrompt: "video prompt", FirstFrameImage: firstFrame, CreatedAt: now, UpdatedAt: now}
			if err := fixture.service.DB.Create(&storyboard).Error; err != nil {
				t.Fatal(err)
			}
			targetID := tc.seed(&storyboard)
			if _, err := fixture.service.Jobs.CreateQueuedOrganizationProduction(41, tc.targetType+".generate", tc.targetType, targetID, "mock", nil, run.ID); err != nil {
				t.Fatal(err)
			}
			before := tc.count()
			if _, err := fixture.service.ProcessAvailable(context.Background(), "dedupe-"+tc.name, 1); err != nil {
				t.Fatal(err)
			}
			if after := tc.count(); after != before {
				t.Fatalf("active %s job created duplicate media: before=%d after=%d", tc.name, before, after)
			}
			current, _ := fixture.service.Get(41, run.ID)
			if current.Stage != tc.stage || current.Status != StatusQueued {
				t.Fatalf("run advanced while child active: %+v", current)
			}
		})
	}
}

func TestProductionMediaStagesUseEpisodeBoundConfigs(t *testing.T) {
	fixture := newProductionFixture(t)
	now := response.Now()
	imageConfig := models.AIServiceConfig{OrganizationID: 41, ServiceType: "image", Provider: "mock", Name: "bound-image", BaseURL: "http://localhost", APIKey: "mock", Model: "bound-image", IsActive: true, CreatedAt: now, UpdatedAt: now}
	videoConfig := models.AIServiceConfig{OrganizationID: 41, ServiceType: "video", Provider: "mock", Name: "bound-video", BaseURL: "http://localhost", APIKey: "mock", Model: "bound-video", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := fixture.service.DB.Create(&imageConfig).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DB.Create(&videoConfig).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DB.Model(&fixture.episode).Updates(map[string]any{"image_config_id": imageConfig.ID, "video_config_id": videoConfig.ID}).Error; err != nil {
		t.Fatal(err)
	}
	run, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DB.Model(run).Update("stage", StageFrames).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{OrganizationID: 41, EpisodeID: fixture.episode.ID, StoryboardNumber: 1, Title: "bound shot", ImagePrompt: "frame prompt", VideoPrompt: "video prompt", CreatedAt: now, UpdatedAt: now}
	if err := fixture.service.DB.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.ProcessAvailable(context.Background(), "bound-frame", 1); err != nil {
		t.Fatal(err)
	}
	var image models.ImageGeneration
	if err := fixture.service.DB.Where("storyboard_id = ?", storyboard.ID).Order("id desc").First(&image).Error; err != nil {
		t.Fatal(err)
	}
	if image.ConfigID == nil || *image.ConfigID != imageConfig.ID {
		t.Fatalf("image config=%v want=%d", image.ConfigID, imageConfig.ID)
	}
	var imageJob models.GenerationJob
	if err := fixture.service.DB.Where("target_type = ? AND target_id = ?", "image_generation", image.ID).First(&imageJob).Error; err != nil {
		t.Fatal(err)
	}
	if imageJob.ProductionRunID == nil || *imageJob.ProductionRunID != run.ID {
		t.Fatalf("image job production run=%v want=%d", imageJob.ProductionRunID, run.ID)
	}
	if err := fixture.service.DB.Model(run).Updates(map[string]any{"stage": StageVideos, "lease_owner": "", "lease_expires_at": nil, "available_at": response.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.ProcessAvailable(context.Background(), "bound-video", 1); err != nil {
		t.Fatal(err)
	}
	var video models.VideoGeneration
	if err := fixture.service.DB.Where("storyboard_id = ?", storyboard.ID).Order("id desc").First(&video).Error; err != nil {
		t.Fatal(err)
	}
	if video.ConfigID == nil || *video.ConfigID != videoConfig.ID {
		t.Fatalf("video config=%v want=%d", video.ConfigID, videoConfig.ID)
	}
	var videoJob models.GenerationJob
	if err := fixture.service.DB.Where("target_type = ? AND target_id = ?", "video_generation", video.ID).First(&videoJob).Error; err != nil {
		t.Fatal(err)
	}
	if videoJob.ProductionRunID == nil || *videoJob.ProductionRunID != run.ID {
		t.Fatalf("video job production run=%v want=%d", videoJob.ProductionRunID, run.ID)
	}
}

func TestProductionWorkerStartsStopsAndProcesses(t *testing.T) {
	fixture := newProductionFixture(t)
	run, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	worker := &Worker{Service: fixture.service, Interval: 10 * time.Millisecond}
	worker.Stop()
	worker.Start()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := fixture.service.Get(41, run.ID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if current.Stage != StageScript {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	worker.Stop()
	worker.Stop()
	current, _ := fixture.service.Get(41, run.ID)
	if current.Stage == StageScript {
		t.Fatalf("worker did not process run: %+v", current)
	}
}

func TestProductionWorkerStopCancelsRunningProviderRequest(t *testing.T) {
	fixture := newProductionFixture(t)
	requestStarted := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-requestStarted:
		default:
			close(requestStarted)
		}
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
			http.Error(w, "provider request was not canceled", http.StatusGatewayTimeout)
		}
	}))
	defer provider.Close()
	if err := fixture.service.DB.Model(&models.AIServiceConfig{}).Where("organization_id = ? AND service_type = ?", 41, "text").Updates(map[string]any{"provider": "openai_local", "base_url": provider.URL, "model": "slow-model", "updated_at": response.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	run, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	worker := &Worker{Service: fixture.service, Interval: 5 * time.Millisecond}
	worker.Start()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		worker.Stop()
		t.Fatal("production worker did not start provider request")
	}
	started := time.Now()
	worker.Stop()
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("worker stop took %s", elapsed)
	}
	current, err := fixture.service.Get(41, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != StatusQueued || current.LeaseOwner != "" {
		t.Fatalf("stopped run was not released for recovery: %+v", current)
	}
}

func TestProductionRunValidationAndStageFailures(t *testing.T) {
	fixture := newProductionFixture(t)
	if _, err := fixture.service.Create(41, 0, 0); err == nil {
		t.Fatal("expected invalid target")
	}
	if _, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID+999); err != ErrRunNotFound {
		t.Fatalf("missing target=%v", err)
	}
	if rows, err := fixture.service.List(41, 0, 0); err != nil || len(rows) != 0 {
		t.Fatalf("list rows=%d err=%v", len(rows), err)
	}

	createEpisode := func(number int) models.Episode {
		now := response.Now()
		episode := models.Episode{OrganizationID: 41, DramaID: fixture.drama.ID, EpisodeNumber: number, Title: "边界", Content: "内容", Status: "draft", CreatedAt: now, UpdatedAt: now}
		if err := fixture.service.DB.Create(&episode).Error; err != nil {
			t.Fatal(err)
		}
		return episode
	}
	createRunAt := func(episode models.Episode, stage string) *models.ProductionRun {
		run, err := fixture.service.Create(41, fixture.drama.ID, episode.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := fixture.service.DB.Model(run).Update("stage", stage).Error; err != nil {
			t.Fatal(err)
		}
		return run
	}
	addStoryboard := func(episode models.Episode, prompt, frame, video string) {
		now := response.Now()
		row := models.Storyboard{OrganizationID: 41, EpisodeID: episode.ID, StoryboardNumber: 1, Title: prompt, ImagePrompt: prompt, FirstFrameImage: frame, VideoURL: video, CreatedAt: now, UpdatedAt: now}
		if err := fixture.service.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}

	framesEpisode := createEpisode(2)
	framesRun := createRunAt(framesEpisode, StageFrames)
	addStoryboard(framesEpisode, "", "", "")
	if _, err := fixture.service.ProcessAvailable(context.Background(), "frames-failure", 1); err != nil {
		t.Fatal(err)
	}
	if failed, _ := fixture.service.Get(41, framesRun.ID); failed.Status != StatusFailed {
		t.Fatalf("frames=%+v", failed)
	}

	videosEpisode := createEpisode(3)
	videosRun := createRunAt(videosEpisode, StageVideos)
	addStoryboard(videosEpisode, "图片提示", "", "")
	if _, err := fixture.service.ProcessAvailable(context.Background(), "videos-failure", 1); err != nil {
		t.Fatal(err)
	}
	if failed, _ := fixture.service.Get(41, videosRun.ID); failed.Status != StatusFailed {
		t.Fatalf("videos=%+v", failed)
	}

	composeEpisode := createEpisode(4)
	composeRun := createRunAt(composeEpisode, StageCompose)
	addStoryboard(composeEpisode, "图片提示", "/static/frame.png", "")
	if _, err := fixture.service.ProcessAvailable(context.Background(), "compose-failure", 1); err != nil {
		t.Fatal(err)
	}
	if failed, _ := fixture.service.Get(41, composeRun.ID); failed.Status != StatusFailed {
		t.Fatalf("compose=%+v", failed)
	}

	maxRun, err := fixture.service.Create(41, fixture.drama.ID, createEpisode(5).ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DB.Model(maxRun).Updates(map[string]any{"status": StatusFailed, "attempt": maxRun.MaxAttempts}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Retry(41, maxRun.ID); !errors.Is(err, ErrTerminalRun) {
		t.Fatalf("max retry=%v", err)
	}
}

func TestRetryProductionWriteRetriesOnlySQLiteContention(t *testing.T) {
	attempts := 0
	if err := retryProductionWrite(func() error {
		attempts++
		if attempts < 3 {
			return errors.New("database is locked")
		}
		return nil
	}); err != nil || attempts != 3 {
		t.Fatalf("retry err=%v attempts=%d", err, attempts)
	}
	attempts = 0
	want := errors.New("permission denied")
	if err := retryProductionWrite(func() error { attempts++; return want }); !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("non-retry err=%v attempts=%d", err, attempts)
	}
}

func TestProductionLeaseHeartbeatPreventsDuplicateClaim(t *testing.T) {
	fixture := newProductionFixture(t)
	fixture.service.leaseDuration = 2 * time.Second
	run, err := fixture.service.Create(41, fixture.drama.ID, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := fixture.service.claim("lease-owner-a", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	heartbeatErrors := fixture.service.startLeaseHeartbeat(ctx, cancel, run.ID, "lease-owner-a")
	time.Sleep(2500 * time.Millisecond)
	duplicate, err := fixture.service.claim("lease-owner-b", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(duplicate) != 0 {
		t.Fatalf("active run was claimed twice: %+v", duplicate)
	}
	cancel()
	if err := <-heartbeatErrors; err != nil {
		t.Fatal(err)
	}
}
