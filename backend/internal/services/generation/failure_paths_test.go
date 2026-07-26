package generation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/storage"
)

func mockImageConfig(t *testing.T, organizationID uint) models.AIServiceConfig {
	t.Helper()
	now := response.Now()
	config := models.AIServiceConfig{OrganizationID: organizationID, ServiceType: "image", Provider: "mock", Name: "mock", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	return config
}

func mockVideoConfig(t *testing.T, organizationID uint) models.AIServiceConfig {
	t.Helper()
	now := response.Now()
	config := models.AIServiceConfig{OrganizationID: organizationID, ServiceType: "video", Provider: "mock", Name: "mock", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	return config
}

// A canceled context must abort finalize before the provider file is downloaded, and
// the record must not be marked completed.
func TestImageFinalizeStopsOnCanceledContext(t *testing.T) {
	database := generationDatabase(t)
	service := &ImageService{Store: storage.NewLocal(t.TempDir()), Jobs: jobs.New(database)}
	record := &models.ImageGeneration{OrganizationID: 1, Prompt: "canceled", CreatedAt: response.Now(), UpdatedAt: response.Now()}
	if err := database.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := service.Finalize(ctx, record, "https://cdn.example/image.png")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
	if record.Status == "completed" {
		t.Fatalf("record=%+v, want the canceled finalize to leave the record incomplete", record)
	}
}

// A job owned by a different worker must not be finalized by this caller.
func TestImageFinalizeOwnedRejectsForeignOwner(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	service := &ImageService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}
	record := &models.ImageGeneration{OrganizationID: 1, Prompt: "owned", CreatedAt: response.Now(), UpdatedAt: response.Now()}
	if err := database.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	job, err := jobService.CreateQueuedOrganization(1, "image.generate", "image_generation", record.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := jobService.ClaimWaiting("real-owner", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}

	finalizeErr := service.FinalizeOwned(context.Background(), record, "https://cdn.example/image.png", job.ID, "impostor-owner")
	if !errors.Is(finalizeErr, jobs.ErrTerminalJob) {
		t.Fatalf("err=%v, want jobs.ErrTerminalJob for a foreign owner", finalizeErr)
	}
	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusRunning || current.LeaseOwner != "real-owner" {
		t.Fatalf("job=%+v, want the real owner's lease untouched", current)
	}
}

// When a job id is supplied but the service has no job store, ownership cannot be
// proven and finalize must refuse.
func TestImageFinalizeOwnedRequiresJobService(t *testing.T) {
	generationDatabase(t)
	service := &ImageService{Store: storage.NewLocal(t.TempDir())}
	record := &models.ImageGeneration{OrganizationID: 1, Prompt: "no jobs"}

	err := service.FinalizeOwned(context.Background(), record, "https://cdn.example/image.png", 42, "owner")
	if !errors.Is(err, jobs.ErrTerminalJob) {
		t.Fatalf("err=%v, want jobs.ErrTerminalJob when the job service is missing", err)
	}
}

// A download failure must mark both the generation record and its owned job failed.
func TestImageFinalizeOwnedRecordsDownloadFailure(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	service := &ImageService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}
	record := &models.ImageGeneration{OrganizationID: 1, Prompt: "bad source", CreatedAt: response.Now(), UpdatedAt: response.Now()}
	if err := database.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	job, err := jobService.CreateQueuedOrganization(1, "image.generate", "image_generation", record.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := jobService.ClaimWaiting("worker", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}

	if err := service.FinalizeOwned(context.Background(), record, "not-a-valid-url", job.ID, "worker"); err == nil {
		t.Fatal("finalize accepted an invalid provider URL")
	}
	if record.Status != "failed" || record.ErrorMsg == "" {
		t.Fatalf("record=%+v, want a failed record with the error recorded", record)
	}
	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusFailed || current.LastError == "" {
		t.Fatalf("job=%+v, want the owned job failed", current)
	}
	var stored models.ImageGeneration
	if err := database.First(&stored, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" {
		t.Fatalf("stored=%+v, want the failure persisted", stored)
	}
}

// Without a job, a download failure still has to persist the failure on the record.
func TestImageFinalizeWithoutJobPersistsFailure(t *testing.T) {
	database := generationDatabase(t)
	service := &ImageService{Store: storage.NewLocal(t.TempDir())}
	record := &models.ImageGeneration{OrganizationID: 1, Prompt: "bad source", CreatedAt: response.Now(), UpdatedAt: response.Now()}
	if err := database.Create(record).Error; err != nil {
		t.Fatal(err)
	}

	if err := service.Finalize(context.Background(), record, "not-a-valid-url"); err == nil {
		t.Fatal("finalize accepted an invalid provider URL")
	}
	var stored models.ImageGeneration
	if err := database.First(&stored, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" || stored.ErrorMsg == "" {
		t.Fatalf("stored=%+v, want the failure persisted without a job", stored)
	}
}

// GenerateProduction must attach the production run to the created job so a production
// run can track its generations.
func TestImageGenerateProductionAttachesRunToJob(t *testing.T) {
	database := generationDatabase(t)
	config := mockImageConfig(t, 1)
	jobService := jobs.New(database)
	service := &ImageService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}
	record := &models.ImageGeneration{OrganizationID: 1, Prompt: "production image", ImageType: "storyboard_frame"}

	if err := service.GenerateProduction(context.Background(), record, &config.ID, 77); err != nil {
		t.Fatal(err)
	}
	if record.JobID == nil {
		t.Fatalf("record=%+v, want a job attached", record)
	}
	job, err := jobService.Get(*record.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.ProductionRunID == nil || *job.ProductionRunID != 77 {
		t.Fatalf("job=%+v, want production run 77 recorded", job)
	}
	if record.Status != "completed" {
		t.Fatalf("record=%+v, want the mock generation to complete", record)
	}
}

// The same production guarantee applies to video generation.
func TestVideoGenerateProductionAttachesRunToJob(t *testing.T) {
	database := generationDatabase(t)
	config := mockVideoConfig(t, 1)
	jobService := jobs.New(database)
	service := &VideoService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}
	record := &models.VideoGeneration{OrganizationID: 1, Prompt: "production video", Duration: 1}

	if err := service.GenerateProduction(context.Background(), record, &config.ID, 88); err != nil {
		t.Fatal(err)
	}
	if record.JobID == nil {
		t.Fatalf("record=%+v, want a job attached", record)
	}
	job, err := jobService.Get(*record.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.ProductionRunID == nil || *job.ProductionRunID != 88 {
		t.Fatalf("job=%+v, want production run 88 recorded", job)
	}
}

// A canceled context must stop video finalize before any download happens.
func TestVideoFinalizeStopsOnCanceledContext(t *testing.T) {
	generationDatabase(t)
	service := &VideoService{Store: storage.NewLocal(t.TempDir())}
	record := &models.VideoGeneration{OrganizationID: 1, Prompt: "canceled"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := service.Finalize(ctx, record, "https://cdn.example/video.mp4")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
}

// An unowned job id (no lease owner) must be rejected unless the job is genuinely
// running without an owner.
func TestVideoFinalizeOwnedRejectsForeignOwnerAndMissingJobService(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	record := &models.VideoGeneration{OrganizationID: 1, Prompt: "owned", CreatedAt: response.Now(), UpdatedAt: response.Now()}
	if err := database.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	job, err := jobService.CreateQueuedOrganization(1, "video.generate", "video_generation", record.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := jobService.ClaimWaiting("real-owner", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}

	service := &VideoService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}
	if err := service.FinalizeAuthorizedOwned(context.Background(), record, "https://cdn.example/v.mp4", "", job.ID, "impostor"); !errors.Is(err, jobs.ErrTerminalJob) {
		t.Fatalf("err=%v, want jobs.ErrTerminalJob for a foreign owner", err)
	}
	// A leased job cannot be finalized through the ownerless "current" path either.
	if err := service.FinalizeAuthorizedOwned(context.Background(), record, "https://cdn.example/v.mp4", "", job.ID, ""); !errors.Is(err, jobs.ErrTerminalJob) {
		t.Fatalf("err=%v, want jobs.ErrTerminalJob when the job is leased by a worker", err)
	}

	without := &VideoService{Store: storage.NewLocal(t.TempDir())}
	if err := without.FinalizeAuthorizedOwned(context.Background(), record, "https://cdn.example/v.mp4", "", job.ID, "owner"); !errors.Is(err, jobs.ErrTerminalJob) {
		t.Fatalf("err=%v, want jobs.ErrTerminalJob when the job service is missing", err)
	}
}

// A video download failure must fail the owned job and persist the record failure.
func TestVideoFinalizeOwnedRecordsDownloadFailure(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	service := &VideoService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}
	record := &models.VideoGeneration{OrganizationID: 1, Prompt: "bad source", CreatedAt: response.Now(), UpdatedAt: response.Now()}
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

	if err := service.FinalizeAuthorizedOwned(context.Background(), record, "not-a-valid-url", "", job.ID, "worker"); err == nil {
		t.Fatal("finalize accepted an invalid provider URL")
	}
	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusFailed || current.LastError == "" {
		t.Fatalf("job=%+v, want the owned job failed", current)
	}
	var stored models.VideoGeneration
	if err := database.First(&stored, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" || stored.ErrorMsg == "" {
		t.Fatalf("stored=%+v, want the failure persisted", stored)
	}
}

// A bearer-token finalize must still reject unsafe provider URLs rather than sending
// the credential somewhere unvalidated.
func TestVideoFinalizeAuthorizedRejectsUnsafeURL(t *testing.T) {
	generationDatabase(t)
	service := &VideoService{Store: storage.NewLocal(t.TempDir())}
	record := &models.VideoGeneration{OrganizationID: 1, Prompt: "token"}

	if err := service.FinalizeAuthorized(context.Background(), record, "http://169.254.169.254/latest/meta-data", "secret-token"); err == nil {
		t.Fatal("authorized finalize accepted a link-local metadata URL")
	}
	if record.Status != "failed" {
		t.Fatalf("record=%+v, want the record marked failed", record)
	}
}

// Video generation must reject a malformed reference array before calling the provider
// and must fail the job it just created.
func TestVideoGenerateFailsJobOnInvalidReferences(t *testing.T) {
	database := generationDatabase(t)
	config := mockVideoConfig(t, 1)
	jobService := jobs.New(database)
	service := &VideoService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}
	record := &models.VideoGeneration{OrganizationID: 1, Prompt: "bad refs", ReferenceImageURLs: `["https://cdn.example/a.png", 3]`}

	if err := service.Generate(context.Background(), record, &config.ID); err == nil {
		t.Fatal("generation accepted a malformed reference array")
	}
	if record.JobID == nil {
		t.Fatalf("record=%+v, want a job to have been created", record)
	}
	job, err := jobService.Get(*record.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != jobs.StatusFailed || job.LastError == "" {
		t.Fatalf("job=%+v, want the job failed with the parse error", job)
	}
	var stored models.VideoGeneration
	if err := database.First(&stored, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" {
		t.Fatalf("stored=%+v, want the failure persisted", stored)
	}
}

// A poll job whose generation row is missing must fail the claimed job instead of
// silently dropping it.
func TestPollImageJobFailsWhenTargetRowIsMissing(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	job, err := jobService.CreateQueuedOrganization(1, "image.generate", "image_generation", 4242, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := jobService.ClaimWaiting("worker", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	runner := &AsyncRunner{Images: &ImageService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}, Jobs: jobService}

	runner.pollImageJob(claimed[0])

	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusFailed || current.LastError != "image generation target not found" {
		t.Fatalf("job=%+v, want the job failed for a missing target", current)
	}
}

// A provider that reports a hard failure must mark both the record and job failed and
// propagate the message into the grid history row that tracks the generation.
func TestPollImageJobRecordsProviderFailureInGridHistory(t *testing.T) {
	database := generationDatabase(t)
	config := mockImageConfig(t, 1)
	jobService := jobs.New(database)
	now := response.Now()
	record := models.ImageGeneration{OrganizationID: 1, Prompt: "async", TaskID: "task-1", ConfigID: &config.ID, Status: "processing", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	history := models.GridHistory{OrganizationID: 1, ImageGenID: &record.ID, Status: "pending", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&history).Error; err != nil {
		t.Fatal(err)
	}
	job, err := jobService.CreateQueuedOrganization(1, "image.generate", "image_generation", record.ID, "mock", &config.ID)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := jobService.ClaimWaiting("worker", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	runner := &AsyncRunner{Images: &ImageService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}, Jobs: jobService}

	// The mock image adapter is sync-only, so Poll reports a hard failure.
	runner.pollImageJob(claimed[0])

	var stored models.ImageGeneration
	if err := database.First(&stored, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" || stored.ErrorMsg == "" {
		t.Fatalf("record=%+v, want the provider failure persisted", stored)
	}
	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusFailed {
		t.Fatalf("job=%+v, want the job failed", current)
	}
	var storedHistory models.GridHistory
	if err := database.First(&storedHistory, history.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedHistory.Status != "failed" || storedHistory.ErrorMsg == "" {
		t.Fatalf("history=%+v, want the failure mirrored into grid history", storedHistory)
	}
}

// The same provider-failure handling applies to video polling.
func TestPollVideoJobRecordsProviderFailure(t *testing.T) {
	database := generationDatabase(t)
	config := mockVideoConfig(t, 1)
	jobService := jobs.New(database)
	now := response.Now()
	record := models.VideoGeneration{OrganizationID: 1, Prompt: "async", TaskID: "task-1", ConfigID: &config.ID, Status: "processing", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	job, err := jobService.CreateQueuedOrganization(1, "video.generate", "video_generation", record.ID, "mock", &config.ID)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := jobService.ClaimWaiting("worker", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	runner := &AsyncRunner{Videos: &VideoService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}, Jobs: jobService}

	runner.pollVideoJob(claimed[0])

	var stored models.VideoGeneration
	if err := database.First(&stored, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" || stored.ErrorMsg == "" {
		t.Fatalf("record=%+v, want the provider failure persisted", stored)
	}
	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusFailed {
		t.Fatalf("job=%+v, want the job failed", current)
	}
}

// A missing AI config during polling must fail the claimed job rather than requeue it
// forever.
func TestPollJobsFailWhenConfigIsMissing(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	now := response.Now()
	missingConfig := uint(9999)
	record := models.ImageGeneration{OrganizationID: 1, Prompt: "async", TaskID: "task-1", ConfigID: &missingConfig, Status: "processing", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	job, err := jobService.CreateQueuedOrganization(1, "image.generate", "image_generation", record.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := jobService.ClaimWaiting("worker", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	runner := &AsyncRunner{Images: &ImageService{Store: storage.NewLocal(t.TempDir()), Jobs: jobService}, Jobs: jobService}

	runner.pollImageJob(claimed[0])

	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusFailed || current.LastError == "" {
		t.Fatalf("job=%+v, want the job failed for a missing config", current)
	}
}

// failClaimedJob has to work for legacy jobs that have no lease owner.
func TestFailClaimedJobHandlesUnleasedJob(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)
	job, err := jobService.CreateForTargetOrganization(1, "image.generate", "image_generation", 5, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	runner := &AsyncRunner{Jobs: jobService}

	runner.failClaimedJob(models.GenerationJob{ID: job.ID, OrganizationID: 1}, "image task id missing")

	current, err := jobService.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != jobs.StatusFailed || current.LastError != "image task id missing" {
		t.Fatalf("job=%+v, want the unleased job failed", current)
	}
}

// failClaimedJob must be a no-op when there is no job to fail.
func TestFailClaimedJobIgnoresMissingJobContext(t *testing.T) {
	database := generationDatabase(t)
	jobService := jobs.New(database)

	(&AsyncRunner{}).failClaimedJob(models.GenerationJob{ID: 1}, "no job service")
	(&AsyncRunner{Jobs: jobService}).failClaimedJob(models.GenerationJob{ID: 0}, "no job id")

	var count int64
	if err := database.Model(&models.GenerationJob{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("jobs=%d, want no job rows to have been touched", count)
	}
}

// A compose job for a storyboard that has no video must fail with a clear message.
func TestComposeStoryboardFailsWithoutVideo(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	storyboard := models.Storyboard{OrganizationID: 1, EpisodeID: 1, StoryboardNumber: 1, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	runner := &AsyncRunner{Store: storage.NewLocal(t.TempDir()), Jobs: jobs.New(database)}

	_, err := runner.composeStoryboard(context.Background(), 1, composeShotPayload{StoryboardID: storyboard.ID})
	if err == nil || err.Error() != "storyboard has no video" {
		t.Fatalf("err=%v, want a missing-video error", err)
	}
}

// A compose payload may not write outside the storage root.
func TestComposeStoryboardRejectsUnsafeOutputPath(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	store := storage.NewLocal(t.TempDir())
	rel, _, err := store.SaveBytes("videos", "shot.mp4", []byte("video bytes"))
	if err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{OrganizationID: 1, EpisodeID: 1, StoryboardNumber: 1, VideoURL: store.PublicURL(rel), CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	runner := &AsyncRunner{Store: store, Jobs: jobs.New(database)}

	payload := composeShotPayload{StoryboardID: storyboard.ID, VideoURL: store.PublicURL(rel), OutputRel: "../escape.mp4"}
	if _, err := runner.composeStoryboard(context.Background(), 1, payload); err == nil {
		t.Fatal("compose accepted an output path outside the storage root")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(store.Root), "escape.mp4")); !os.IsNotExist(err) {
		t.Fatalf("escape file was created: %v", err)
	}
}

// A storyboard belonging to another organization must not be composable.
func TestComposeStoryboardIsOrganizationScoped(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	storyboard := models.Storyboard{OrganizationID: 2, EpisodeID: 1, StoryboardNumber: 1, VideoURL: "/static/videos/other.mp4", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	runner := &AsyncRunner{Store: storage.NewLocal(t.TempDir()), Jobs: jobs.New(database)}

	if _, err := runner.composeStoryboard(context.Background(), 1, composeShotPayload{StoryboardID: storyboard.ID}); err == nil {
		t.Fatal("compose reached a storyboard from another organization")
	}
}

// Merging an episode with no storyboards has nothing to work with.
func TestMergeEpisodeFailsWithoutVideos(t *testing.T) {
	database := generationDatabase(t)
	runner := &AsyncRunner{Store: storage.NewLocal(t.TempDir()), Jobs: jobs.New(database)}

	_, err := runner.mergeEpisode(context.Background(), 1, 999, nil, "")
	if err == nil || err.Error() != "no videos to merge" {
		t.Fatalf("err=%v, want a no-videos error", err)
	}
}

// A storyboard without any usable video must abort the merge with its id named.
func TestMergeEpisodeFailsWhenStoryboardHasNoVideo(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	storyboard := models.Storyboard{OrganizationID: 1, EpisodeID: 5, StoryboardNumber: 1, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	runner := &AsyncRunner{Store: storage.NewLocal(t.TempDir()), Jobs: jobs.New(database)}

	_, err := runner.mergeEpisode(context.Background(), 1, 5, nil, "")
	if err == nil {
		t.Fatal("merge accepted a storyboard without a video")
	}
	if want := "has no video"; err != nil && !strings.Contains(err.Error(), want) {
		t.Fatalf("err=%v, want a message containing %q", err, want)
	}
}

// Explicit merge inputs must be resolved through storage, so an external URL is refused.
func TestMergeEpisodeRejectsExternalInputURL(t *testing.T) {
	database := generationDatabase(t)
	runner := &AsyncRunner{Store: storage.NewLocal(t.TempDir()), Jobs: jobs.New(database)}

	if _, err := runner.mergeEpisode(context.Background(), 1, 5, []string{"https://cdn.example/remote.mp4"}, ""); err == nil {
		t.Fatal("merge accepted an external input URL")
	}
}

// A merge output path outside the storage root must be rejected.
func TestMergeEpisodeRejectsUnsafeOutputPath(t *testing.T) {
	database := generationDatabase(t)
	store := storage.NewLocal(t.TempDir())
	rel, _, err := store.SaveBytes("videos", "clip.mp4", []byte("video bytes"))
	if err != nil {
		t.Fatal(err)
	}
	runner := &AsyncRunner{Store: store, Jobs: jobs.New(database)}

	if _, err := runner.mergeEpisode(context.Background(), 1, 5, []string{store.PublicURL(rel)}, "/etc/passwd.mp4"); err == nil {
		t.Fatal("merge accepted an absolute output path")
	}
}

// composeEpisode with no shots and no storyboards must report the empty episode.
func TestComposeEpisodeFailsWhenEpisodeIsEmpty(t *testing.T) {
	database := generationDatabase(t)
	runner := &AsyncRunner{Store: storage.NewLocal(t.TempDir()), Jobs: jobs.New(database)}

	_, err := runner.composeEpisode(context.Background(), 1, 404, nil)
	if err == nil || err.Error() != "episode has no storyboards" {
		t.Fatalf("err=%v, want an empty-episode error", err)
	}
}

// An explicit shot list that fails must abort the whole episode compose.
func TestComposeEpisodePropagatesShotFailure(t *testing.T) {
	database := generationDatabase(t)
	runner := &AsyncRunner{Store: storage.NewLocal(t.TempDir()), Jobs: jobs.New(database)}

	shots := []composeShotPayload{{StoryboardID: 4242, VideoURL: "/static/videos/missing.mp4"}}
	if _, err := runner.composeEpisode(context.Background(), 1, 7, shots); err == nil {
		t.Fatal("episode compose ignored a failing shot")
	}
}

// purgeCacheOnce needs both a cache and a store; missing wiring must be reported
// instead of silently skipping the purge.
func TestPurgeCacheOnceRequiresCacheAndStore(t *testing.T) {
	generationDatabase(t)
	if err := (&AsyncRunner{}).purgeCacheOnce(); err == nil {
		t.Fatal("purge accepted a runner without a cache")
	}
	if err := (&AsyncRunner{Store: storage.NewLocal(t.TempDir())}).purgeCacheOnce(); err == nil {
		t.Fatal("purge accepted a runner without a cache service")
	}
}

// registerAssetWithDB must ignore assets that carry no usable media reference.
func TestRegisterAssetSkipsIncompleteAssets(t *testing.T) {
	database := generationDatabase(t)

	if err := registerAssetWithDB(database, models.Asset{OrganizationID: 1, Type: "image"}); err != nil {
		t.Fatal(err)
	}
	if err := registerAssetWithDB(database, models.Asset{OrganizationID: 1, URL: "/static/images/a.png"}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := database.Model(&models.Asset{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("assets=%d, want incomplete assets skipped", count)
	}
}

// Assets tied to the same video generation must not be duplicated.
func TestRegisterAssetDeduplicatesByVideoGeneration(t *testing.T) {
	database := generationDatabase(t)
	videoGenID := uint(12)
	asset := models.Asset{OrganizationID: 1, Name: "生成视频", Type: "video", Category: "video", URL: "/static/videos/a.mp4", VideoGenID: &videoGenID}

	if err := registerAssetWithDB(database, asset); err != nil {
		t.Fatal(err)
	}
	second := asset
	second.URL = "/static/videos/b.mp4"
	if err := registerAssetWithDB(database, second); err != nil {
		t.Fatal(err)
	}

	var count int64
	if err := database.Model(&models.Asset{}).Where("video_gen_id = ?", videoGenID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("assets=%d, want one asset per video generation", count)
	}
}

// A storyboard-scoped asset is keyed by category and URL, so a different frame category
// is a distinct asset while an exact repeat is not.
func TestRegisterAssetScopesStoryboardAssetsByCategoryAndURL(t *testing.T) {
	database := generationDatabase(t)
	storyboardID := uint(31)
	base := models.Asset{OrganizationID: 1, Name: "镜头合成", Type: "video", Category: "composed", URL: "/static/composed/shot_31.mp4", StoryboardID: &storyboardID}

	if err := registerAssetWithDB(database, base); err != nil {
		t.Fatal(err)
	}
	if err := registerAssetWithDB(database, base); err != nil {
		t.Fatal(err)
	}
	other := base
	other.Category = "preview"
	if err := registerAssetWithDB(database, other); err != nil {
		t.Fatal(err)
	}

	var count int64
	if err := database.Model(&models.Asset{}).Where("storyboard_id = ?", storyboardID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("assets=%d, want the repeat deduplicated and the new category kept", count)
	}
}

// TTS must refuse storyboards that belong to another organization.
func TestTTSRejectsForeignOrganizationStoryboard(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	storyboard := models.Storyboard{OrganizationID: 2, EpisodeID: 1, StoryboardNumber: 1, Dialogue: "阿宁：你好", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	service := &TTSService{Store: storage.NewLocal(t.TempDir())}

	if _, err := service.GenerateForStoryboardOrganization(context.Background(), 1, storyboard.ID, nil); err == nil {
		t.Fatal("TTS reached a storyboard from another organization")
	}
	if _, err := service.GenerateForStoryboardOrganizationOwned(context.Background(), 1, storyboard.ID, nil, nil, 0, ""); err == nil {
		t.Fatal("owned TTS reached a storyboard from another organization")
	}
}

// Dialogue that carries no speakable content must be rejected before any provider call.
func TestTTSRejectsNonSpeakableDialogue(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	storyboard := models.Storyboard{OrganizationID: 1, EpisodeID: 1, StoryboardNumber: 1, Dialogue: "音效：风声", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	service := &TTSService{Store: storage.NewLocal(t.TempDir())}

	_, err := service.GenerateForStoryboard(context.Background(), storyboard.ID, nil)
	if err == nil || err.Error() != "no tts content" {
		t.Fatalf("err=%v, want the sound-effect line rejected", err)
	}
}

// A nil storyboard and a canceled context are both refused by the shared entry point.
func TestTTSGenerateForStoryboardGuardsInputs(t *testing.T) {
	generationDatabase(t)
	service := &TTSService{Store: storage.NewLocal(t.TempDir())}

	if _, err := service.generateForStoryboard(context.Background(), nil, nil, 1, nil, 0, ""); err == nil {
		t.Fatal("nil storyboard accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	storyboard := models.Storyboard{OrganizationID: 1, ID: 1, Dialogue: "阿宁：你好"}
	if _, err := service.generateForStoryboard(ctx, &storyboard, nil, 1, nil, 0, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
}

// An owned TTS job requires a job service to transition; without one it must refuse
// rather than write results for an unverifiable job.
func TestTTSOwnedJobRequiresJobService(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	storyboard := models.Storyboard{OrganizationID: 1, EpisodeID: 1, StoryboardNumber: 1, Dialogue: "阿宁：你好", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	mockAudioConfig(t, 1)
	service := &TTSService{Store: storage.NewLocal(t.TempDir())}

	_, err := service.GenerateForStoryboardOrganizationOwned(context.Background(), 1, storyboard.ID, nil, nil, 55, "worker")
	if !errors.Is(err, jobs.ErrTerminalJob) {
		t.Fatalf("err=%v, want jobs.ErrTerminalJob without a job service", err)
	}
}

// Voice previews must surface a missing audio configuration instead of returning a
// silent empty URL.
func TestVoicePreviewRequiresAudioConfig(t *testing.T) {
	generationDatabase(t)
	service := &TTSService{Store: storage.NewLocal(t.TempDir())}

	if _, err := service.GenerateVoiceSampleOrganization(context.Background(), 1, "阿宁", "voice-1", nil); err == nil {
		t.Fatal("voice sample accepted a missing audio config")
	}
	if _, err := service.GenerateVoicePreviewOrganization(context.Background(), 1, "试听文本", "voice-1", nil); err == nil {
		t.Fatal("voice preview accepted a missing audio config")
	}
}

// An unsupported audio provider must fail the preview.
func TestVoicePreviewFailsForUnsupportedProvider(t *testing.T) {
	database := generationDatabase(t)
	now := response.Now()
	config := models.AIServiceConfig{OrganizationID: 1, ServiceType: "audio", Provider: "openai", Name: "unsupported", BaseURL: "https://api.example.com", APIKey: "k", Model: "tts", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	service := &TTSService{Store: storage.NewLocal(t.TempDir())}

	if _, err := service.GenerateVoicePreviewOrganization(context.Background(), 1, "试听", "voice-1", &config.ID); err == nil {
		t.Fatal("preview accepted an unsupported audio provider")
	}
}

// EnsureLocalFile must reject empty and escaping paths.
func TestEnsureLocalFileRejectsUnsafePaths(t *testing.T) {
	store := storage.NewLocal(t.TempDir())
	for _, value := range []string{"", "../../etc/passwd", "https://cdn.example/a.mp4"} {
		if _, err := EnsureLocalFile(store, value); err == nil {
			t.Fatalf("EnsureLocalFile(%q) accepted an unsafe path", value)
		}
	}
}

func mockAudioConfig(t *testing.T, organizationID uint) models.AIServiceConfig {
	t.Helper()
	now := response.Now()
	config := models.AIServiceConfig{OrganizationID: organizationID, ServiceType: "audio", Provider: "mock", Name: "mock", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	return config
}
