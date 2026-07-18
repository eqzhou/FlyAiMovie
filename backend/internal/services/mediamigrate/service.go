package mediamigrate

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	"github.com/eqzhou/flyaimovie/internal/services/mediafetch"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"gorm.io/gorm"
)

type Candidate struct {
	OrganizationID uint
	TargetType     string
	TargetID       uint
	SourceURL      string
	Kind           string
}

type Result struct {
	Found     int `json:"found"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type Service struct {
	DB    *gorm.DB
	Store *storage.LocalStorage
}

func (s *Service) Scan(limit int) ([]Candidate, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	items := make([]Candidate, 0)
	var assets []models.Asset
	if err := s.DB.Where("deleted_at IS NULL AND local_path = ''").Limit(limit).Find(&assets).Error; err != nil {
		return nil, err
	}
	for _, item := range assets {
		if IsRemote(item.URL) {
			items = append(items, Candidate{item.OrganizationID, "asset", item.ID, item.URL, mediaKind(item.Type)})
		}
	}
	var images []models.ImageGeneration
	if err := s.DB.Where("local_path = ''").Limit(limit).Find(&images).Error; err != nil {
		return nil, err
	}
	for _, item := range images {
		if IsRemote(item.ImageURL) {
			items = append(items, Candidate{item.OrganizationID, "image_generation", item.ID, item.ImageURL, "image"})
		}
	}
	var videos []models.VideoGeneration
	if err := s.DB.Where("deleted_at IS NULL AND local_path = ''").Limit(limit).Find(&videos).Error; err != nil {
		return nil, err
	}
	for _, item := range videos {
		if IsRemote(item.VideoURL) {
			items = append(items, Candidate{item.OrganizationID, "video_generation", item.ID, item.VideoURL, "video"})
		}
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Service) Run(ctx context.Context, candidates []Candidate) Result {
	result := Result{Found: len(candidates)}
	for _, candidate := range candidates {
		if err := s.migrateOne(ctx, candidate); err != nil {
			result.Failed++
		} else {
			result.Succeeded++
		}
	}
	return result
}

func (s *Service) migrateOne(ctx context.Context, candidate Candidate) error {
	now := response.Now()
	record := models.MediaMigration{OrganizationID: candidate.OrganizationID, TargetType: candidate.TargetType, TargetID: candidate.TargetID,
		SourceURL: candidate.SourceURL, Status: "running", Attempts: 1, CreatedAt: now, UpdatedAt: now}
	var existing models.MediaMigration
	if err := s.DB.Where("organization_id = ? AND target_type = ? AND target_id = ?", candidate.OrganizationID, candidate.TargetType, candidate.TargetID).First(&existing).Error; err == nil {
		if existing.Status == "completed" {
			return nil
		}
		record.ID, record.CreatedAt, record.Attempts = existing.ID, existing.CreatedAt, existing.Attempts+1
	}
	if err := s.DB.Save(&record).Error; err != nil {
		return err
	}
	rel, err := mediafetch.Download(ctx, s.Store, candidate.SourceURL, migrationSubdir(candidate.Kind), candidate.Kind)
	if err != nil {
		s.DB.Model(&record).Updates(map[string]any{"status": "failed", "last_error": err.Error(), "updated_at": response.Now()})
		return err
	}
	publicURL := s.Store.PublicURL(rel)
	hash, size, err := mediacache.HashFile(s.Store.Abs(rel))
	if err != nil {
		_ = os.Remove(s.Store.Abs(rel))
		s.DB.Model(&record).Updates(map[string]any{"status": "failed", "last_error": err.Error(), "updated_at": response.Now()})
		return err
	}
	canonicalRel := rel
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		cacheObject, _, cacheErr := mediacache.New(tx, s.Store).Put(mediacache.PutInput{OrganizationID: candidate.OrganizationID,
			Namespace: candidate.TargetType, Key: strconv.FormatUint(uint64(candidate.TargetID), 10), ContentHash: hash, Kind: candidate.Kind,
			LocalPath: rel, PublicURL: publicURL, Size: size})
		if cacheErr != nil {
			return cacheErr
		}
		canonicalRel, publicURL = cacheObject.LocalPath, cacheObject.PublicURL
		var update *gorm.DB
		switch candidate.TargetType {
		case "asset":
			update = tx.Model(&models.Asset{}).Where("organization_id = ? AND id = ?", candidate.OrganizationID, candidate.TargetID).Updates(map[string]any{"url": publicURL, "local_path": canonicalRel, "content_hash": hash, "file_size": size, "updated_at": response.Now()})
		case "image_generation":
			update = tx.Model(&models.ImageGeneration{}).Where("organization_id = ? AND id = ?", candidate.OrganizationID, candidate.TargetID).Updates(map[string]any{"image_url": publicURL, "local_path": canonicalRel, "updated_at": response.Now()})
		case "video_generation":
			update = tx.Model(&models.VideoGeneration{}).Where("organization_id = ? AND id = ?", candidate.OrganizationID, candidate.TargetID).Updates(map[string]any{"video_url": publicURL, "local_path": canonicalRel, "updated_at": response.Now()})
		}
		if update == nil {
			return gorm.ErrInvalidData
		}
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		if err := replaceMediaReferences(tx, candidate, publicURL); err != nil {
			return err
		}
		completed := response.Now()
		return tx.Model(&record).Updates(map[string]any{"status": "completed", "local_path": canonicalRel, "last_error": "", "updated_at": completed, "completed_at": completed}).Error
	})
	if err != nil {
		_ = os.Remove(s.Store.Abs(rel))
		s.DB.Model(&record).Updates(map[string]any{"status": "failed", "last_error": err.Error(), "updated_at": response.Now()})
	} else if canonicalRel != rel {
		_ = os.Remove(s.Store.Abs(rel))
	}
	return err
}

func replaceMediaReferences(tx *gorm.DB, candidate Candidate, publicURL string) error {
	type reference struct {
		model  any
		fields []string
	}
	var references []reference
	switch candidate.Kind {
	case "image":
		references = []reference{
			{&models.Asset{}, []string{"url", "thumbnail_url"}},
			{&models.Character{}, []string{"image_url"}},
			{&models.CharacterTemplate{}, []string{"image_url"}},
			{&models.Scene{}, []string{"image_url"}},
			{&models.Prop{}, []string{"image_url"}},
			{&models.Storyboard{}, []string{"composed_image", "first_frame_image", "last_frame_image"}},
			{&models.GridHistory{}, []string{"image_url"}},
			{&models.Drama{}, []string{"thumbnail"}},
			{&models.Episode{}, []string{"thumbnail"}},
		}
	case "audio":
		references = []reference{
			{&models.Asset{}, []string{"url"}},
			{&models.Character{}, []string{"voice_sample_url"}},
			{&models.Storyboard{}, []string{"tts_audio_url"}},
		}
	default:
		references = []reference{
			{&models.Asset{}, []string{"url", "thumbnail_url"}},
			{&models.Storyboard{}, []string{"video_url", "composed_video_url"}},
			{&models.Episode{}, []string{"video_url"}},
			{&models.VideoMerge{}, []string{"merged_url"}},
		}
	}
	for _, item := range references {
		for _, field := range item.fields {
			updates := map[string]any{field: publicURL}
			if tx.Migrator().HasColumn(item.model, "updated_at") {
				updates["updated_at"] = response.Now()
			}
			if err := tx.Model(item.model).Where("organization_id = ? AND "+field+" = ?", candidate.OrganizationID, candidate.SourceURL).Updates(updates).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func IsRemote(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != ""
}

func mediaKind(value string) string {
	if strings.EqualFold(value, "image") {
		return "image"
	}
	if strings.EqualFold(value, "audio") {
		return "audio"
	}
	return "video"
}
func migrationSubdir(kind string) string {
	if kind == "image" {
		return "images"
	}
	if kind == "audio" {
		return "audio"
	}
	return "videos"
}
