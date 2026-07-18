package mediacleanup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"gorm.io/gorm"
)

type Result struct {
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

type Service struct {
	DB    *gorm.DB
	Store *storage.LocalStorage
}

func New(database *gorm.DB, store *storage.LocalStorage) *Service {
	return &Service{DB: database, Store: store}
}

func (s *Service) Queue(organizationID uint, paths []string) error {
	return s.QueueReason(organizationID, paths, "organization_deleted")
}

func (s *Service) QueueReason(organizationID uint, paths []string, reason string) error {
	if s == nil || s.DB == nil || s.Store == nil {
		return fmt.Errorf("media cleanup service is not configured")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 100 {
		return fmt.Errorf("cleanup reason is required")
	}
	now := response.Now()
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		relative, err := s.normalize(path)
		if err != nil {
			return err
		}
		if _, ok := seen[relative]; ok {
			continue
		}
		seen[relative] = struct{}{}
		digest := sha256.Sum256([]byte(relative))
		hash := hex.EncodeToString(digest[:])
		var existing models.MediaDeletionTask
		err = s.DB.Where("organization_id = ? AND path_hash = ?", organizationID, hash).First(&existing).Error
		if err == nil {
			if existing.Status == "completed" {
				continue
			}
			if err := s.DB.Model(&existing).Updates(map[string]any{"status": "pending", "reason": reason, "available_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		task := models.MediaDeletionTask{OrganizationID: organizationID, PathHash: hash, LocalPath: relative, Reason: reason, Status: "pending", AvailableAt: now, CreatedAt: now, UpdatedAt: now}
		if err := s.DB.Create(&task).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ProcessOrganization(organizationID uint, limit int) (Result, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	query := s.DB.Where("status IN ? AND attempts < ? AND available_at <= ?", []string{"pending", "failed"}, 10, response.Now())
	if organizationID != 0 {
		query = query.Where("organization_id = ?", organizationID)
	}
	var tasks []models.MediaDeletionTask
	if err := query.Order("id asc").Limit(limit).Find(&tasks).Error; err != nil {
		return Result{}, err
	}
	result := Result{}
	for index := range tasks {
		if err := s.process(&tasks[index]); err != nil {
			result.Failed++
		} else {
			result.Completed++
		}
	}
	return result, nil
}

func (s *Service) process(task *models.MediaDeletionTask) error {
	absolute := filepath.Join(s.Store.Root, filepath.FromSlash(task.LocalPath))
	err := os.Remove(absolute)
	now := response.Now()
	if err == nil || os.IsNotExist(err) {
		return s.DB.Model(task).Updates(map[string]any{"status": "completed", "attempts": task.Attempts + 1, "last_error": "", "completed_at": now, "updated_at": now}).Error
	}
	next := time.Now().UTC().Add(time.Duration(task.Attempts+1) * time.Minute).Format(time.RFC3339)
	if dbErr := s.DB.Model(task).Updates(map[string]any{"status": "failed", "attempts": task.Attempts + 1, "last_error": err.Error(), "available_at": next, "updated_at": now}).Error; dbErr != nil {
		return dbErr
	}
	return err
}

func (s *Service) normalize(path string) (string, error) {
	root, err := filepath.Abs(s.Store.Root)
	if err != nil {
		return "", err
	}
	if resolvedRoot, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolvedRoot
	}
	value := strings.TrimSpace(path)
	if value == "" {
		return "", fmt.Errorf("empty cleanup path")
	}
	if strings.HasPrefix(filepath.ToSlash(value), "/static/") {
		value = strings.TrimPrefix(filepath.ToSlash(value), "/static/")
	} else if filepath.IsAbs(value) {
		if resolvedValue, resolveErr := filepath.EvalSymlinks(value); resolveErr == nil {
			value = resolvedValue
		}
		value, err = filepath.Rel(root, value)
		if err != nil {
			return "", err
		}
	} else {
		value = strings.TrimPrefix(value, "/")
		value = strings.TrimPrefix(value, "static/")
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("cleanup path escapes storage root")
	}
	return filepath.ToSlash(clean), nil
}
