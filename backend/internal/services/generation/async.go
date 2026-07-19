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
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	"github.com/eqzhou/flyaimovie/internal/services/mediacleanup"
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
	Cache    *mediacache.Service
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
		if r.Cache != nil {
			go r.cacheLoop()
		}
	})
}

func (r *AsyncRunner) cacheLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			if err := r.purgeCacheOnce(); err != nil {
				log.Printf("purge expired cache: %v", err)
			}
		}
	}
}

func (r *AsyncRunner) purgeCacheOnce() error {
	if r.Cache == nil || r.Store == nil {
		return fmt.Errorf("cache worker is not configured")
	}
	if _, err := r.Cache.PurgeAllExpired(1000); err != nil {
		return err
	}
	_, err := mediacleanup.New(r.Cache.DB, r.Store).ProcessOrganization(0, 1000)
	return err
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
	for processed := 0; processed < 40; processed++ {
		claimed, err := r.Jobs.ClaimWaiting(owner, 1)
		if err != nil {
			log.Printf("claim jobs: %v", err)
			return
		}
		if len(claimed) == 0 {
			return
		}
		job := claimed[0]
		switch job.TargetType {
		case "image_generation":
			if r.Images == nil {
				_ = r.Jobs.SetFailedOwned(job.ID, job.LeaseOwner, "image worker unavailable")
				continue
			}
			r.pollImageJob(job)
		case "video_generation":
			if r.Videos == nil {
				_ = r.Jobs.SetFailedOwned(job.ID, job.LeaseOwner, "video worker unavailable")
				continue
			}
			r.pollVideoJob(job)
		case "storyboard_tts":
			r.runTTSJob(job)
		case "storyboard_compose", "episode_compose", "episode_merge":
			r.runComposeJob(job, owner)
		default:
			_ = r.Jobs.SetFailedOwned(job.ID, job.LeaseOwner, "unsupported job target")
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

type storyboardComposeWrite struct {
	organizationID uint
	storyboardID   uint
	url            string
	asset          models.Asset
}

type episodeMergeWrite struct {
	organizationID uint
	episode        models.Episode
	url            string
	asset          models.Asset
	mergeID        uint
}

type composeWrites struct {
	storyboards []storyboardComposeWrite
	merge       *episodeMergeWrite
}

func (writes *composeWrites) apply(database *gorm.DB) error {
	for _, write := range writes.storyboards {
		if err := scopedDB(database, write.organizationID).Model(&models.Storyboard{}).Where("id = ?", write.storyboardID).Updates(map[string]any{"composed_video_url": write.url, "status": "composed", "updated_at": response.Now()}).Error; err != nil {
			return err
		}
		if err := registerAssetWithDB(database, write.asset); err != nil {
			return err
		}
	}
	if writes.merge != nil {
		write := writes.merge
		timestamp := response.Now()
		if err := scopedDB(database, write.organizationID).Model(&models.Episode{}).Where("id = ?", write.episode.ID).Updates(map[string]any{"video_url": write.url, "status": "completed", "updated_at": timestamp}).Error; err != nil {
			return err
		}
		merge := models.VideoMerge{OrganizationID: write.organizationID, EpisodeID: &write.episode.ID, DramaID: &write.episode.DramaID, Title: write.episode.Title, Status: "completed", MergedURL: write.url, CreatedAt: timestamp, CompletedAt: &timestamp}
		if err := database.Create(&merge).Error; err != nil {
			return err
		}
		write.mergeID = merge.ID
		if err := registerAssetWithDB(database, write.asset); err != nil {
			return err
		}
	}
	return nil
}

func composeWriteTarget(owner []any) *composeWrites {
	if len(owner) < 3 {
		return nil
	}
	writes, _ := owner[2].(*composeWrites)
	return writes
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
	ctx, finish := r.claimedJobContext(job)
	defer finish()
	writes := &composeWrites{}
	var result string
	var err error
	switch job.TargetType {
	case "storyboard_compose":
		result, err = r.composeStoryboard(ctx, job.OrganizationID, p.composeShotPayload, owner, job.ID, writes)
	case "episode_compose":
		result, err = r.composeEpisode(ctx, job.OrganizationID, p.EpisodeID, p.Shots, owner, job.ID, writes)
	case "episode_merge":
		result, err = r.mergeEpisode(ctx, job.OrganizationID, p.EpisodeID, p.Inputs, p.OutputRel, owner, job.ID, writes)
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
	_ = r.Jobs.SetSucceededOwnedWith(ctx, job.ID, owner, result, writes.apply)
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
	canonicalPath, canonicalURL, contentHash, size, err := cacheGeneratedFile(r.Cache, r.Store, organizationID, "storyboard_compose", id, "video", rel, url, "video/mp4")
	if err != nil {
		return "", err
	}
	rel, url = canonicalPath, canonicalURL
	write := storyboardComposeWrite{organizationID: organizationID, storyboardID: id, url: url, asset: models.Asset{OrganizationID: organizationID, StoryboardID: &id, Name: "镜头合成", Type: "video", Category: "composed", URL: url, LocalPath: rel, ContentHash: contentHash, FileSize: size}}
	if writes := composeWriteTarget(owner); writes != nil {
		writes.storyboards = append(writes.storyboards, write)
	} else if err := db.DB.Transaction(func(tx *gorm.DB) error {
		return (&composeWrites{storyboards: []storyboardComposeWrite{write}}).apply(tx)
	}); err != nil {
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
	canonicalPath, canonicalURL, contentHash, size, err := cacheGeneratedFile(r.Cache, r.Store, organizationID, "episode_merge", episodeID, "video", rel, url, "video/mp4")
	if err != nil {
		return "", err
	}
	rel, url = canonicalPath, canonicalURL
	var ep models.Episode
	if err := scopedDB(db.DB, organizationID).Where("id = ?", episodeID).First(&ep).Error; err != nil {
		return "", err
	}
	write := &episodeMergeWrite{organizationID: organizationID, episode: ep, url: url, asset: models.Asset{OrganizationID: organizationID, DramaID: &ep.DramaID, EpisodeID: &ep.ID, Name: ep.Title, Type: "video", Category: "episode", URL: url, LocalPath: rel, ContentHash: contentHash, FileSize: size}}
	if writes := composeWriteTarget(owner); writes != nil {
		writes.merge = write
	} else if err := db.DB.Transaction(func(tx *gorm.DB) error { return (&composeWrites{merge: write}).apply(tx) }); err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]any{"merged_url": url, "merge_id": write.mergeID})
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
		_ = r.Jobs.SetFailedOwned(job.ID, job.LeaseOwner, "tts worker unavailable")
		return
	}
	ctx, finish := r.claimedJobContext(job)
	defer finish()
	if ctx.Err() != nil {
		return
	}
	var storyboard models.Storyboard
	if err := db.DB.Where("organization_id = ? AND id = ? AND deleted_at IS NULL", job.OrganizationID, job.TargetID).First(&storyboard).Error; err != nil {
		_ = r.Jobs.SetFailedOwned(job.ID, job.LeaseOwner, err.Error())
		return
	}
	if _, err := r.TTS.GenerateForStoryboardOrganizationOwned(ctx, job.OrganizationID, storyboard.ID, job.ConfigID, r.Jobs, job.ID, job.LeaseOwner); err != nil {
		if ctx.Err() == nil {
			_ = r.Jobs.SetFailedOwned(job.ID, job.LeaseOwner, err.Error())
		}
		return
	}
}

func (r *AsyncRunner) pollImageJob(job models.GenerationJob) {
	ctx, finish := r.claimedJobContext(job)
	defer finish()
	if ctx.Err() != nil {
		return
	}
	var rec models.ImageGeneration
	if err := scopedDB(db.DB, job.OrganizationID).Where("id = ?", job.TargetID).First(&rec).Error; err != nil {
		if job.ID != 0 && job.LeaseOwner != "" {
			_ = r.Jobs.SetFailedOwned(job.ID, job.LeaseOwner, "image generation target not found")
		}
		return
	}
	if rec.TaskID == "" {
		r.failClaimedJob(job, "image task id missing")
		return
	}
	cfg, err := ai.GetTaskConfigOrganization(job.OrganizationID, "image", rec.ConfigID)
	if err != nil {
		r.failClaimedJob(job, err.Error())
		return
	}
	res, err := adapters.GetImageAdapter(cfg.Provider).Poll(ctx, adapters.AIConfig{Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model}, rec.TaskID)
	if err != nil {
		if ctx.Err() == nil {
			r.requeueJob(job, err)
		}
		return
	}
	if ctx.Err() != nil || (job.ID != 0 && !r.Jobs.IsOwned(job.ID, job.LeaseOwner)) {
		return
	}
	if res.Status == "completed" && res.ImageURL != "" {
		ownedJobID := job.ID
		if job.LeaseOwner == "" {
			ownedJobID = 0
		}
		if err := r.Images.FinalizeOwned(ctx, &rec, res.ImageURL, ownedJobID, job.LeaseOwner); err != nil {
			updateGridHistory(rec.OrganizationID, rec.ID, "", "failed", err.Error())
			return
		}
		updateGridHistory(rec.OrganizationID, rec.ID, rec.ImageURL, "completed", "")
		return
	}
	if res.Status == "failed" {
		rec.Status, rec.ErrorMsg = "failed", res.Error
		rec.UpdatedAt = response.Now()
		if r.Jobs != nil && job.ID > 0 && job.LeaseOwner != "" {
			if err := r.Jobs.SetFailedOwnedWith(context.Background(), job.ID, job.LeaseOwner, res.Error, func(tx *gorm.DB) error { return scopedDB(tx, job.OrganizationID).Save(&rec).Error }); err == nil {
				updateGridHistory(rec.OrganizationID, rec.ID, "", "failed", res.Error)
			}
		} else if err := scopedDB(db.DB, job.OrganizationID).Save(&rec).Error; err == nil {
			updateGridHistory(rec.OrganizationID, rec.ID, "", "failed", res.Error)
		}
		return
	}
	r.requeueJob(job, nil)
}

func (r *AsyncRunner) pollVideoJob(job models.GenerationJob) {
	ctx, finish := r.claimedJobContext(job)
	defer finish()
	if ctx.Err() != nil {
		return
	}
	var rec models.VideoGeneration
	if err := scopedDB(db.DB, job.OrganizationID).Where("id = ?", job.TargetID).First(&rec).Error; err != nil {
		if job.ID != 0 && job.LeaseOwner != "" {
			_ = r.Jobs.SetFailedOwned(job.ID, job.LeaseOwner, "video generation target not found")
		}
		return
	}
	if rec.TaskID == "" {
		r.failClaimedJob(job, "video task id missing")
		return
	}
	cfg, err := ai.GetTaskConfigOrganization(job.OrganizationID, "video", rec.ConfigID)
	if err != nil {
		r.failClaimedJob(job, err.Error())
		return
	}
	res, err := adapters.GetVideoAdapter(cfg.Provider).Poll(ctx, adapters.AIConfig{Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model}, rec.TaskID)
	if err != nil {
		if ctx.Err() == nil {
			r.requeueJob(job, err)
		}
		return
	}
	if ctx.Err() != nil || (job.ID != 0 && !r.Jobs.IsOwned(job.ID, job.LeaseOwner)) {
		return
	}
	if res.Status == "completed" && res.VideoURL != "" {
		ownedJobID := job.ID
		if job.LeaseOwner == "" {
			ownedJobID = 0
		}
		_ = r.Videos.FinalizeAuthorizedOwned(ctx, &rec, res.VideoURL, res.BearerToken, ownedJobID, job.LeaseOwner)
		return
	}
	if res.Status == "failed" {
		rec.Status, rec.ErrorMsg = "failed", res.Error
		rec.UpdatedAt = response.Now()
		if r.Jobs != nil && job.ID > 0 && job.LeaseOwner != "" {
			_ = r.Jobs.SetFailedOwnedWith(context.Background(), job.ID, job.LeaseOwner, res.Error, func(tx *gorm.DB) error { return scopedDB(tx, job.OrganizationID).Save(&rec).Error })
		} else {
			_ = scopedDB(db.DB, job.OrganizationID).Save(&rec).Error
		}
		return
	}
	r.requeueJob(job, nil)
}

func (r *AsyncRunner) requeueJob(job models.GenerationJob, err error) {
	updates := map[string]any{"status": jobs.StatusWaitingProvider, "available_at": time.Now().UTC().Add(4 * time.Second).Format(time.RFC3339), "lease_owner": "", "lease_expires_at": nil, "updated_at": response.Now()}
	if err != nil {
		updates["last_error"] = err.Error()
	}
	query := db.DB.Model(&models.GenerationJob{}).Where("id = ? AND status = ?", job.ID, jobs.StatusRunning)
	if job.LeaseOwner != "" {
		query = query.Where("lease_owner = ?", job.LeaseOwner)
	}
	query.Updates(updates)
}

func (r *AsyncRunner) claimedJobContext(job models.GenerationJob) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	if r.Jobs == nil || job.ID == 0 || job.LeaseOwner == "" {
		return ctx, cancel
	}
	if !r.Jobs.IsOwned(job.ID, job.LeaseOwner) {
		cancel()
		return ctx, cancel
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				current, err := r.Jobs.Get(job.ID)
				if err != nil || current.Status != jobs.StatusRunning || current.LeaseOwner != job.LeaseOwner || r.Jobs.RenewLease(job.ID, job.LeaseOwner) != nil {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() {
		close(done)
		cancel()
	}
}

func (r *AsyncRunner) failClaimedJob(job models.GenerationJob, message string) {
	if r.Jobs == nil || job.ID == 0 {
		return
	}
	if job.LeaseOwner != "" {
		_ = r.Jobs.SetFailedOwned(job.ID, job.LeaseOwner, message)
		return
	}
	_ = r.Jobs.SetFailed(job.ID, message)
}

func (r *AsyncRunner) pollImages() {
	var rows []models.ImageGeneration
	db.DB.Where("status = ? AND task_id != '' AND task_id IS NOT NULL AND organization_id > 0", "processing").
		Order("id asc").Limit(20).Find(&rows)
	for _, record := range rows {
		r.pollImageJob(models.GenerationJob{OrganizationID: record.OrganizationID, TargetID: record.ID})
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
	for _, record := range rows {
		r.pollVideoJob(models.GenerationJob{OrganizationID: record.OrganizationID, TargetID: record.ID})
	}
}
