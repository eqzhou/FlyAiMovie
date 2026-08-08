package production

import (
	"fmt"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"gorm.io/gorm"
)

func (s *Service) release(id uint, owner string) error {
	result := s.DB.Model(&models.ProductionRun{}).Where("id = ? AND status = ? AND lease_owner = ?", id, StatusQueued, owner).Updates(map[string]any{"lease_owner": "", "lease_expires_at": nil, "updated_at": response.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTerminalRun
	}
	return nil
}

func (s *Service) deferRun(run *models.ProductionRun, message string) error {
	now := time.Now().UTC()
	availableAt := now.Add(2 * time.Second).Format(time.RFC3339)
	result := s.DB.Model(&models.ProductionRun{}).Where("id = ? AND status = ? AND lease_owner = ?", run.ID, StatusQueued, run.LeaseOwner).Updates(map[string]any{"status_message": message, "available_at": availableAt, "updated_at": now.Format(time.RFC3339)})
	return result.Error
}

func (s *Service) complete(run *models.ProductionRun) error {
	now := response.Now()
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.ProductionRun{}).Where("id = ? AND status = ? AND lease_owner = ?", run.ID, StatusQueued, run.LeaseOwner).Updates(map[string]any{"status": StatusSucceeded, "stage": StageCompleted, "progress": 100, "status_message": "制作完成", "completed_at": now, "lease_owner": "", "lease_expires_at": nil, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTerminalRun
		}
		// Keep episode lifecycle aligned with production success, including remakes that
		// already had a video_url and skipped a fresh merge job.
		episode := tx.Model(&models.Episode{}).Where("organization_id = ? AND id = ?", run.OrganizationID, run.EpisodeID).Updates(map[string]any{"status": "completed", "updated_at": now})
		if episode.Error != nil {
			return episode.Error
		}
		if episode.RowsAffected != 1 {
			return fmt.Errorf("production episode not found")
		}
		return nil
	})
	if err != nil {
		return err
	}
	run.Status = StatusSucceeded
	run.Stage = StageCompleted
	run.Progress = 100
	return nil
}

func (s *Service) failRun(run *models.ProductionRun, reason error) error {
	now := response.Now()
	result := s.DB.Model(&models.ProductionRun{}).Where("id = ? AND status = ? AND lease_owner = ?", run.ID, StatusQueued, run.LeaseOwner).Updates(map[string]any{"status": StatusFailed, "status_message": "制作失败", "last_error": reason.Error(), "completed_at": now, "lease_owner": "", "lease_expires_at": nil, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return nil
	}
	_ = s.cancelActiveChildren(run.OrganizationID, run.ID)
	var episode models.Episode
	if err := s.DB.Where("organization_id = ? AND id = ?", run.OrganizationID, run.EpisodeID).First(&episode).Error; err == nil {
		// Remakes move completed episodes back to processing. If they already have a
		// final cut, restore completed on failure; otherwise mark failed.
		status := "failed"
		if strings.TrimSpace(episode.VideoURL) != "" {
			status = "completed"
		}
		_ = s.DB.Model(&episode).Where("status = ?", "processing").Updates(map[string]any{"status": status, "updated_at": now}).Error
	}
	return nil
}

func (s *Service) cancelActiveChildren(organizationID, runID uint) error {
	now := response.Now()
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var active []models.GenerationJob
		if err := tx.Where("organization_id = ? AND production_run_id = ? AND status NOT IN ?", organizationID, runID, []string{jobs.StatusSucceeded, jobs.StatusFailed, jobs.StatusCanceled}).Find(&active).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.GenerationJob{}).Where("organization_id = ? AND production_run_id = ? AND status NOT IN ?", organizationID, runID, []string{jobs.StatusSucceeded, jobs.StatusFailed, jobs.StatusCanceled}).Updates(map[string]any{"status": jobs.StatusCanceled, "cancel_requested_at": now, "completed_at": now, "lease_owner": "", "lease_expires_at": nil, "updated_at": now}).Error; err != nil {
			return err
		}
		imageIDs := make([]uint, 0)
		videoIDs := make([]uint, 0)
		for _, job := range active {
			switch job.TargetType {
			case "image_generation":
				imageIDs = append(imageIDs, job.TargetID)
			case "video_generation":
				videoIDs = append(videoIDs, job.TargetID)
			}
		}
		terminal := []string{"completed", "failed", "canceled"}
		if len(imageIDs) > 0 {
			if err := tx.Model(&models.ImageGeneration{}).Where("organization_id = ? AND id IN ? AND status NOT IN ?", organizationID, imageIDs, terminal).Updates(map[string]any{"status": "canceled", "updated_at": now}).Error; err != nil {
				return err
			}
		}
		if len(videoIDs) > 0 {
			if err := tx.Model(&models.VideoGeneration{}).Where("organization_id = ? AND id IN ? AND status NOT IN ?", organizationID, videoIDs, terminal).Updates(map[string]any{"status": "canceled", "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
