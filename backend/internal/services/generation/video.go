package generation

import (
	"context"
	"encoding/json"
	"fmt"
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

type VideoService struct {
	Store      *storage.LocalStorage
	Jobs       *jobs.Service
	References *mediaref.Resolver
	Cache      *mediacache.Service
}

func (s *VideoService) Generate(ctx context.Context, rec *models.VideoGeneration, configID *uint) error {
	return s.generate(ctx, rec, configID, nil)
}

func (s *VideoService) GenerateProduction(ctx context.Context, rec *models.VideoGeneration, configID *uint, productionRunID uint) error {
	return s.generate(ctx, rec, configID, &productionRunID)
}

func (s *VideoService) generate(ctx context.Context, rec *models.VideoGeneration, configID, productionRunID *uint) error {
	cfg, err := ai.GetTaskConfigOrganization(rec.OrganizationID, "video", configID)
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
		var job *models.GenerationJob
		if productionRunID != nil {
			job, err = s.Jobs.CreateForTargetOrganizationProduction(rec.OrganizationID, "video.generate", "video_generation", rec.ID, cfg.Provider, &cfg.ID, *productionRunID)
		} else {
			job, err = s.Jobs.CreateForTargetOrganization(rec.OrganizationID, "video.generate", "video_generation", rec.ID, cfg.Provider, &cfg.ID)
		}
		if err != nil {
			return err
		}
		rec.JobID = &job.ID
		if err := db.DB.Model(rec).Update("job_id", job.ID).Error; err != nil {
			return err
		}
	}

	imageURL, firstFrameURL, lastFrameURL := rec.ImageURL, rec.FirstFrameURL, rec.LastFrameURL
	referenceImageURLs, err := parseVideoReferenceURLs(rec.ReferenceImageURLs)
	if err != nil {
		s.failCurrentGeneration(rec, err)
		return err
	}
	if s.References != nil {
		for source, target := range map[*string]*string{&rec.ImageURL: &imageURL, &rec.FirstFrameURL: &firstFrameURL, &rec.LastFrameURL: &lastFrameURL} {
			if *source == "" {
				continue
			}
			*target, err = s.References.ResolveImage(ctx, cfg.Provider, *source)
			if err != nil {
				s.failCurrentGeneration(rec, err)
				return err
			}
		}
		referenceImageURLs, err = s.References.ResolveImages(ctx, cfg.Provider, referenceImageURLs)
		if err != nil {
			s.failCurrentGeneration(rec, err)
			return err
		}
	}
	adapter := adapters.GetVideoAdapter(cfg.Provider)
	result, err := adapter.Generate(ctx, adapters.AIConfig{
		Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model,
	}, adapters.VideoGenInput{
		Prompt:             rec.Prompt,
		Duration:           rec.Duration,
		Size:               rec.Resolution,
		AspectRatio:        rec.AspectRatio,
		ReferenceMode:      rec.ReferenceMode,
		ImageURL:           imageURL,
		FirstFrameURL:      firstFrameURL,
		LastFrameURL:       lastFrameURL,
		ReferenceImageURLs: referenceImageURLs,
	})
	if err != nil {
		s.failCurrentGeneration(rec, err)
		return err
	}
	if result.IsAsync {
		rec.TaskID = result.TaskID
		rec.Status = "processing"
		rec.UpdatedAt = response.Now()
		if s.Jobs != nil && rec.JobID != nil {
			return s.Jobs.SetWaitingCurrentWith(context.Background(), *rec.JobID, result.TaskID, func(tx *gorm.DB) error { return scopedDB(tx, rec.OrganizationID).Save(rec).Error })
		}
		return scopedDB(db.DB, rec.OrganizationID).Save(rec).Error
	}
	if s.Jobs != nil && rec.JobID != nil {
		return s.finalizeAuthorized(ctx, rec, result.VideoURL, "", *rec.JobID, "")
	}
	return s.Finalize(ctx, rec, result.VideoURL)
}

func (s *VideoService) failCurrentGeneration(rec *models.VideoGeneration, generationErr error) {
	if generationErr == nil {
		return
	}
	rec.Status, rec.ErrorMsg, rec.UpdatedAt = "failed", generationErr.Error(), response.Now()
	if s.Jobs != nil && rec.JobID != nil {
		_ = s.Jobs.SetFailedCurrentWith(context.Background(), *rec.JobID, generationErr.Error(), func(tx *gorm.DB) error { return scopedDB(tx, rec.OrganizationID).Save(rec).Error })
		return
	}
	_ = scopedDB(db.DB, rec.OrganizationID).Save(rec).Error
	s.failJob(rec.OrganizationID, rec.ID, generationErr)
}

func parseVideoReferenceURLs(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}, nil
	}
	if strings.HasPrefix(raw, "data:") {
		return []string{raw}, nil
	}
	values := []string(nil)
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			return nil, fmt.Errorf("invalid reference image URLs: %w", err)
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
		return nil, fmt.Errorf("at most 8 video reference images are allowed")
	}
	return clean, nil
}

func (s *VideoService) Finalize(ctx context.Context, rec *models.VideoGeneration, videoURL string) error {
	return s.FinalizeAuthorized(ctx, rec, videoURL, "")
}

func (s *VideoService) FinalizeAuthorized(ctx context.Context, rec *models.VideoGeneration, videoURL, bearerToken string) error {
	return s.finalizeAuthorized(ctx, rec, videoURL, bearerToken, 0, "")
}

func (s *VideoService) FinalizeAuthorizedOwned(ctx context.Context, rec *models.VideoGeneration, videoURL, bearerToken string, jobID uint, owner string) error {
	return s.finalizeAuthorized(ctx, rec, videoURL, bearerToken, jobID, owner)
}

func (s *VideoService) finalizeAuthorized(ctx context.Context, rec *models.VideoGeneration, videoURL, bearerToken string, jobID uint, owner string) error {
	if err := s.requireOwned(ctx, jobID, owner); err != nil {
		return err
	}
	var rel string
	var err error
	if bearerToken == "" {
		rel, err = mediafetch.Download(ctx, s.Store, videoURL, "videos", "video")
	} else {
		rel, err = mediafetch.DownloadAuthorized(ctx, s.Store, videoURL, "videos", "video", bearerToken)
	}
	if err != nil {
		rec.Status = "failed"
		rec.ErrorMsg = err.Error()
		if jobID > 0 && owner != "" {
			_ = s.Jobs.SetFailedOwnedWith(context.Background(), jobID, owner, err.Error(), func(tx *gorm.DB) error { return scopedDB(tx, rec.OrganizationID).Save(rec).Error })
		} else if jobID > 0 {
			_ = s.Jobs.SetFailedCurrentWith(context.Background(), jobID, err.Error(), func(tx *gorm.DB) error { return scopedDB(tx, rec.OrganizationID).Save(rec).Error })
		} else {
			scopedDB(db.DB, rec.OrganizationID).Save(rec)
			s.failOwnedJob(jobID, owner, rec.OrganizationID, rec.ID, err)
		}
		return err
	}
	if err := s.requireOwned(ctx, jobID, owner); err != nil {
		return err
	}
	rec.LocalPath = rel
	rec.VideoURL = s.Store.PublicURL(rel)
	canonicalPath, canonicalURL, contentHash, size, cacheErr := cacheGeneratedFile(s.Cache, s.Store, rec.OrganizationID, "video_generation", rec.ID, "video", rec.LocalPath, rec.VideoURL, "video/mp4")
	if cacheErr != nil {
		rec.Status, rec.ErrorMsg = "failed", cacheErr.Error()
		if jobID > 0 && owner != "" {
			_ = s.Jobs.SetFailedOwnedWith(context.Background(), jobID, owner, cacheErr.Error(), func(tx *gorm.DB) error { return scopedDB(tx, rec.OrganizationID).Save(rec).Error })
		} else if jobID > 0 {
			_ = s.Jobs.SetFailedCurrentWith(context.Background(), jobID, cacheErr.Error(), func(tx *gorm.DB) error { return scopedDB(tx, rec.OrganizationID).Save(rec).Error })
		} else {
			s.failOwnedJob(jobID, owner, rec.OrganizationID, rec.ID, cacheErr)
		}
		return cacheErr
	}
	if err := s.requireOwned(ctx, jobID, owner); err != nil {
		return err
	}
	rec.LocalPath, rec.VideoURL = canonicalPath, canonicalURL
	rec.Status = "completed"
	now := response.Now()
	rec.CompletedAt = &now
	rec.UpdatedAt = now
	asset := models.Asset{OrganizationID: rec.OrganizationID, DramaID: rec.DramaID, StoryboardID: rec.StoryboardID, Name: "生成视频", Type: "video", Category: "video", URL: rec.VideoURL, LocalPath: rec.LocalPath, ContentHash: contentHash, FileSize: size, VideoGenID: &rec.ID}
	result, _ := json.Marshal(map[string]string{"video_url": rec.VideoURL})
	commit := func(database *gorm.DB) error {
		if err := scopedDB(database, rec.OrganizationID).Save(rec).Error; err != nil {
			return err
		}
		if rec.StoryboardID != nil {
			query := database.Model(&models.Storyboard{}).Where("id = ?", *rec.StoryboardID)
			if rec.OrganizationID != 0 {
				query = query.Where("organization_id = ?", rec.OrganizationID)
			}
			if err := query.Updates(map[string]any{"video_url": rec.VideoURL, "status": "video_ready", "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return registerAssetWithDB(database, asset)
	}
	if jobID > 0 {
		if owner != "" {
			return s.Jobs.SetSucceededOwnedWith(ctx, jobID, owner, string(result), commit)
		}
		return s.Jobs.SetSucceededCurrentWith(context.Background(), jobID, string(result), commit)
	}
	if err := db.DB.Transaction(commit); err != nil {
		return err
	}
	if s.Jobs != nil {
		_ = s.Jobs.SetSucceededByTargetOrganization(rec.OrganizationID, "video_generation", rec.ID, string(result))
	}
	return nil
}

func (s *VideoService) requireOwned(ctx context.Context, jobID uint, owner string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if jobID > 0 {
		if s.Jobs == nil {
			return jobs.ErrTerminalJob
		}
		if owner != "" && !s.Jobs.IsOwned(jobID, owner) {
			return jobs.ErrTerminalJob
		}
		if owner == "" {
			job, err := s.Jobs.Get(jobID)
			if err != nil || job.Status != jobs.StatusRunning || job.LeaseOwner != "" {
				return jobs.ErrTerminalJob
			}
		}
	}
	return nil
}

func (s *VideoService) failOwnedJob(jobID uint, owner string, organizationID, resourceID uint, err error) {
	if s.Jobs == nil || err == nil {
		return
	}
	if jobID > 0 {
		_ = s.Jobs.SetFailedOwned(jobID, owner, err.Error())
		return
	}
	s.failJob(organizationID, resourceID, err)
}

func (s *VideoService) failJob(organizationID, resourceID uint, err error) {
	if s.Jobs != nil && err != nil {
		_ = s.Jobs.SetFailedByTargetOrganization(organizationID, "video_generation", resourceID, err.Error())
	}
}
