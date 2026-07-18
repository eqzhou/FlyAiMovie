package generation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/adapters"
	"github.com/eqzhou/flyaimovie/internal/services/ai"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	"github.com/eqzhou/flyaimovie/internal/services/mediafetch"
	"github.com/eqzhou/flyaimovie/internal/services/mediaref"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"gorm.io/gorm"
)

type ImageService struct {
	Store      *storage.LocalStorage
	Jobs       *jobs.Service
	References *mediaref.Resolver
	Cache      *mediacache.Service
}

func (s *ImageService) Finalize(ctx context.Context, rec *models.ImageGeneration, sourceURL string) error {
	local, err := s.Download(ctx, sourceURL, "images")
	if err != nil {
		rec.Status, rec.ErrorMsg, rec.UpdatedAt = "failed", err.Error(), response.Now()
		scopedDB(db.DB, rec.OrganizationID).Save(rec)
		s.failJob(rec.OrganizationID, rec.ID, err)
		return err
	}
	rec.LocalPath, rec.ImageURL, rec.Status = local, s.Store.PublicURL(local), "completed"
	canonicalPath, canonicalURL, contentHash, size, cacheErr := cacheGeneratedFile(s.Cache, s.Store, rec.OrganizationID, "image_generation", rec.ID, "image", rec.LocalPath, rec.ImageURL, "image/png")
	if cacheErr != nil {
		rec.Status, rec.ErrorMsg, rec.UpdatedAt = "failed", cacheErr.Error(), response.Now()
		return cacheErr
	}
	rec.LocalPath, rec.ImageURL = canonicalPath, canonicalURL
	now := response.Now()
	rec.CompletedAt, rec.UpdatedAt = &now, now
	if err := scopedDB(db.DB, rec.OrganizationID).Save(rec).Error; err != nil {
		return err
	}
	s.ApplySideEffects(rec)
	registerAsset(models.Asset{OrganizationID: rec.OrganizationID, DramaID: rec.DramaID, StoryboardID: rec.StoryboardID, Name: "生成图片", Type: "image", Category: rec.ImageType, URL: rec.ImageURL, LocalPath: rec.LocalPath, ContentHash: contentHash, FileSize: size, ImageGenID: &rec.ID})
	if s.Jobs != nil {
		result, _ := json.Marshal(map[string]string{"image_url": rec.ImageURL})
		_ = s.Jobs.SetSucceededByTargetOrganization(rec.OrganizationID, "image_generation", rec.ID, string(result))
	}
	return nil
}

func (s *ImageService) Generate(ctx context.Context, rec *models.ImageGeneration, configID *uint) error {
	cfg, err := ai.GetTaskConfigOrganization(rec.OrganizationID, "image", configID)
	if err != nil {
		return err
	}
	ts := response.Now()
	rec.Provider = cfg.Provider
	rec.Model = cfg.Model
	rec.ConfigID = &cfg.ID
	rec.Status = "processing"
	rec.UpdatedAt = ts
	if rec.CreatedAt == "" {
		rec.CreatedAt = ts
	}
	if rec.ID == 0 {
		if err := db.DB.Create(rec).Error; err != nil {
			return err
		}
	} else {
		if err := db.DB.Save(rec).Error; err != nil {
			return err
		}
	}
	if s.Jobs != nil {
		job, err := s.Jobs.CreateForTargetOrganization(rec.OrganizationID, "image.generate", "image_generation", rec.ID, cfg.Provider, &cfg.ID)
		if err != nil {
			return err
		}
		rec.JobID = &job.ID
		if err := db.DB.Model(rec).Update("job_id", job.ID).Error; err != nil {
			return err
		}
	}

	refs, err := parseImageReferenceURLs(rec.ReferenceImages)
	if err != nil {
		s.failJob(rec.OrganizationID, rec.ID, err)
		return err
	}
	if s.References != nil {
		refs, err = s.References.ResolveImages(ctx, cfg.Provider, refs)
		if err != nil {
			s.failJob(rec.OrganizationID, rec.ID, err)
			return err
		}
	}
	adapter := adapters.GetImageAdapter(cfg.Provider)
	result, err := adapter.Generate(ctx, adapters.AIConfig{
		Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model,
	}, adapters.ImageGenInput{Prompt: rec.Prompt, Size: rec.Size, ReferenceImages: refs, FrameType: rec.FrameType})
	if err != nil {
		rec.Status = "failed"
		rec.ErrorMsg = err.Error()
		rec.UpdatedAt = response.Now()
		scopedDB(db.DB, rec.OrganizationID).Save(rec)
		s.failJob(rec.OrganizationID, rec.ID, err)
		return err
	}

	var localRel string
	if result.ImageURL != "" {
		localRel, err = s.Download(ctx, result.ImageURL, "images")
	} else if result.Base64 != "" {
		localRel, err = s.saveBase64(result.Base64, result.MimeType, "images")
	} else if result.IsAsync && result.TaskID != "" {
		rec.TaskID = result.TaskID
		rec.Status = "processing"
		rec.UpdatedAt = response.Now()
		scopedDB(db.DB, rec.OrganizationID).Save(rec)
		if s.Jobs != nil && rec.JobID != nil {
			_ = s.Jobs.SetWaiting(*rec.JobID, result.TaskID)
		}
		return nil
	}
	if err != nil {
		rec.Status = "failed"
		rec.ErrorMsg = err.Error()
		scopedDB(db.DB, rec.OrganizationID).Save(rec)
		s.failJob(rec.OrganizationID, rec.ID, err)
		return err
	}
	rec.LocalPath = localRel
	rec.ImageURL = s.Store.PublicURL(localRel)
	canonicalPath, canonicalURL, contentHash, size, cacheErr := cacheGeneratedFile(s.Cache, s.Store, rec.OrganizationID, "image_generation", rec.ID, "image", rec.LocalPath, rec.ImageURL, "image/png")
	if cacheErr != nil {
		rec.Status, rec.ErrorMsg = "failed", cacheErr.Error()
		s.failJob(rec.OrganizationID, rec.ID, cacheErr)
		return cacheErr
	}
	rec.LocalPath, rec.ImageURL = canonicalPath, canonicalURL
	rec.Status = "completed"
	now := response.Now()
	rec.CompletedAt = &now
	rec.UpdatedAt = now
	scopedDB(db.DB, rec.OrganizationID).Save(rec)

	// side-effects: update character/scene/storyboard
	s.ApplySideEffects(rec)
	registerAsset(models.Asset{OrganizationID: rec.OrganizationID, DramaID: rec.DramaID, StoryboardID: rec.StoryboardID, Name: "生成图片", Type: "image", Category: rec.ImageType, URL: rec.ImageURL, LocalPath: rec.LocalPath, ContentHash: contentHash, FileSize: size, ImageGenID: &rec.ID})
	if s.Jobs != nil {
		result, _ := json.Marshal(map[string]string{"image_url": rec.ImageURL})
		_ = s.Jobs.SetSucceededByTargetOrganization(rec.OrganizationID, "image_generation", rec.ID, string(result))
	}
	return nil
}

func parseImageReferenceURLs(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}, nil
	}
	if strings.HasPrefix(raw, "data:image/") {
		return []string{raw}, nil
	}
	var values []string
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			return nil, fmt.Errorf("invalid reference images: %w", err)
		}
	} else {
		values = strings.Split(raw, ",")
	}
	clean := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			clean = append(clean, value)
		}
	}
	if len(clean) > 8 {
		return nil, fmt.Errorf("at most 8 image references are allowed")
	}
	return clean, nil
}

func (s *ImageService) failJob(organizationID, resourceID uint, err error) {
	if s.Jobs != nil && err != nil {
		_ = s.Jobs.SetFailedByTargetOrganization(organizationID, "image_generation", resourceID, err.Error())
	}
}

func (s *ImageService) ApplySideEffects(rec *models.ImageGeneration) {
	ts := response.Now()
	scope := func(q *gorm.DB) *gorm.DB {
		if rec.OrganizationID != 0 {
			return q.Where("organization_id = ?", rec.OrganizationID)
		}
		return q
	}
	if rec.CharacterID != nil {
		scope(db.DB.Model(&models.Character{})).Where("id = ?", *rec.CharacterID).Updates(map[string]any{
			"image_url": rec.ImageURL, "local_path": rec.LocalPath, "updated_at": ts,
		})
	}
	if rec.SceneID != nil {
		scope(db.DB.Model(&models.Scene{})).Where("id = ?", *rec.SceneID).Updates(map[string]any{
			"image_url": rec.ImageURL, "local_path": rec.LocalPath, "status": "completed", "updated_at": ts,
		})
	}
	if rec.StoryboardID != nil {
		updates := map[string]any{"updated_at": ts}
		switch rec.FrameType {
		case "last_frame":
			updates["last_frame_image"] = rec.ImageURL
		case "storyboard", "composed":
			updates["composed_image"] = rec.ImageURL
		default:
			updates["first_frame_image"] = rec.ImageURL
		}
		scope(db.DB.Model(&models.Storyboard{})).Where("id = ?", *rec.StoryboardID).Updates(updates)
	}
	if rec.PropID != nil {
		scope(db.DB.Model(&models.Prop{})).Where("id = ?", *rec.PropID).Updates(map[string]any{
			"image_url": rec.ImageURL, "local_path": rec.LocalPath, "updated_at": ts,
		})
	}
}

func (s *ImageService) Download(ctx context.Context, url, subdir string) (string, error) {
	return mediafetch.Download(ctx, s.Store, url, subdir, "image")
}

func (s *ImageService) saveBase64(b64, _ string, subdir string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		// try raw
		data, err = base64.RawStdEncoding.DecodeString(b64)
		if err != nil {
			return "", err
		}
	}
	info, err := mediafetch.ValidateImageUpload(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("validate generated image: %w", err)
	}
	rel, _, err := s.Store.Save(subdir, "generated"+info.Extension, bytes.NewReader(data))
	return rel, nil
}

func (s *ImageService) SaveUpload(r io.Reader, filename string) (string, error) {
	rel, _, err := s.Store.Save("uploads", filename, r)
	return rel, err
}
