package generation

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"log"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/adapters"
	"github.com/eqzhou/flyaimovie/internal/services/ai"
	"github.com/eqzhou/flyaimovie/internal/services/ffmpeg"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"gorm.io/gorm"
)

// AsyncRunner processes pending image/video generation jobs in background.
type AsyncRunner struct {
	Images   *ImageService
	Videos   *VideoService
	TTS      *TTSService
	Jobs     *jobs.Service
	Store    *storage.LocalStorage
	once     sync.Once
	stopOnce sync.Once
	stop     chan struct{}
}

// Stop asks the background worker loop to exit. It is primarily useful for
// graceful shutdown and isolated integration tests.
func (r *AsyncRunner) Stop() {
	if r.stop == nil {
		return
	}
	r.stopOnce.Do(func() { close(r.stop) })
}

func (r *AsyncRunner) Start() {
	r.once.Do(func() {
		r.stop = make(chan struct{})
		if r.Jobs != nil {
			if recovered, err := r.Jobs.RecoverExpired(); err != nil {
				log.Printf("recover jobs: %v", err)
			} else if recovered > 0 {
				log.Printf("recovered %d expired generation jobs", recovered)
			}
		}
		go r.loop()
	})
}

func (r *AsyncRunner) loop() {
	t := time.NewTicker(4 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-t.C:
			r.pollClaimed()
		}
	}
}

func (r *AsyncRunner) pollClaimed() {
	if r.Jobs == nil {
		r.pollImages()
		r.pollVideos()
		return
	}
	owner := "worker-" + uuid.NewString()
	claimed, err := r.Jobs.ClaimWaiting(owner, 40)
	if err != nil {
		log.Printf("claim jobs: %v", err)
		return
	}
	for _, job := range claimed {
		switch job.TargetType {
		case "image_generation":
			r.pollImageJob(job)
		case "video_generation":
			r.pollVideoJob(job)
		case "storyboard_tts":
			r.runTTSJob(job)
		case "storyboard_compose", "episode_compose", "episode_merge":
			r.runComposeJob(job, owner)
		}
	}
}

type composePayload struct {
	composeShotPayload
	EpisodeID uint                 `json:"episode_id"`
	Shots     []composeShotPayload `json:"shots"`
	Inputs    []string             `json:"inputs"`
}

type composeShotPayload struct {
	StoryboardID uint   `json:"storyboard_id"`
	VideoURL     string `json:"video_url"`
	AudioURL     string `json:"audio_url"`
	SubtitleURL  string `json:"subtitle_url"`
	OutputRel    string `json:"output_rel"`
}

func (r *AsyncRunner) runComposeJob(job models.GenerationJob, owner string) {
	if r.Jobs == nil {
		return
	}
	if r.Store == nil {
		_ = r.Jobs.SetFailedOwned(job.ID, owner, "compose worker unavailable")
		return
	}
	var p composePayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &p); err != nil {
		_ = r.Jobs.SetFailedOwned(job.ID, owner, "invalid compose payload")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				current, err := r.Jobs.Get(job.ID)
				if err != nil || current.Status == jobs.StatusCanceled || current.Status != jobs.StatusRunning || current.LeaseOwner != owner {
					cancel()
					return
				}
				if err := r.Jobs.RenewLease(job.ID, owner); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	var result string
	var err error
	switch job.TargetType {
	case "storyboard_compose":
		result, err = r.composeStoryboard(ctx, job.OrganizationID, p.composeShotPayload, owner, job.ID)
	case "episode_compose":
		result, err = r.composeEpisode(ctx, job.OrganizationID, p.EpisodeID, p.Shots, owner, job.ID)
	case "episode_merge":
		result, err = r.mergeEpisode(ctx, job.OrganizationID, p.EpisodeID, p.Inputs, p.OutputRel, owner, job.ID)
	}
	if err != nil {
		if ctx.Err() != nil {
			_ = r.Jobs.SetFailedOwned(job.ID, owner, "compose canceled")
			return
		}
		_ = r.Jobs.SetFailedOwned(job.ID, owner, err.Error())
		return
	}
	if !r.Jobs.IsOwned(job.ID, owner) {
		return
	}
	_ = r.Jobs.SetSucceededOwned(job.ID, owner, result)
}

func (r *AsyncRunner) composeStoryboard(ctx context.Context, organizationID uint, payload composeShotPayload, owner ...any) (string, error) {
	id := payload.StoryboardID
	hasSnapshot := payload.VideoURL != "" || payload.OutputRel != ""
	var sb models.Storyboard
	if err := scopedDB(db.DB, organizationID).Where("id = ?", id).First(&sb).Error; err != nil {
		return "", err
	}
	videoURL := payload.VideoURL
	if !hasSnapshot {
		videoURL = sb.VideoURL
	}
	if videoURL == "" {
		return "", fmt.Errorf("storyboard has no video")
	}
	video, err := EnsureLocalFile(r.Store, videoURL)
	if err != nil {
		return "", err
	}
	audio, subtitle := "", ""
	audioURL := payload.AudioURL
	if !hasSnapshot {
		audioURL = sb.TTSAudioURL
	}
	if audioURL != "" {
		audio, err = EnsureLocalFile(r.Store, audioURL)
		if err != nil {
			return "", err
		}
	}
	subtitleURL := payload.SubtitleURL
	if !hasSnapshot {
		subtitleURL = sb.SubtitleURL
	}
	if subtitleURL != "" {
		subtitle, err = EnsureLocalFile(r.Store, subtitleURL)
		if err != nil {
			return "", err
		}
	}
	rel := payload.OutputRel
	if rel == "" {
		rel = filepath.ToSlash(filepath.Join("composed", fmt.Sprintf("shot_%d.mp4", id)))
	}
	if err := validateOutputRel(rel); err != nil {
		return "", err
	}
	out := filepath.Join(r.Store.Root, filepath.FromSlash(rel))
	if err := ffmpeg.ComposeShotContext(ctx, video, audio, subtitle, out); err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if len(owner) >= 2 {
		worker, _ := owner[0].(string)
		jobID, _ := owner[1].(uint)
		if !r.Jobs.IsOwned(jobID, worker) {
			return "", context.Canceled
		}
	}
	url := r.Store.PublicURL(rel)
	if err := scopedDB(db.DB, organizationID).Model(&models.Storyboard{}).Where("id = ?", id).Updates(map[string]any{"composed_video_url": url, "status": "composed", "updated_at": response.Now()}).Error; err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]string{"composed_video_url": url})
	return string(b), nil
}

func (r *AsyncRunner) composeEpisode(ctx context.Context, organizationID, episodeID uint, shots []composeShotPayload, owner ...any) (string, error) {
	if len(shots) > 0 {
		for _, shot := range shots {
			if _, err := r.composeStoryboard(ctx, organizationID, shot, owner...); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf(`{"episode_id":%d,"composed":%d}`, episodeID, len(shots)), nil
	}
	var rows []models.Storyboard
	if err := scopedDB(db.DB, organizationID).Where("episode_id = ? AND deleted_at IS NULL", episodeID).Order("storyboard_number").Find(&rows).Error; err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("episode has no storyboards")
	}
	for _, sb := range rows {
		if _, err := r.composeStoryboard(ctx, organizationID, composeShotPayload{StoryboardID: sb.ID}, owner...); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf(`{"episode_id":%d,"composed":%d}`, episodeID, len(rows)), nil
}

func (r *AsyncRunner) mergeEpisode(ctx context.Context, organizationID, episodeID uint, inputs []string, outputRel string, owner ...any) (string, error) {
	paths := make([]string, 0)
	if len(inputs) > 0 {
		for _, src := range inputs {
			p, err := EnsureLocalFile(r.Store, src)
			if err != nil {
				return "", err
			}
			paths = append(paths, p)
		}
	}
	var rows []models.Storyboard
	if len(paths) == 0 {
		if err := scopedDB(db.DB, organizationID).Where("episode_id = ? AND deleted_at IS NULL", episodeID).Order("storyboard_number").Find(&rows).Error; err != nil {
			return "", err
		}
	}
	if len(paths) == 0 && len(rows) == 0 {
		return "", fmt.Errorf("no videos to merge")
	}
	for _, sb := range rows {
		src := sb.ComposedVideoURL
		if src == "" {
			src = sb.VideoURL
		}
		if src == "" {
			return "", fmt.Errorf("storyboard %d has no video", sb.ID)
		}
		p, err := EnsureLocalFile(r.Store, src)
		if err != nil {
			return "", err
		}
		paths = append(paths, p)
	}
	rel := outputRel
	if rel == "" {
		rel = filepath.ToSlash(filepath.Join("merged", fmt.Sprintf("episode_%d.mp4", episodeID)))
	}
	if err := validateOutputRel(rel); err != nil {
		return "", err
	}
	out := filepath.Join(r.Store.Root, filepath.FromSlash(rel))
	if err := ffmpeg.MergeEpisodeContext(ctx, paths, out); err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if len(owner) >= 2 {
		worker, _ := owner[0].(string)
		jobID, _ := owner[1].(uint)
		if !r.Jobs.IsOwned(jobID, worker) {
			return "", context.Canceled
		}
	}
	url := r.Store.PublicURL(rel)
	ts := response.Now()
	var ep models.Episode
	if err := scopedDB(db.DB, organizationID).Where("id = ?", episodeID).First(&ep).Error; err != nil {
		return "", err
	}
	if err := scopedDB(db.DB, organizationID).Model(&ep).Updates(map[string]any{"video_url": url, "status": "completed", "updated_at": ts}).Error; err != nil {
		return "", err
	}
	merge := models.VideoMerge{OrganizationID: organizationID, EpisodeID: &ep.ID, DramaID: &ep.DramaID, Title: ep.Title, Status: "completed", MergedURL: url, CreatedAt: ts, CompletedAt: &ts}
	if err := db.DB.Create(&merge).Error; err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]any{"merged_url": url, "merge_id": merge.ID})
	return string(b), nil
}

func scopedDB(database *gorm.DB, organizationID uint) *gorm.DB {
	if organizationID == 0 {
		return database
	}
	return database.Where("organization_id = ?", organizationID)
}

func validateOutputRel(rel string) error {
	if rel == "" || strings.Contains(rel, "\\") || strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) {
		return fmt.Errorf("invalid compose output path")
	}
	clean := path.Clean(filepath.ToSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("compose output path escapes storage root")
	}
	if !strings.HasPrefix(clean, "composed/") && !strings.HasPrefix(clean, "merged/") {
		return fmt.Errorf("compose output directory is not allowed")
	}
	return nil
}

func (r *AsyncRunner) runTTSJob(job models.GenerationJob) {
	if r.TTS == nil {
		_ = r.Jobs.SetFailedByTargetOrganization(job.OrganizationID, job.TargetType, job.TargetID, "tts worker unavailable")
		return
	}
	var storyboard models.Storyboard
	if err := db.DB.Where("organization_id = ? AND id = ? AND deleted_at IS NULL", job.OrganizationID, job.TargetID).First(&storyboard).Error; err != nil {
		_ = r.Jobs.SetFailedByTargetOrganization(job.OrganizationID, job.TargetType, job.TargetID, err.Error())
		return
	}
	var episode models.Episode
	if err := db.DB.Where("organization_id = ? AND id = ? AND deleted_at IS NULL", job.OrganizationID, storyboard.EpisodeID).First(&episode).Error; err != nil {
		_ = r.Jobs.SetFailedByTargetOrganization(job.OrganizationID, job.TargetType, job.TargetID, err.Error())
		return
	}
	if _, err := r.TTS.GenerateForStoryboardOrganization(context.Background(), job.OrganizationID, storyboard.ID, episode.AudioConfigID); err != nil {
		_ = r.Jobs.SetFailedByTargetOrganization(job.OrganizationID, job.TargetType, job.TargetID, err.Error())
		return
	}
	_ = r.Jobs.SetSucceededByTargetOrganization(job.OrganizationID, job.TargetType, job.TargetID, `{"status":"completed"}`)
}

func (r *AsyncRunner) pollImageJob(job models.GenerationJob) {
	var rec models.ImageGeneration
	if err := scopedDB(db.DB, job.OrganizationID).Where("id = ?", job.TargetID).First(&rec).Error; err != nil {
		return
	}
	if rec.TaskID == "" {
		r.Images.failJob(rec.OrganizationID, rec.ID, fmt.Errorf("image task id missing"))
		return
	}
	cfg, err := ai.GetTaskConfigOrganization(job.OrganizationID, "image", rec.ConfigID)
	if err != nil {
		r.Images.failJob(rec.OrganizationID, rec.ID, err)
		return
	}
	res, err := adapters.GetImageAdapter(cfg.Provider).Poll(context.Background(), adapters.AIConfig{Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model}, rec.TaskID)
	if err != nil {
		r.requeueJob(job.ID, err)
		return
	}
	if res.Status == "completed" && res.ImageURL != "" {
		if err := r.Images.Finalize(context.Background(), &rec, res.ImageURL); err != nil {
			updateGridHistory(rec.OrganizationID, rec.ID, "", "failed", err.Error())
			return
		}
		updateGridHistory(rec.OrganizationID, rec.ID, rec.ImageURL, "completed", "")
		return
	}
	if res.Status == "failed" {
		rec.Status, rec.ErrorMsg = "failed", res.Error
		rec.UpdatedAt = response.Now()
		scopedDB(db.DB, job.OrganizationID).Save(&rec)
		r.Images.failJob(rec.OrganizationID, rec.ID, fmt.Errorf("%s", res.Error))
		updateGridHistory(rec.OrganizationID, rec.ID, "", "failed", res.Error)
		return
	}
	r.requeueJob(job.ID, nil)
}

func (r *AsyncRunner) pollVideoJob(job models.GenerationJob) {
	var rec models.VideoGeneration
	if err := scopedDB(db.DB, job.OrganizationID).Where("id = ?", job.TargetID).First(&rec).Error; err != nil {
		return
	}
	if rec.TaskID == "" {
		r.Videos.failJob(rec.OrganizationID, rec.ID, fmt.Errorf("video task id missing"))
		return
	}
	cfg, err := ai.GetTaskConfigOrganization(job.OrganizationID, "video", rec.ConfigID)
	if err != nil {
		r.Videos.failJob(rec.OrganizationID, rec.ID, err)
		return
	}
	res, err := adapters.GetVideoAdapter(cfg.Provider).Poll(context.Background(), adapters.AIConfig{Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model}, rec.TaskID)
	if err != nil {
		r.requeueJob(job.ID, err)
		return
	}
	if res.Status == "completed" && res.VideoURL != "" {
		_ = r.Videos.Finalize(context.Background(), &rec, res.VideoURL)
		return
	}
	if res.Status == "failed" {
		rec.Status, rec.ErrorMsg = "failed", res.Error
		rec.UpdatedAt = response.Now()
		scopedDB(db.DB, job.OrganizationID).Save(&rec)
		r.Videos.failJob(rec.OrganizationID, rec.ID, fmt.Errorf("%s", res.Error))
		return
	}
	r.requeueJob(job.ID, nil)
}

func (r *AsyncRunner) requeueJob(id uint, err error) {
	updates := map[string]any{"status": jobs.StatusWaitingProvider, "available_at": time.Now().UTC().Add(4 * time.Second).Format(time.RFC3339), "lease_owner": "", "lease_expires_at": nil, "updated_at": response.Now()}
	if err != nil {
		updates["last_error"] = err.Error()
	}
	db.DB.Model(&models.GenerationJob{}).Where("id = ? AND status = ?", id, jobs.StatusRunning).Updates(updates)
}

func (r *AsyncRunner) pollImages() {
	var rows []models.ImageGeneration
	db.DB.Where("status = ? AND task_id != '' AND task_id IS NOT NULL AND organization_id > 0", "processing").
		Order("id asc").Limit(20).Find(&rows)
	for _, rec := range rows {
		cfg, err := ai.GetTaskConfigOrganization(rec.OrganizationID, "image", rec.ConfigID)
		if err != nil {
			continue
		}
		// Prefer provider from record if possible
		if rec.ConfigID == nil && rec.Provider != "" {
			var row models.AIServiceConfig
			if err := db.DB.Where("organization_id = ? AND provider = ? AND service_type = ? AND is_active = ?", rec.OrganizationID, rec.Provider, "image", true).
				Order("is_default desc, priority desc").First(&row).Error; err == nil {
				cfg = &ai.ServiceConfig{ID: row.ID, Provider: row.Provider, BaseURL: row.BaseURL, APIKey: row.APIKey, Model: row.Model}
			}
		}
		adapter := adapters.GetImageAdapter(cfg.Provider)
		res, err := adapter.Poll(context.Background(), adapters.AIConfig{
			Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model,
		}, rec.TaskID)
		if err != nil {
			continue
		}
		if res.Status == "completed" && res.ImageURL != "" {
			local, err := r.Images.Download(context.Background(), res.ImageURL, "images")
			if err != nil {
				rec.Status = "failed"
				rec.ErrorMsg = err.Error()
				rec.UpdatedAt = response.Now()
				scopedDB(db.DB, rec.OrganizationID).Save(&rec)
				r.Images.failJob(rec.OrganizationID, rec.ID, err)
				continue
			}
			rec.LocalPath = local
			rec.ImageURL = r.Images.Store.PublicURL(local)
			rec.Status = "completed"
			now := response.Now()
			rec.CompletedAt = &now
			rec.UpdatedAt = now
			scopedDB(db.DB, rec.OrganizationID).Save(&rec)
			r.Images.ApplySideEffects(&rec)
			updateGridHistory(rec.OrganizationID, rec.ID, rec.ImageURL, "completed", "")
			if rec.JobID != nil {
				_ = r.Images.Jobs.SetSucceededByTargetOrganization(rec.OrganizationID, "image_generation", rec.ID, fmt.Sprintf(`{"image_url":%q}`, rec.ImageURL))
			}
		} else if res.Status == "failed" {
			rec.Status = "failed"
			rec.ErrorMsg = res.Error
			rec.UpdatedAt = response.Now()
			scopedDB(db.DB, rec.OrganizationID).Save(&rec)
			r.Images.failJob(rec.OrganizationID, rec.ID, fmt.Errorf("%s", res.Error))
			updateGridHistory(rec.OrganizationID, rec.ID, "", "failed", res.Error)
		}
	}
}

func updateGridHistory(organizationID, imageGenerationID uint, imageURL, status, errorMessage string) {
	updates := map[string]any{"status": status, "updated_at": response.Now()}
	if imageURL != "" {
		updates["image_url"] = imageURL
	}
	if errorMessage != "" {
		updates["error_msg"] = errorMessage
	}
	if status == "completed" {
		now := response.Now()
		updates["completed_at"] = &now
	}
	query := db.DB.Model(&models.GridHistory{}).Where("image_gen_id = ? AND status NOT IN ?", imageGenerationID, []string{"split", "failed"})
	if organizationID != 0 {
		query = query.Where("organization_id = ?", organizationID)
	}
	query.Updates(updates)
}

func (r *AsyncRunner) pollVideos() {
	var rows []models.VideoGeneration
	db.DB.Where("status IN ? AND task_id != '' AND task_id IS NOT NULL AND organization_id > 0", []string{"processing", "pending"}).
		Order("id asc").Limit(20).Find(&rows)
	for _, rec := range rows {
		cfg, err := ai.GetTaskConfigOrganization(rec.OrganizationID, "video", rec.ConfigID)
		if err != nil {
			continue
		}
		if rec.ConfigID == nil && rec.Provider != "" {
			var row models.AIServiceConfig
			if err := db.DB.Where("organization_id = ? AND provider = ? AND service_type = ? AND is_active = ?", rec.OrganizationID, rec.Provider, "video", true).
				Order("is_default desc, priority desc").First(&row).Error; err == nil {
				cfg = &ai.ServiceConfig{ID: row.ID, Provider: row.Provider, BaseURL: row.BaseURL, APIKey: row.APIKey, Model: row.Model}
			}
		}
		adapter := adapters.GetVideoAdapter(cfg.Provider)
		res, err := adapter.Poll(context.Background(), adapters.AIConfig{
			Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model,
		}, rec.TaskID)
		if err != nil {
			log.Printf("video poll %d: %v", rec.ID, err)
			continue
		}
		if res.Status == "completed" && res.VideoURL != "" {
			_ = r.Videos.Finalize(context.Background(), &rec, res.VideoURL)
		} else if res.Status == "failed" {
			rec.Status = "failed"
			rec.ErrorMsg = res.Error
			rec.UpdatedAt = response.Now()
			scopedDB(db.DB, rec.OrganizationID).Save(&rec)
			r.Videos.failJob(rec.OrganizationID, rec.ID, fmt.Errorf("%s", res.Error))
		} else {
			rec.Status = res.Status
			rec.UpdatedAt = response.Now()
			scopedDB(db.DB, rec.OrganizationID).Save(&rec)
		}
	}
}
