package jobs

import (
	"strconv"
	"time"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
)

func (s *Service) cacheResult(job models.GenerationJob, target string, extra map[string]any) error {
	if target != StatusSucceeded {
		return nil
	}
	result, ok := extra["result_json"].(string)
	if !ok || result == "" {
		return nil
	}
	_, _, err := mediacache.New(s.DB, nil).PutValue(job.OrganizationID, "job_result", strconv.FormatUint(uint64(job.ID), 10), "json", result, 7*24*time.Hour)
	return err
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
