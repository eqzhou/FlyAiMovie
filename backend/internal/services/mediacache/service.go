package mediacache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/mediacleanup"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	StatusActive   = "active"
	StatusOrphaned = "orphaned"
	orphanTTL      = 24 * time.Hour
)

type PutInput struct {
	OrganizationID uint
	Namespace      string
	Key            string
	ContentHash    string
	Kind           string
	LocalPath      string
	PublicURL      string
	MimeType       string
	Payload        string
	Size           int64
	ExpiresAt      *string
}

type PurgeResult struct {
	ReleasedReferences int `json:"released_references"`
	DeletedObjects     int `json:"deleted_objects"`
	Queued             int `json:"queued"`
}

type Stats struct {
	Objects    int64 `json:"objects"`
	References int64 `json:"references"`
	Bytes      int64 `json:"bytes"`
	Orphaned   int64 `json:"orphaned"`
}

func (s *Service) PurgeAllExpired(limitPerOrganization int) (PurgeResult, error) {
	organizationIDs := make(map[uint]struct{})
	for _, model := range []any{&models.MediaCacheReference{}, &models.MediaCacheObject{}} {
		var ids []uint
		if err := s.DB.Model(model).Distinct("organization_id").Pluck("organization_id", &ids).Error; err != nil {
			return PurgeResult{}, err
		}
		for _, id := range ids {
			organizationIDs[id] = struct{}{}
		}
	}
	result := PurgeResult{}
	for organizationID := range organizationIDs {
		current, err := s.PurgeExpired(organizationID, limitPerOrganization)
		if err != nil {
			return result, err
		}
		result.ReleasedReferences += current.ReleasedReferences
		result.DeletedObjects += current.DeletedObjects
		result.Queued += current.Queued
	}
	return result, nil
}

type Service struct {
	DB    *gorm.DB
	Store *storage.LocalStorage
}

func New(database *gorm.DB, store *storage.LocalStorage) *Service {
	return &Service{DB: database, Store: store}
}

func (s *Service) Put(input PutInput) (*models.MediaCacheObject, bool, error) {
	if err := validatePut(input); err != nil {
		return nil, false, err
	}
	var object models.MediaCacheObject
	reused := false
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var existingReference models.MediaCacheReference
		referenceErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("organization_id = ? AND namespace = ? AND cache_key = ?", input.OrganizationID, input.Namespace, input.Key).
			First(&existingReference).Error
		if referenceErr != nil && !errors.Is(referenceErr, gorm.ErrRecordNotFound) {
			return referenceErr
		}

		objectErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("organization_id = ? AND content_hash = ? AND kind = ?", input.OrganizationID, input.ContentHash, input.Kind).
			First(&object).Error
		now := response.Now()
		if errors.Is(objectErr, gorm.ErrRecordNotFound) {
			object = models.MediaCacheObject{OrganizationID: input.OrganizationID, ContentHash: input.ContentHash, Kind: input.Kind,
				LocalPath: normalizePath(input.LocalPath), PublicURL: input.PublicURL, MimeType: input.MimeType, Payload: input.Payload, Size: input.Size,
				Status: StatusActive, LastAccessedAt: now, CreatedAt: now, UpdatedAt: now}
			created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&object)
			if created.Error != nil {
				return created.Error
			}
			if created.RowsAffected == 0 {
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND content_hash = ? AND kind = ?", input.OrganizationID, input.ContentHash, input.Kind).First(&object).Error; err != nil {
					return err
				}
				reused = true
			}
		} else if objectErr != nil {
			return objectErr
		} else {
			reused = true
			if input.LocalPath != "" && (object.LocalPath == "" || (s.Store != nil && !cacheFileExists(s.Store, object.LocalPath))) {
				updates := map[string]any{"local_path": normalizePath(input.LocalPath), "public_url": input.PublicURL, "mime_type": input.MimeType,
					"payload": input.Payload, "size": input.Size, "kind": input.Kind, "updated_at": now}
				if err := tx.Model(&object).Updates(updates).Error; err != nil {
					return err
				}
				object.LocalPath, object.PublicURL, object.MimeType, object.Payload, object.Size, object.Kind = normalizePath(input.LocalPath), input.PublicURL, input.MimeType, input.Payload, input.Size, input.Kind
				reused = false
			}
		}

		if referenceErr == nil && existingReference.ObjectID == object.ID {
			if err := tx.Model(&existingReference).Updates(map[string]any{"expires_at": input.ExpiresAt, "updated_at": now}).Error; err != nil {
				return err
			}
			return tx.Model(&object).Updates(map[string]any{"last_accessed_at": now, "updated_at": now}).Error
		}
		if referenceErr == nil {
			if existingReference.ObjectID != object.ID && input.LocalPath != "" {
				_ = tx.Model(&models.MediaCacheObject{}).Where("id = ? AND local_path = ?", existingReference.ObjectID, normalizePath(input.LocalPath)).Updates(map[string]any{"local_path": "", "public_url": "", "updated_at": now}).Error
			}
			if err := decrementObject(tx, existingReference.ObjectID, now); err != nil {
				return err
			}
			if err := tx.Model(&existingReference).Updates(map[string]any{"object_id": object.ID, "expires_at": input.ExpiresAt, "updated_at": now}).Error; err != nil {
				return err
			}
		} else {
			reference := models.MediaCacheReference{OrganizationID: input.OrganizationID, Namespace: input.Namespace, CacheKey: input.Key,
				ObjectID: object.ID, ExpiresAt: input.ExpiresAt, CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&reference).Error; err != nil {
				return err
			}
		}
		updates := map[string]any{"reference_count": gorm.Expr("reference_count + 1"), "status": StatusActive, "expires_at": nil, "last_accessed_at": now, "updated_at": now}
		if err := tx.Model(&models.MediaCacheObject{}).Where("id = ?", object.ID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&object, object.ID).Error
	})
	if err != nil {
		return nil, false, err
	}
	return &object, reused, nil
}

func (s *Service) Resolve(organizationID uint, namespace, key string) (*models.MediaCacheObject, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("namespace and key are required")
	}
	var reference models.MediaCacheReference
	if err := s.DB.Where("organization_id = ? AND namespace = ? AND cache_key = ?", organizationID, namespace, key).First(&reference).Error; err != nil {
		return nil, err
	}
	if reference.ExpiresAt != nil && *reference.ExpiresAt <= response.Now() {
		return nil, gorm.ErrRecordNotFound
	}
	var object models.MediaCacheObject
	if err := s.DB.Where("id = ? AND organization_id = ? AND status = ?", reference.ObjectID, organizationID, StatusActive).First(&object).Error; err != nil {
		return nil, err
	}
	now := response.Now()
	_ = s.DB.Model(&object).Updates(map[string]any{"last_accessed_at": now, "updated_at": now}).Error
	object.LastAccessedAt, object.UpdatedAt = now, now
	return &object, nil
}

func (s *Service) PutValue(organizationID uint, namespace, key, kind, value string, ttl time.Duration) (*models.MediaCacheObject, bool, error) {
	if len(value) > 10<<20 {
		return nil, false, fmt.Errorf("cache value exceeds 10 MB")
	}
	digest := sha256.Sum256([]byte(value))
	var expiresAt *string
	if ttl > 0 {
		expires := time.Now().UTC().Add(ttl).Format(time.RFC3339)
		expiresAt = &expires
	}
	return s.Put(PutInput{OrganizationID: organizationID, Namespace: namespace, Key: key, ContentHash: hex.EncodeToString(digest[:]),
		Kind: kind, MimeType: "application/json", Payload: value, Size: int64(len(value)), ExpiresAt: expiresAt})
}

func (s *Service) ResolveValue(organizationID uint, namespace, key string) (string, error) {
	object, err := s.Resolve(organizationID, namespace, key)
	if err != nil {
		return "", err
	}
	if object.Payload == "" {
		return "", gorm.ErrRecordNotFound
	}
	return object.Payload, nil
}

func (s *Service) Release(organizationID uint, namespace, key string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var reference models.MediaCacheReference
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND namespace = ? AND cache_key = ?", organizationID, namespace, key).First(&reference).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := tx.Delete(&reference).Error; err != nil {
			return err
		}
		return decrementObject(tx, reference.ObjectID, response.Now())
	})
}

func (s *Service) PurgeExpired(organizationID uint, limit int) (PurgeResult, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	result := PurgeResult{}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		now := response.Now()
		var references []models.MediaCacheReference
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND expires_at IS NOT NULL AND expires_at <= ?", organizationID, now).Limit(limit).Find(&references).Error; err != nil {
			return err
		}
		for index := range references {
			if err := tx.Delete(&references[index]).Error; err != nil {
				return err
			}
			if err := decrementObject(tx, references[index].ObjectID, now); err != nil {
				return err
			}
			result.ReleasedReferences++
		}
		var objects []models.MediaCacheObject
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND reference_count = 0 AND status = ? AND expires_at IS NOT NULL AND expires_at <= ?", organizationID, StatusOrphaned, now).Limit(limit).Find(&objects).Error; err != nil {
			return err
		}
		paths := make([]string, 0, len(objects))
		seen := make(map[string]struct{}, len(objects))
		for _, object := range objects {
			if object.LocalPath != "" {
				if _, ok := seen[object.LocalPath]; !ok {
					seen[object.LocalPath] = struct{}{}
					paths = append(paths, object.LocalPath)
				}
			}
		}
		if len(paths) > 0 {
			if s.Store == nil {
				return fmt.Errorf("storage is required to purge cached media")
			}
			if err := mediacleanup.New(tx, s.Store).QueueReason(organizationID, paths, "cache_expired"); err != nil {
				return err
			}
			result.Queued = len(paths)
		}
		if len(objects) > 0 {
			ids := make([]uint, 0, len(objects))
			for _, object := range objects {
				ids = append(ids, object.ID)
			}
			if err := tx.Where("id IN ?", ids).Delete(&models.MediaCacheObject{}).Error; err != nil {
				return err
			}
			result.DeletedObjects = len(ids)
		}
		return nil
	})
	return result, err
}

func (s *Service) Stats(organizationID uint) (Stats, error) {
	var stats Stats
	if err := s.DB.Model(&models.MediaCacheObject{}).Where("organization_id = ?", organizationID).Count(&stats.Objects).Error; err != nil {
		return stats, err
	}
	if err := s.DB.Model(&models.MediaCacheReference{}).Where("organization_id = ?", organizationID).Count(&stats.References).Error; err != nil {
		return stats, err
	}
	if err := s.DB.Model(&models.MediaCacheObject{}).Where("organization_id = ?", organizationID).Select("COALESCE(SUM(size), 0)").Scan(&stats.Bytes).Error; err != nil {
		return stats, err
	}
	if err := s.DB.Model(&models.MediaCacheObject{}).Where("organization_id = ? AND status = ?", organizationID, StatusOrphaned).Count(&stats.Orphaned).Error; err != nil {
		return stats, err
	}
	return stats, nil
}

func decrementObject(tx *gorm.DB, objectID uint, now string) error {
	if err := tx.Model(&models.MediaCacheObject{}).Where("id = ?", objectID).Update("reference_count", gorm.Expr("CASE WHEN reference_count > 0 THEN reference_count - 1 ELSE 0 END")).Error; err != nil {
		return err
	}
	var object models.MediaCacheObject
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&object, objectID).Error; err != nil {
		return err
	}
	if object.ReferenceCount > 0 {
		return nil
	}
	expires := time.Now().UTC().Add(orphanTTL).Format(time.RFC3339)
	return tx.Model(&object).Updates(map[string]any{"status": StatusOrphaned, "expires_at": expires, "updated_at": now}).Error
}

func validatePut(input PutInput) error {
	if strings.TrimSpace(input.Namespace) == "" || strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.ContentHash) == "" || strings.TrimSpace(input.Kind) == "" {
		return fmt.Errorf("namespace, key, content hash and kind are required")
	}
	if len(input.Namespace) > 64 || len(input.Key) > 255 || len(input.ContentHash) > 128 || len(input.Kind) > 64 || len(input.Payload) > 10<<20 || input.Size < 0 {
		return fmt.Errorf("cache input exceeds allowed limits")
	}
	if input.LocalPath != "" && normalizePath(input.LocalPath) == "" {
		return fmt.Errorf("local path escapes storage root")
	}
	return nil
}

func normalizePath(value string) string {
	value = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(value)), "/")
	value = strings.TrimPrefix(value, "static/")
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." || clean == "" || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(clean)
}

func HashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func cacheFileExists(store *storage.LocalStorage, localPath string) bool {
	_, err := store.Resolve(localPath)
	return err == nil
}
