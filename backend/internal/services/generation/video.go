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
	"github.com/eqzhou/flyaimovie/internal/services/mediafetch"
	"github.com/eqzhou/flyaimovie/internal/services/mediaref"
	"github.com/eqzhou/flyaimovie/internal/storage"
)

type VideoService struct {
	Store      *storage.LocalStorage
	Jobs       *jobs.Service
	References *mediaref.Resolver
}

func (s *VideoService) Generate(ctx context.Context, rec *models.VideoGeneration, configID *uint) error {
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
		job, err := s.Jobs.CreateForTargetOrganization(rec.OrganizationID, "video.generate", "video_generation", rec.ID, cfg.Provider, &cfg.ID)
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
		s.failJob(rec.OrganizationID, rec.ID, err)
		return err
	}
	if s.References != nil {
		for source, target := range map[*string]*string{&rec.ImageURL: &imageURL, &rec.FirstFrameURL: &firstFrameURL, &rec.LastFrameURL: &lastFrameURL} {
			if *source == "" {
				continue
			}
			*target, err = s.References.ResolveImage(ctx, cfg.Provider, *source)
			if err != nil {
				s.failJob(rec.OrganizationID, rec.ID, err)
				return err
			}
		}
		referenceImageURLs, err = s.References.ResolveImages(ctx, cfg.Provider, referenceImageURLs)
		if err != nil {
			s.failJob(rec.OrganizationID, rec.ID, err)
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
		rec.Status = "failed"
		rec.ErrorMsg = err.Error()
		rec.UpdatedAt = response.Now()
		scopedDB(db.DB, rec.OrganizationID).Save(rec)
		s.failJob(rec.OrganizationID, rec.ID, err)
		return err
	}
	if result.IsAsync {
		rec.TaskID = result.TaskID
		rec.Status = "processing"
		scopedDB(db.DB, rec.OrganizationID).Save(rec)
		if s.Jobs != nil && rec.JobID != nil {
			_ = s.Jobs.SetWaiting(*rec.JobID, result.TaskID)
		}
		return nil
	}
	return s.Finalize(ctx, rec, result.VideoURL)
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
		scopedDB(db.DB, rec.OrganizationID).Save(rec)
		s.failJob(rec.OrganizationID, rec.ID, err)
		return err
	}
	rec.LocalPath = rel
	rec.VideoURL = s.Store.PublicURL(rel)
	rec.Status = "completed"
	now := response.Now()
	rec.CompletedAt = &now
	rec.UpdatedAt = now
	scopedDB(db.DB, rec.OrganizationID).Save(rec)
	if rec.StoryboardID != nil {
		query := db.DB.Model(&models.Storyboard{}).Where("id = ?", *rec.StoryboardID)
		if rec.OrganizationID != 0 {
			query = query.Where("organization_id = ?", rec.OrganizationID)
		}
		query.Updates(map[string]any{
			"video_url":  rec.VideoURL,
			"status":     "video_ready",
			"updated_at": now,
		})
	}
	registerAsset(models.Asset{OrganizationID: rec.OrganizationID, DramaID: rec.DramaID, StoryboardID: rec.StoryboardID, Name: "生成视频", Type: "video", Category: "video", URL: rec.VideoURL, LocalPath: rec.LocalPath, VideoGenID: &rec.ID})
	if s.Jobs != nil {
		result, _ := json.Marshal(map[string]string{"video_url": rec.VideoURL})
		_ = s.Jobs.SetSucceededByTargetOrganization(rec.OrganizationID, "video_generation", rec.ID, string(result))
	}
	return nil
}

func (s *VideoService) failJob(organizationID, resourceID uint, err error) {
	if s.Jobs != nil && err != nil {
		_ = s.Jobs.SetFailedByTargetOrganization(organizationID, "video_generation", resourceID, err.Error())
	}
}
