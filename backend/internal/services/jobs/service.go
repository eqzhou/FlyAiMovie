package jobs

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/eqzhou/flyaimovie/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	StatusQueued          = "queued"
	StatusRunning         = "running"
	StatusWaitingProvider = "waiting_provider"
	StatusSucceeded       = "succeeded"
	StatusFailed          = "failed"
	StatusCanceled        = "canceled"
	leaseDuration         = 2 * time.Minute
)

var (
	ErrJobNotFound    = errors.New("job not found")
	ErrTerminalJob    = errors.New("terminal job cannot transition")
	ErrQuotaExceeded  = errors.New("generation quota exceeded")
	ErrBudgetExceeded = errors.New("generation budget exceeded")
)

type Service struct {
	DB *gorm.DB
}

var createMu sync.Mutex

func New(database *gorm.DB) *Service {
	return &Service{DB: database}
}

func (s *Service) CreateForTarget(kind, targetType string, targetID uint, provider string, configID *uint) (*models.GenerationJob, error) {
	return s.create(0, kind, targetType, targetID, provider, configID, StatusRunning, "")
}

func (s *Service) CreateForTargetOrganization(organizationID uint, kind, targetType string, targetID uint, provider string, configID *uint) (*models.GenerationJob, error) {
	return s.create(organizationID, kind, targetType, targetID, provider, configID, StatusRunning, "")
}

func (s *Service) CreateQueued(kind, targetType string, targetID uint, provider string, configID *uint) (*models.GenerationJob, error) {
	return s.create(0, kind, targetType, targetID, provider, configID, StatusQueued, "")
}

func (s *Service) CreateQueuedOrganization(organizationID uint, kind, targetType string, targetID uint, provider string, configID *uint) (*models.GenerationJob, error) {
	return s.create(organizationID, kind, targetType, targetID, provider, configID, StatusQueued, "")
}

// CreateQueuedPayload creates a durable queued job and stores all inputs needed
// by a worker. The payload is written in the same insert as the job so a worker
// cannot claim an incomplete request.
func (s *Service) CreateQueuedPayload(kind, targetType string, targetID uint, provider string, configID *uint, payload string) (*models.GenerationJob, error) {
	return s.create(0, kind, targetType, targetID, provider, configID, StatusQueued, payload)
}

func (s *Service) CreateQueuedPayloadOrganization(organizationID uint, kind, targetType string, targetID uint, provider string, configID *uint, payload string) (*models.GenerationJob, error) {
	return s.create(organizationID, kind, targetType, targetID, provider, configID, StatusQueued, payload)
}

func (s *Service) create(organizationID uint, kind, targetType string, targetID uint, provider string, configID *uint, initialStatus, payload string) (*models.GenerationJob, error) {
	if kind == "" || targetType == "" || targetID == 0 {
		return nil, fmt.Errorf("invalid job target")
	}
	createMu.Lock()
	defer createMu.Unlock()
	var job models.GenerationJob
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var existing models.GenerationJob
		if err := tx.Where("organization_id = ? AND target_type = ? AND target_id = ? AND status NOT IN ?", organizationID, targetType, targetID, []string{StatusSucceeded, StatusFailed, StatusCanceled}).Order("id desc").First(&existing).Error; err == nil {
			job = existing
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		estimatedCost := EstimateCost(kind, targetType, provider)
		if err := enforceQuota(tx, organizationID, estimatedCost); err != nil {
			return err
		}
		timestamp := now()
		leaseExpiry := time.Now().UTC().Add(leaseDuration).Format(time.RFC3339)
		job = models.GenerationJob{
			OrganizationID: organizationID, Kind: kind, Status: initialStatus, TargetType: targetType, TargetID: targetID,
			ConfigID: configID, Provider: provider, Attempt: 1, MaxAttempts: 3,
			Progress: 1, Stage: initialStatus, StatusMessage: "job created", Currency: "CNY", AvailableAt: timestamp, LeaseExpiresAt: &leaseExpiry, PayloadJSON: payload,
			EstimatedCost: estimatedCost,
			CreatedAt:     timestamp, UpdatedAt: timestamp,
		}
		if initialStatus == StatusRunning {
			job.StartedAt = &timestamp
		}
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		return tx.Create(&models.JobEvent{OrganizationID: organizationID, JobID: job.ID, Stage: initialStatus, Progress: 1, Level: "info", Message: "job created", CreatedAt: timestamp}).Error
	})
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func enforceQuota(tx *gorm.DB, organizationID uint, estimatedCost float64) error {
	if organizationID == 0 {
		return nil
	}
	quota := models.OrganizationQuota{OrganizationID: organizationID, DailyJobLimit: 200, MaxActiveJobs: 10, BudgetWarningPercent: 80}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ?", organizationID).First(&quota).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var active int64
	if err := tx.Model(&models.GenerationJob{}).Where("organization_id = ? AND status NOT IN ?", organizationID, []string{StatusSucceeded, StatusFailed, StatusCanceled}).Count(&active).Error; err != nil {
		return err
	}
	if quota.MaxActiveJobs > 0 && active >= int64(quota.MaxActiveJobs) {
		return fmt.Errorf("%w: active job limit reached", ErrQuotaExceeded)
	}
	startOfDay := time.Now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339)
	var daily int64
	if err := tx.Model(&models.GenerationJob{}).Where("organization_id = ? AND created_at >= ?", organizationID, startOfDay).Count(&daily).Error; err != nil {
		return err
	}
	if quota.DailyJobLimit > 0 && daily >= int64(quota.DailyJobLimit) {
		return fmt.Errorf("%w: daily job limit reached", ErrQuotaExceeded)
	}
	if quota.DailyBudgetCNY > 0 && estimatedCost > 0 {
		var reserved float64
		if err := tx.Model(&models.GenerationJob{}).
			Where("organization_id = ? AND created_at >= ? AND status NOT IN ?", organizationID, startOfDay, []string{StatusSucceeded, StatusFailed, StatusCanceled}).
			Select("COALESCE(SUM(estimated_cost), 0)").Scan(&reserved).Error; err != nil {
			return err
		}
		var spent float64
		if err := tx.Model(&models.GenerationJob{}).
			Where("organization_id = ? AND created_at >= ?", organizationID, startOfDay).
			Select("COALESCE(SUM(actual_cost), 0)").Scan(&spent).Error; err != nil {
			return err
		}
		if spent+reserved+estimatedCost > quota.DailyBudgetCNY {
			return fmt.Errorf("%w: daily budget %.2f CNY exceeded", ErrBudgetExceeded, quota.DailyBudgetCNY)
		}
	}
	return nil
}

// EstimateCost is deliberately conservative and provider-specific. It gives
// the organization budget a deterministic reservation before a provider call;
// a later provider billing integration can replace these defaults.
func EstimateCost(kind, targetType, provider string) float64 {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" || provider == "mock" {
		return 0
	}
	switch targetType {
	case "image_generation":
		switch provider {
		case "openai":
			return 0.04
		case "gemini", "chatfire", "ali", "volcengine":
			return 0.03
		case "minimax":
			return 0.05
		}
	case "video_generation":
		switch provider {
		case "openai":
			return 0.50
		case "minimax":
			return 0.30
		case "volcengine", "vidu", "ali":
			return 0.20
		}
	case "storyboard_tts":
		if provider == "minimax" {
			return 0.02
		}
	case "storyboard_compose", "episode_compose", "episode_merge":
		return 0.01
	}
	if strings.Contains(strings.ToLower(kind), "tts") && provider == "minimax" {
		return 0.02
	}
	return 0
}

// RenewLease extends a worker lease only while that worker still owns it.
func (s *Service) RenewLease(id uint, owner string) error {
	if owner == "" {
		return fmt.Errorf("worker owner is required")
	}
	expiry := time.Now().UTC().Add(leaseDuration).Format(time.RFC3339)
	result := s.DB.Model(&models.GenerationJob{}).
		Where("id = ? AND status = ? AND lease_owner = ?", id, StatusRunning, owner).
		Updates(map[string]any{"lease_expires_at": expiry, "updated_at": now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTerminalJob
	}
	return nil
}

func (s *Service) transitionOwned(id uint, owner, target string, extra map[string]any) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		return New(tx).transitionOwnedNoTx(id, owner, target, extra)
	})
}

func (s *Service) transitionOwnedNoTx(id uint, owner, target string, extra map[string]any) error {
	if owner == "" {
		return fmt.Errorf("worker owner is required")
	}
	var job models.GenerationJob
	if err := s.DB.First(&job, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrJobNotFound
		}
		return err
	}
	if isTerminal(job.Status) || job.Status != StatusRunning || job.LeaseOwner != owner {
		return ErrTerminalJob
	}
	updates := map[string]any{"status": target, "updated_at": now()}
	for k, v := range extra {
		updates[k] = v
	}
	if isTerminal(target) {
		updates["completed_at"] = now()
		updates["lease_owner"] = ""
		updates["lease_expires_at"] = nil
		if target == StatusSucceeded {
			updates["actual_cost"] = job.EstimatedCost
		}
	}
	applyStageFields(updates, target)
	result := s.DB.Model(&models.GenerationJob{}).
		Where("id = ? AND status = ? AND lease_owner = ?", id, StatusRunning, owner).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTerminalJob
	}
	return s.appendEvent(job.OrganizationID, id, target, eventProgress(target, extra), eventMessage(target, extra))
}

func (s *Service) SetSucceededOwned(id uint, owner, resultJSON string) error {
	return s.transitionOwned(id, owner, StatusSucceeded, map[string]any{"result_json": resultJSON, "progress": 100})
}

func (s *Service) SetFailedOwned(id uint, owner, message string) error {
	return s.transitionOwned(id, owner, StatusFailed, map[string]any{"last_error": message})
}

func (s *Service) IsOwned(id uint, owner string) bool {
	if owner == "" {
		return false
	}
	var job models.GenerationJob
	return s.DB.Select("id").Where("id = ? AND status = ? AND lease_owner = ?", id, StatusRunning, owner).First(&job).Error == nil
}

func (s *Service) SetWaiting(id uint, providerTaskID string) error {
	leaseExpiry := time.Now().UTC().Add(leaseDuration).Format(time.RFC3339)
	timestamp := now()
	return s.transition(id, StatusWaitingProvider, map[string]any{
		"provider_task_id": providerTaskID,
		"progress":         10,
		"lease_expires_at": leaseExpiry,
		"available_at":     timestamp,
		"lease_owner":      "",
	})
}

// ClaimWaiting atomically leases provider polling jobs. Only one worker can
// claim a row because the conditional update checks the current status and
// lease expiry in the same transaction.
func (s *Service) ClaimWaiting(owner string, limit int) ([]models.GenerationJob, error) {
	if owner == "" {
		return nil, fmt.Errorf("worker owner is required")
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	nowText := now()
	var candidates []models.GenerationJob
	if err := s.DB.Where("status IN ? AND available_at <= ? AND (lease_owner = '' OR lease_owner IS NULL OR lease_expires_at IS NULL OR lease_expires_at < ?)", []string{StatusQueued, StatusWaitingProvider}, nowText, nowText).
		Order("id asc").Limit(limit).Find(&candidates).Error; err != nil {
		return nil, err
	}
	claimed := make([]models.GenerationJob, 0, len(candidates))
	leaseExpiry := time.Now().UTC().Add(leaseDuration).Format(time.RFC3339)
	for _, candidate := range candidates {
		result := s.DB.Model(&models.GenerationJob{}).
			Where("id = ? AND status IN ? AND (lease_owner = '' OR lease_owner IS NULL OR lease_expires_at IS NULL OR lease_expires_at < ?)", candidate.ID, []string{StatusQueued, StatusWaitingProvider}, nowText).
			Updates(map[string]any{"status": StatusRunning, "lease_owner": owner, "lease_expires_at": leaseExpiry, "updated_at": nowText})
		if result.Error != nil {
			return claimed, result.Error
		}
		if result.RowsAffected == 1 {
			candidate.Status = StatusRunning
			candidate.LeaseOwner = owner
			candidate.LeaseExpiresAt = &leaseExpiry
			claimed = append(claimed, candidate)
		}
	}
	return claimed, nil
}

// RecoverExpired reopens jobs whose process lease elapsed. The generation rows
// remain the source of truth, so the next poll cycle can finish an already
// accepted provider task without submitting it again.
func (s *Service) RecoverExpired() (int64, error) {
	nowText := now()
	var recovered int64
	workerTargets := []string{"storyboard_tts", "storyboard_compose", "episode_compose", "episode_merge"}
	queued := s.DB.Model(&models.GenerationJob{}).
		Where("status = ? AND target_type IN ? AND lease_expires_at IS NOT NULL AND lease_expires_at < ?", StatusRunning, workerTargets, nowText).
		Updates(map[string]any{"status": StatusQueued, "available_at": nowText, "lease_owner": "", "lease_expires_at": nil, "updated_at": nowText})
	if queued.Error != nil {
		return 0, queued.Error
	}
	recovered += queued.RowsAffected
	waiting := s.DB.Model(&models.GenerationJob{}).
		Where("status IN ? AND lease_expires_at IS NOT NULL AND lease_expires_at < ? AND (provider_task_id != '' OR target_type IN ?)", []string{StatusRunning, StatusWaitingProvider}, nowText, []string{"storyboard_compose", "episode_compose", "episode_merge"}).
		Updates(map[string]any{"status": StatusWaitingProvider, "available_at": nowText, "lease_owner": "", "lease_expires_at": nil, "updated_at": nowText})
	if waiting.Error != nil {
		return 0, waiting.Error
	}
	recovered += waiting.RowsAffected
	composeQueued := s.DB.Model(&models.GenerationJob{}).
		Where("status = ? AND target_type IN ? AND updated_at = ?", StatusWaitingProvider, []string{"storyboard_compose", "episode_compose", "episode_merge"}, nowText).
		Updates(map[string]any{"status": StatusQueued})
	if composeQueued.Error != nil {
		return recovered, composeQueued.Error
	}
	failed := s.DB.Model(&models.GenerationJob{}).
		Where("status = ? AND target_type NOT IN ? AND lease_expires_at IS NOT NULL AND lease_expires_at < ? AND (provider_task_id = '' OR provider_task_id IS NULL)", StatusRunning, workerTargets, nowText).
		Updates(map[string]any{"status": StatusFailed, "last_error": "worker stopped before provider task was recorded", "completed_at": nowText, "lease_owner": "", "lease_expires_at": nil, "updated_at": nowText})
	if failed.Error != nil {
		return recovered, failed.Error
	}
	return recovered + failed.RowsAffected, nil
}

func (s *Service) SetSucceededByTarget(targetType string, targetID uint, resultJSON string) error {
	job, err := s.byTarget(targetType, targetID)
	if err != nil {
		return err
	}
	return s.transition(job.ID, StatusSucceeded, map[string]any{"result_json": resultJSON, "progress": 100})
}

func (s *Service) SetSucceededByTargetOrganization(organizationID uint, targetType string, targetID uint, resultJSON string) error {
	job, err := s.byTargetOrganization(organizationID, targetType, targetID)
	if err != nil {
		return err
	}
	return s.transition(job.ID, StatusSucceeded, map[string]any{"result_json": resultJSON, "progress": 100})
}

func (s *Service) SetFailedByTarget(targetType string, targetID uint, message string) error {
	job, err := s.byTarget(targetType, targetID)
	if err != nil {
		return err
	}
	return s.transition(job.ID, StatusFailed, map[string]any{"last_error": message})
}

func (s *Service) SetFailedByTargetOrganization(organizationID uint, targetType string, targetID uint, message string) error {
	job, err := s.byTargetOrganization(organizationID, targetType, targetID)
	if err != nil {
		return err
	}
	return s.transition(job.ID, StatusFailed, map[string]any{"last_error": message})
}

func (s *Service) SetSucceeded(id uint, resultJSON string) error {
	return s.transition(id, StatusSucceeded, map[string]any{"result_json": resultJSON, "progress": 100})
}

func (s *Service) SetFailed(id uint, message string) error {
	return s.transition(id, StatusFailed, map[string]any{"last_error": message})
}

func (s *Service) Cancel(id uint) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		service := New(tx)
		var job models.GenerationJob
		if err := tx.First(&job, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrJobNotFound
			}
			return err
		}
		if isTerminal(job.Status) {
			return ErrTerminalJob
		}
		if err := service.transition(job.ID, StatusCanceled, nil); err != nil {
			return err
		}
		timestamp := now()
		switch job.TargetType {
		case "image_generation":
			return tx.Model(&models.ImageGeneration{}).Where("id = ? AND status NOT IN ?", job.TargetID, terminalResourceStatuses()).Updates(map[string]any{"status": "canceled", "updated_at": timestamp}).Error
		case "video_generation":
			return tx.Model(&models.VideoGeneration{}).Where("id = ? AND status NOT IN ?", job.TargetID, terminalResourceStatuses()).Updates(map[string]any{"status": "canceled", "updated_at": timestamp}).Error
		default:
			return nil
		}
	})
}

// Retry requeues a failed or canceled generation target without reusing a
// provider task id. The next generation call submits a fresh provider task.
func (s *Service) Retry(id uint) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var job models.GenerationJob
		if err := tx.First(&job, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrJobNotFound
			}
			return err
		}
		if job.Status != StatusFailed && job.Status != StatusCanceled {
			return fmt.Errorf("job is not retryable")
		}
		if job.Attempt >= job.MaxAttempts {
			return fmt.Errorf("retry limit reached")
		}
		timestamp := now()
		availableAt := time.Now().UTC().Add(retryBackoff(job.Attempt)).Format(time.RFC3339)
		nextStatus := StatusRunning
		if job.TargetType == "storyboard_tts" || job.TargetType == "storyboard_compose" || job.TargetType == "episode_compose" || job.TargetType == "episode_merge" {
			nextStatus = StatusQueued
		}
		result := tx.Model(&models.GenerationJob{}).Where("id = ? AND status = ?", id, job.Status).Updates(map[string]any{
			"status": nextStatus, "attempt": job.Attempt + 1, "progress": 1,
			"stage": nextStatus, "status_message": "job retry scheduled",
			"provider_task_id": "", "last_error": "", "result_json": "",
			"available_at": availableAt, "started_at": timestamp, "completed_at": nil,
			"cancel_requested_at": nil, "updated_at": timestamp,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("job changed concurrently")
		}
		if err := tx.Create(&models.JobEvent{OrganizationID: job.OrganizationID, JobID: job.ID, Stage: nextStatus, Progress: 1, Level: "info", Message: "job retry scheduled", CreatedAt: timestamp}).Error; err != nil {
			return err
		}
		switch job.TargetType {
		case "image_generation":
			return tx.Model(&models.ImageGeneration{}).Where("id = ?", job.TargetID).Updates(map[string]any{"status": "pending", "task_id": "", "error_msg": "", "completed_at": nil, "updated_at": timestamp}).Error
		case "video_generation":
			return tx.Model(&models.VideoGeneration{}).Where("id = ?", job.TargetID).Updates(map[string]any{"status": "pending", "task_id": "", "error_msg": "", "completed_at": nil, "updated_at": timestamp}).Error
		default:
			return nil
		}
	})
}

func (s *Service) EventsOrganization(organizationID, jobID uint) ([]models.JobEvent, error) {
	if _, err := s.GetOrganization(organizationID, jobID); err != nil {
		return nil, err
	}
	var events []models.JobEvent
	err := s.DB.Where("organization_id = ? AND job_id = ?", organizationID, jobID).Order("id asc").Find(&events).Error
	return events, err
}

func (s *Service) BatchCancelOrganization(organizationID uint, ids []uint) ([]uint, map[uint]string, error) {
	if len(ids) > 100 {
		return nil, nil, fmt.Errorf("at most 100 jobs can be canceled at once")
	}
	canceled := make([]uint, 0, len(ids))
	failures := make(map[uint]string)
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			failures[id] = "invalid job id"
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if _, err := s.GetOrganization(organizationID, id); err != nil {
			failures[id] = ErrJobNotFound.Error()
			continue
		}
		if err := s.Cancel(id); err != nil {
			failures[id] = err.Error()
			continue
		}
		canceled = append(canceled, id)
	}
	return canceled, failures, nil
}

// retryBackoff spaces repeated provider submissions while keeping the delay
// bounded. Attempt is the attempt that just failed/canceled.
func retryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 5 * time.Second
	for i := 1; i < attempt; i++ {
		if delay >= 5*time.Minute/2 {
			return 5 * time.Minute
		}
		delay *= 2
	}
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func (s *Service) Get(id uint) (*models.GenerationJob, error) {
	var job models.GenerationJob
	if err := s.DB.First(&job, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}
	return &job, nil
}

func (s *Service) GetOrganization(organizationID, id uint) (*models.GenerationJob, error) {
	var job models.GenerationJob
	if err := s.DB.Where("organization_id = ?", organizationID).First(&job, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}
	return &job, nil
}

func (s *Service) List(status, kind string, limit int) ([]models.GenerationJob, error) {
	return s.list(0, false, status, kind, limit)
}

func (s *Service) ListOrganization(organizationID uint, status, kind string, limit int) ([]models.GenerationJob, error) {
	return s.list(organizationID, true, status, kind, limit)
}

func (s *Service) list(organizationID uint, scoped bool, status, kind string, limit int) ([]models.GenerationJob, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	query := s.DB.Order("id desc").Limit(limit)
	if scoped {
		query = query.Where("organization_id = ?", organizationID)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if kind != "" {
		query = query.Where("kind = ?", kind)
	}
	var rows []models.GenerationJob
	return rows, query.Find(&rows).Error
}

func (s *Service) byTarget(targetType string, targetID uint) (*models.GenerationJob, error) {
	var job models.GenerationJob
	if err := s.DB.Where("target_type = ? AND target_id = ?", targetType, targetID).Order("id desc").First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}
	return &job, nil
}

func (s *Service) byTargetOrganization(organizationID uint, targetType string, targetID uint) (*models.GenerationJob, error) {
	var job models.GenerationJob
	query := s.DB.Where("organization_id = ? AND target_type = ? AND target_id = ?", organizationID, targetType, targetID)
	if err := query.Order("id desc").First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}
	return &job, nil
}

func (s *Service) transition(id uint, target string, extra map[string]any) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		return New(tx).transitionNoTx(id, target, extra)
	})
}

func (s *Service) transitionNoTx(id uint, target string, extra map[string]any) error {
	var job models.GenerationJob
	if err := s.DB.First(&job, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrJobNotFound
		}
		return err
	}
	if job.Status == target {
		return nil
	}
	if isTerminal(job.Status) || !allowedTransition(job.Status, target) {
		return ErrTerminalJob
	}
	timestamp := now()
	updates := map[string]any{"status": target, "updated_at": timestamp}
	for key, value := range extra {
		updates[key] = value
	}
	if isTerminal(target) {
		updates["completed_at"] = timestamp
		updates["lease_owner"] = ""
		updates["lease_expires_at"] = nil
		if target == StatusSucceeded {
			updates["actual_cost"] = job.EstimatedCost
		}
	}
	applyStageFields(updates, target)
	result := s.DB.Model(&models.GenerationJob{}).
		Where("id = ? AND status = ?", id, job.Status).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("job changed concurrently")
	}
	return s.appendEvent(job.OrganizationID, id, target, eventProgress(target, extra), eventMessage(target, extra))
}

func applyStageFields(updates map[string]any, target string) {
	updates["stage"] = target
	updates["status_message"] = eventMessage(target, updates)
}

func eventProgress(target string, extra map[string]any) int {
	if value, ok := extra["progress"].(int); ok {
		return value
	}
	if target == StatusSucceeded {
		return 100
	}
	return 0
}

func eventMessage(target string, extra map[string]any) string {
	if value, ok := extra["last_error"].(string); ok && value != "" {
		return value
	}
	switch target {
	case StatusWaitingProvider:
		return "waiting for provider"
	case StatusSucceeded:
		return "job completed"
	case StatusFailed:
		return "job failed"
	case StatusCanceled:
		return "job canceled"
	default:
		return target
	}
}

func (s *Service) appendEvent(organizationID, jobID uint, stage string, progress int, message string) error {
	level := "info"
	if stage == StatusFailed {
		level = "error"
	}
	return s.DB.Create(&models.JobEvent{OrganizationID: organizationID, JobID: jobID, Stage: stage, Progress: progress, Level: level, Message: message, CreatedAt: now()}).Error
}

func allowedTransition(from, to string) bool {
	switch from {
	case StatusQueued:
		return to == StatusRunning || to == StatusCanceled
	case StatusRunning:
		return to == StatusWaitingProvider || to == StatusSucceeded || to == StatusFailed || to == StatusCanceled
	case StatusWaitingProvider:
		return to == StatusRunning || to == StatusSucceeded || to == StatusFailed || to == StatusCanceled
	default:
		return false
	}
}

func isTerminal(status string) bool {
	return status == StatusSucceeded || status == StatusFailed || status == StatusCanceled
}

func terminalResourceStatuses() []string {
	return []string{"completed", "failed", "canceled"}
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
