package generation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/adapters"
	"github.com/eqzhou/flyaimovie/internal/services/ai"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/services/mediafetch"
	"github.com/eqzhou/flyaimovie/internal/services/mediaref"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ImageService struct {
	Store      *storage.LocalStorage
	Jobs       *jobs.Service
	References *mediaref.Resolver
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
	now := response.Now()
	rec.CompletedAt, rec.UpdatedAt = &now, now
	if err := scopedDB(db.DB, rec.OrganizationID).Save(rec).Error; err != nil {
		return err
	}
	s.ApplySideEffects(rec)
	registerAsset(models.Asset{OrganizationID: rec.OrganizationID, DramaID: rec.DramaID, StoryboardID: rec.StoryboardID, Name: "生成图片", Type: "image", Category: rec.ImageType, URL: rec.ImageURL, LocalPath: rec.LocalPath, ImageGenID: &rec.ID})
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

	refs := []string{}
	if rec.ReferenceImages != "" {
		// comma or JSON-ish simple split
		for _, part := range strings.Split(rec.ReferenceImages, ",") {
			part = strings.TrimSpace(part)
			part = strings.Trim(part, `"'[] `)
			if part != "" {
				refs = append(refs, part)
			}
		}
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
	rec.Status = "completed"
	now := response.Now()
	rec.CompletedAt = &now
	rec.UpdatedAt = now
	scopedDB(db.DB, rec.OrganizationID).Save(rec)

	// side-effects: update character/scene/storyboard
	s.ApplySideEffects(rec)
	registerAsset(models.Asset{OrganizationID: rec.OrganizationID, DramaID: rec.DramaID, StoryboardID: rec.StoryboardID, Name: "生成图片", Type: "image", Category: rec.ImageType, URL: rec.ImageURL, LocalPath: rec.LocalPath, ImageGenID: &rec.ID})
	if s.Jobs != nil {
		result, _ := json.Marshal(map[string]string{"image_url": rec.ImageURL})
		_ = s.Jobs.SetSucceededByTargetOrganization(rec.OrganizationID, "image_generation", rec.ID, string(result))
	}
	return nil
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

func (s *ImageService) saveBase64(b64, mime, subdir string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		// try raw
		data, err = base64.RawStdEncoding.DecodeString(b64)
		if err != nil {
			return "", err
		}
	}
	ext := ".png"
	if mime == "image/jpeg" {
		ext = ".jpg"
	}
	name := fmt.Sprintf("%s_%s%s", time.Now().Format("20060102150405"), uuid.NewString()[:8], ext)
	absDir := filepath.Join(s.Store.Root, subdir)
	_ = os.MkdirAll(absDir, 0o755)
	rel := filepath.ToSlash(filepath.Join(subdir, name))
	abs := filepath.Join(s.Store.Root, filepath.FromSlash(rel))
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return "", err
	}
	return rel, nil
}

func (s *ImageService) SaveUpload(r io.Reader, filename string) (string, error) {
	rel, _, err := s.Store.Save("uploads", filename, r)
	return rel, err
}
