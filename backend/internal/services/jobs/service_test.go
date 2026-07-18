package jobs

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
)

func testService(t *testing.T) *Service {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/jobs.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	return New(database)
}

func TestJobLifecycleAndTerminalGuard(t *testing.T) {
	service := testService(t)
	job, err := service.CreateForTarget("video.generate", "video_generation", 42, "vidu", uintPtr(7))
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusRunning || job.Attempt != 1 || job.ConfigID == nil || *job.ConfigID != 7 {
		t.Fatalf("unexpected created job: %+v", job)
	}
	if err := service.SetWaiting(job.ID, "provider-task"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetSucceededByTarget("video_generation", 42, `{"url":"/static/video.mp4"}`); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFailedByTarget("video_generation", 42, "late failure"); !errors.Is(err, ErrTerminalJob) {
		t.Fatalf("late transition error = %v, want ErrTerminalJob", err)
	}
	var stored models.GenerationJob
	if err := service.DB.First(&stored, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusSucceeded || stored.CompletedAt == nil {
		t.Fatalf("unexpected terminal job: %+v", stored)
	}
}

func TestCancelJobAlsoCancelsTarget(t *testing.T) {
	service := testService(t)
	now := now()
	video := models.VideoGeneration{Status: "processing", CreatedAt: now, UpdatedAt: now}
	if err := service.DB.Create(&video).Error; err != nil {
		t.Fatal(err)
	}
	job, err := service.CreateForTarget("video.generate", "video_generation", video.ID, "vidu", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.DB.First(&video, video.ID).Error; err != nil {
		t.Fatal(err)
	}
	if video.Status != "canceled" {
		t.Fatalf("target status = %q, want canceled", video.Status)
	}
	if err := service.SetSucceededByTarget("video_generation", video.ID, "{}"); !errors.Is(err, ErrTerminalJob) {
		t.Fatalf("completion after cancel error = %v", err)
	}
}

func TestCancelJobAlsoCancelsImageTarget(t *testing.T) {
	service := testService(t)
	nowText := now()
	image := models.ImageGeneration{Status: "processing", CreatedAt: nowText, UpdatedAt: nowText}
	if err := service.DB.Create(&image).Error; err != nil {
		t.Fatal(err)
	}
	job, err := service.CreateForTarget("image.generate", "image_generation", image.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.DB.First(&image, image.ID).Error; err != nil {
		t.Fatal(err)
	}
	if image.Status != "canceled" {
		t.Fatalf("image status=%q", image.Status)
	}
	if err := service.Cancel(9999); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("missing cancel error=%v", err)
	}
}

func TestJobEventsTrackLifecycle(t *testing.T) {
	service := testService(t)
	job, err := service.CreateForTargetOrganization(7, "video.generate", "video_generation", 43, "vidu", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetWaiting(job.ID, "provider-task"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFailed(job.ID, "provider rejected request"); err != nil {
		t.Fatal(err)
	}
	events, err := service.EventsOrganization(7, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("events len=%d want 3: %+v", len(events), events)
	}
	if events[0].Stage != StatusRunning || events[1].Stage != StatusWaitingProvider || events[2].Stage != StatusFailed {
		t.Fatalf("unexpected event stages: %+v", events)
	}
	if events[2].Message != "provider rejected request" {
		t.Fatalf("failure message=%q", events[2].Message)
	}
	if _, err := service.EventsOrganization(8, job.ID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("cross-organization events error=%v", err)
	}
}

func TestBatchCancelIsOrganizationScoped(t *testing.T) {
	service := testService(t)
	ownedA, err := service.CreateQueuedOrganization(10, "tts.generate", "storyboard_tts", 1, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	ownedB, err := service.CreateQueuedOrganization(10, "tts.generate", "storyboard_tts", 2, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.CreateQueuedOrganization(11, "tts.generate", "storyboard_tts", 3, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	canceled, failures, err := service.BatchCancelOrganization(10, []uint{ownedA.ID, ownedB.ID, other.ID, 999})
	if err != nil {
		t.Fatal(err)
	}
	if len(canceled) != 2 || len(failures) != 2 {
		t.Fatalf("canceled=%v failures=%v", canceled, failures)
	}
	currentOther, err := service.Get(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if currentOther.Status != StatusQueued {
		t.Fatalf("other organization job status=%q", currentOther.Status)
	}
}

func TestRecoverExpiredRequeuesProviderJob(t *testing.T) {
	service := testService(t)
	job, err := service.CreateForTarget("image.generate", "image_generation", 77, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if err := service.DB.Model(job).Updates(map[string]any{"status": StatusWaitingProvider, "lease_expires_at": expired, "provider_task_id": "task-77"}).Error; err != nil {
		t.Fatal(err)
	}
	count, err := service.RecoverExpired()
	if err != nil || count != 1 {
		t.Fatalf("recover count=%d err=%v", count, err)
	}
	recovered, err := service.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != StatusWaitingProvider || recovered.ProviderTaskID != "task-77" {
		t.Fatalf("unexpected recovered job: %+v", recovered)
	}
}

func TestRetryFailedJobResetsGenerationTarget(t *testing.T) {
	service := testService(t)
	nowText := now()
	image := models.ImageGeneration{Status: "failed", ErrorMsg: "temporary", TaskID: "old-task", CreatedAt: nowText, UpdatedAt: nowText}
	if err := service.DB.Create(&image).Error; err != nil {
		t.Fatal(err)
	}
	job, err := service.CreateForTarget("image.generate", "image_generation", image.ID, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DB.Model(job).Updates(map[string]any{"status": StatusFailed, "attempt": 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Retry(job.ID); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.AvailableAt <= nowText {
		t.Fatalf("retry was immediately available: now=%s available=%s", nowText, current.AvailableAt)
	}
	if err := service.DB.Model(current).Update("available_at", now()).Error; err != nil {
		t.Fatal(err)
	}
	var retried models.ImageGeneration
	if err := service.DB.First(&retried, image.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retried.Status != "pending" || retried.TaskID != "" || retried.ErrorMsg != "" {
		t.Fatalf("unexpected retried target: %+v", retried)
	}
	current, _ = service.Get(job.ID)
	if current.Status != StatusRunning || current.Attempt != 2 {
		t.Fatalf("unexpected retried job: %+v", current)
	}
}

func TestRetryBackoffIsBoundedAndExponential(t *testing.T) {
	if retryBackoff(1) != 5*time.Second || retryBackoff(2) != 10*time.Second || retryBackoff(3) != 20*time.Second {
		t.Fatalf("unexpected early backoff: %v %v %v", retryBackoff(1), retryBackoff(2), retryBackoff(3))
	}
	if retryBackoff(20) != 5*time.Minute {
		t.Fatalf("backoff not capped: %v", retryBackoff(20))
	}
}

func TestClaimWaitingIsExclusive(t *testing.T) {
	service := testService(t)
	job, err := service.CreateForTarget("video.generate", "video_generation", 101, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetWaiting(job.ID, "provider-task"); err != nil {
		t.Fatal(err)
	}
	first, err := service.ClaimWaiting("worker-a", 10)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim len=%d err=%v", len(first), err)
	}
	second, err := service.ClaimWaiting("worker-b", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("second worker claimed %d jobs", len(second))
	}
}

func TestRetryQueuedTTSJobCanBeClaimed(t *testing.T) {
	service := testService(t)
	job, err := service.CreateQueued("tts.generate", "storyboard_tts", 202, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DB.Model(job).Updates(map[string]any{"status": StatusFailed, "attempt": 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Retry(job.ID); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != StatusQueued || current.Attempt != 2 {
		t.Fatalf("unexpected retried tts job: %+v", current)
	}
	if err := service.DB.Model(current).Update("available_at", now()).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := service.ClaimWaiting("tts-worker", 10)
	if err != nil || len(claimed) != 1 || claimed[0].ID != job.ID {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
}

func TestComposeJobPersistsPayloadAndRetryCanBeClaimed(t *testing.T) {
	service := testService(t)
	payload := `{"episode_id":303}`
	job, err := service.CreateQueuedPayload("episode_merge", "episode_merge", 303, "ffmpeg", nil, payload)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusQueued || job.PayloadJSON != payload {
		t.Fatalf("unexpected queued compose job: %+v", job)
	}
	if err := service.DB.Model(job).Updates(map[string]any{"status": StatusFailed, "last_error": "ffmpeg failed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Retry(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.DB.Model(job).Update("available_at", now()).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := service.ClaimWaiting("compose-worker", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].ID != job.ID || claimed[0].PayloadJSON != payload || claimed[0].Attempt != 2 {
		t.Fatalf("unexpected claimed compose job: %+v", claimed)
	}
}

func TestCreateQueuedPayloadConcurrentIsIdempotent(t *testing.T) {
	service := testService(t)
	const workers = 12
	jobsCreated := make(chan *models.GenerationJob, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, err := service.CreateQueuedPayload("episode_merge", "episode_merge", 909, "ffmpeg", nil, `{"episode_id":909}`)
			jobsCreated <- job
			errs <- err
		}()
	}
	wg.Wait()
	close(jobsCreated)
	close(errs)
	var first uint
	for job := range jobsCreated {
		if job == nil {
			t.Fatal("nil job returned")
		}
		if first == 0 {
			first = job.ID
		} else if job.ID != first {
			t.Fatalf("duplicate active jobs: got %d and %d", first, job.ID)
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	service.DB.Model(&models.GenerationJob{}).Where("target_type = ? AND target_id = ? AND status NOT IN ?", "episode_merge", 909, []string{StatusSucceeded, StatusFailed, StatusCanceled}).Count(&count)
	if count != 1 {
		t.Fatalf("active job count = %d, want 1", count)
	}
}

func TestOrganizationQuotaLimitsActiveAndDailyJobs(t *testing.T) {
	service := testService(t)
	nowText := now()
	quota := models.OrganizationQuota{OrganizationID: 9, DailyJobLimit: 2, MaxActiveJobs: 1, CreatedAt: nowText, UpdatedAt: nowText}
	if err := service.DB.Create(&quota).Error; err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateForTargetOrganization(9, "image.generate", "image_generation", 1, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	idempotent, err := service.CreateForTargetOrganization(9, "image.generate", "image_generation", 1, "mock", nil)
	if err != nil || idempotent.ID != first.ID {
		t.Fatalf("idempotent job=%v err=%v", idempotent, err)
	}
	if _, err := service.CreateForTargetOrganization(9, "video.generate", "video_generation", 2, "mock", nil); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("active quota error=%v", err)
	}
	if err := service.SetSucceededByTargetOrganization(9, "image_generation", 1, "{}"); err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateForTargetOrganization(9, "video.generate", "video_generation", 2, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetSucceededByTargetOrganization(9, "video_generation", second.TargetID, "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateForTargetOrganization(9, "audio.generate", "storyboard_tts", 3, "mock", nil); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("daily quota error=%v", err)
	}
	if _, err := service.CreateForTargetOrganization(10, "audio.generate", "storyboard_tts", 3, "mock", nil); err != nil {
		t.Fatalf("other organization was limited: %v", err)
	}
}

func TestCostEstimationAndBudgetReservation(t *testing.T) {
	if EstimateCost("image.generate", "image_generation", "openai") != 0.04 || EstimateCost("video.generate", "video_generation", "volcengine") != 0.20 || EstimateCost("tts.generate", "storyboard_tts", "minimax") != 0.02 || EstimateCost("image.generate", "image_generation", "mock") != 0 {
		t.Fatal("provider cost defaults are incorrect")
	}
	service := testService(t)
	nowText := now()
	quota := models.OrganizationQuota{OrganizationID: 44, DailyJobLimit: 100, MaxActiveJobs: 100, DailyBudgetCNY: 0.10, BudgetWarningPercent: 80, CreatedAt: nowText, UpdatedAt: nowText}
	if err := service.DB.Create(&quota).Error; err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateForTargetOrganization(44, "image.generate", "image_generation", 1, "openai", nil)
	if err != nil || first.EstimatedCost != 0.04 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if err := service.SetSucceeded(first.ID, "{}"); err != nil {
		t.Fatal(err)
	}
	var stored models.GenerationJob
	if err := service.DB.First(&stored, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ActualCost != 0.04 {
		t.Fatalf("actual cost=%v", stored.ActualCost)
	}
	var cached models.MediaCacheObject
	if err := service.DB.Joins("JOIN media_cache_references ON media_cache_references.object_id = media_cache_objects.id").
		Where("media_cache_references.organization_id = ? AND media_cache_references.namespace = ? AND media_cache_references.cache_key = ?", 44, "job_result", strconv.FormatUint(uint64(first.ID), 10)).First(&cached).Error; err != nil {
		t.Fatalf("job result was not cached: %v", err)
	}
	if cached.Payload != "{}" {
		t.Fatalf("cached result=%q", cached.Payload)
	}
	for id := uint(2); id < 10; id++ {
		if _, err := service.CreateForTargetOrganization(44, "image.generate", "image_generation", id, "openai", nil); err != nil {
			if !errors.Is(err, ErrBudgetExceeded) {
				t.Fatalf("budget error=%v", err)
			}
			return
		}
	}
	t.Fatal("budget reservation did not stop generation")
}

func TestJobWrappersListsAndOwnedFailure(t *testing.T) {
	service := testService(t)
	payloadJob, err := service.CreateQueuedPayloadOrganization(30, "episode.merge", "episode_merge", 1, "ffmpeg", nil, `{"episode_id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.CreateForTargetOrganization(31, "image.generate", "image_generation", 2, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	all, err := service.List("", "", 0)
	if err != nil || len(all) != 2 {
		t.Fatalf("all=%v err=%v", all, err)
	}
	owned, err := service.ListOrganization(30, StatusQueued, "episode.merge", 500)
	if err != nil || len(owned) != 1 || owned[0].ID != payloadJob.ID {
		t.Fatalf("owned=%v err=%v", owned, err)
	}
	claimed, err := service.ClaimWaiting("worker-owned", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	if !service.IsOwned(payloadJob.ID, "worker-owned") || service.IsOwned(payloadJob.ID, "other") {
		t.Fatal("owned lease check failed")
	}
	if err := service.SetFailedOwned(payloadJob.ID, "worker-owned", "provider failed"); err != nil {
		t.Fatal(err)
	}
	if service.IsOwned(payloadJob.ID, "worker-owned") {
		t.Fatal("terminal job remained owned")
	}
	if err := service.SetSucceeded(other.ID, `{}`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(9999); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("missing get error=%v", err)
	}
	if err := service.SetFailedByTargetOrganization(99, "missing", 1, "missing"); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("missing target error=%v", err)
	}
	successByTarget, err := service.CreateForTarget("image.generate", "image_generation", 41, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetSucceededByTarget(successByTarget.TargetType, successByTarget.TargetID, `{}`); err != nil {
		t.Fatal(err)
	}
	failureByTarget, err := service.CreateForTarget("video.generate", "video_generation", 42, "mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetFailedByTarget(failureByTarget.TargetType, failureByTarget.TargetID, "failed"); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedLeaseFencing(t *testing.T) {
	service := testService(t)
	job, err := service.CreateQueuedPayload("episode_merge", "episode_merge", 910, "ffmpeg", nil, `{"episode_id":910}`)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.ClaimWaiting("owner-a", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %#v, err=%v", claimed, err)
	}
	if err := service.RenewLease(job.ID, "owner-b"); !errors.Is(err, ErrTerminalJob) {
		t.Fatalf("wrong-owner renew = %v", err)
	}
	if err := service.SetSucceededOwned(job.ID, "owner-b", "{}"); !errors.Is(err, ErrTerminalJob) {
		t.Fatalf("wrong-owner success = %v", err)
	}
	if err := service.RenewLease(job.ID, "owner-a"); err != nil {
		t.Fatalf("owner renew = %v", err)
	}
	if err := service.SetSucceededOwned(job.ID, "owner-a", "{}"); err != nil {
		t.Fatalf("owner success = %v", err)
	}
}

func TestRecoverExpiredComposeJobRequeuesPayload(t *testing.T) {
	service := testService(t)
	payload := `{"storyboard_id":404,"video_url":"/static/videos/input.mp4"}`
	job, err := service.CreateQueuedPayload("shot_compose", "storyboard_compose", 404, "ffmpeg", nil, payload)
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if err := service.DB.Model(job).Updates(map[string]any{"status": StatusRunning, "lease_owner": "dead-worker", "lease_expires_at": expired}).Error; err != nil {
		t.Fatal(err)
	}
	count, err := service.RecoverExpired()
	if err != nil || count != 1 {
		t.Fatalf("recover count=%d err=%v", count, err)
	}
	recovered, err := service.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != StatusQueued || recovered.PayloadJSON != payload || recovered.LeaseOwner != "" {
		t.Fatalf("unexpected recovered compose job: %+v", recovered)
	}
}

func uintPtr(value uint) *uint { return &value }
