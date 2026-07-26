package production

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/agents"
	"github.com/eqzhou/flyaimovie/internal/services/generation"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/services/prompttemplate"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	StatusQueued    = "queued"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"

	StageScript      = "script"
	StageExtract     = "extract"
	StageStoryboards = "storyboards"
	StageFrames      = "frames"
	StageVideos      = "videos"
	StageTTS         = "tts"
	StageCompose     = "compose"
	StageMerge       = "merge"
	StageCompleted   = "completed"
)

var (
	ErrActiveRun   = errors.New("an active production run already exists")
	ErrRunNotFound = errors.New("production run not found")
	ErrTerminalRun = errors.New("production run cannot transition")
)

const productionLease = 90 * time.Second

type Service struct {
	DB            *gorm.DB
	Agents        *agents.Runner
	Images        *generation.ImageService
	Videos        *generation.VideoService
	Jobs          *jobs.Service
	runMu         sync.Mutex
	runs          map[uint]map[string]context.CancelFunc
	leaseDuration time.Duration
}

func New(database *gorm.DB, agentRunner *agents.Runner, images *generation.ImageService, videos *generation.VideoService, jobService *jobs.Service) *Service {
	return &Service{
		DB: database, Agents: agentRunner, Images: images, Videos: videos, Jobs: jobService,
		runs: map[uint]map[string]context.CancelFunc{}, leaseDuration: productionLease,
	}
}

func (s *Service) Create(organizationID, dramaID, episodeID uint) (*models.ProductionRun, error) {
	if dramaID == 0 || episodeID == 0 {
		return nil, fmt.Errorf("drama and episode are required")
	}
	var run models.ProductionRun
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var episode models.Episode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, episodeID, dramaID).First(&episode).Error; err != nil {
			return ErrRunNotFound
		}
		var active int64
		if err := tx.Model(&models.ProductionRun{}).Where("organization_id = ? AND episode_id = ? AND status = ?", organizationID, episodeID, StatusQueued).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrActiveRun
		}
		now := response.Now()
		run = models.ProductionRun{OrganizationID: organizationID, DramaID: dramaID, EpisodeID: episodeID, Status: StatusQueued, Stage: StageScript, Progress: 0, StatusMessage: "等待开始", Attempt: 1, MaxAttempts: 3, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		return tx.Model(&episode).Updates(map[string]any{"status": "processing", "updated_at": now}).Error
	})
	return &run, err
}

func (s *Service) Get(organizationID, id uint) (*models.ProductionRun, error) {
	var run models.ProductionRun
	if err := s.DB.Where("organization_id = ? AND id = ?", organizationID, id).First(&run).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRunNotFound
		}
		return nil, err
	}
	return &run, nil
}

func (s *Service) List(organizationID, episodeID uint, limit int) ([]models.ProductionRun, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	query := s.DB.Where("organization_id = ?", organizationID)
	if episodeID > 0 {
		query = query.Where("episode_id = ?", episodeID)
	}
	var rows []models.ProductionRun
	err := query.Order("id desc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *Service) Cancel(organizationID, id uint) error {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var run models.ProductionRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND id = ?", organizationID, id).First(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRunNotFound
			}
			return err
		}
		if run.Status != StatusQueued {
			return ErrTerminalRun
		}
		now := response.Now()
		if err := tx.Model(&run).Updates(map[string]any{"status": StatusCanceled, "status_message": "已取消", "cancel_requested_at": now, "completed_at": now, "lease_owner": "", "lease_expires_at": nil, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&models.Episode{}).Where("organization_id = ? AND id = ? AND status = ? AND (video_url = '' OR video_url IS NULL)", organizationID, run.EpisodeID, "processing").Updates(map[string]any{"status": "draft", "updated_at": now}).Error
	})
	if err != nil {
		return err
	}
	s.cancelRunContext(id)
	return s.cancelActiveChildren(organizationID, id)
}

func (s *Service) Retry(organizationID, id uint) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var run models.ProductionRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND id = ?", organizationID, id).First(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRunNotFound
			}
			return err
		}
		if run.Status != StatusFailed && run.Status != StatusCanceled {
			return ErrTerminalRun
		}
		if run.Attempt >= run.MaxAttempts {
			return fmt.Errorf("%w: maximum attempts reached", ErrTerminalRun)
		}
		var active int64
		if err := tx.Model(&models.ProductionRun{}).Where("organization_id = ? AND episode_id = ? AND id <> ? AND status = ?", organizationID, run.EpisodeID, run.ID, StatusQueued).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrActiveRun
		}
		if err := tx.Model(&models.GenerationJob{}).Where("organization_id = ? AND production_run_id = ? AND status IN ?", organizationID, id, []string{jobs.StatusFailed, jobs.StatusCanceled}).Update("production_run_id", nil).Error; err != nil {
			return err
		}
		now := response.Now()
		updates := map[string]any{"status": StatusQueued, "status_message": "等待重试", "last_error": "", "attempt": run.Attempt + 1, "available_at": now, "lease_owner": "", "lease_expires_at": nil, "cancel_requested_at": nil, "completed_at": nil, "updated_at": now}
		if err := tx.Model(&run).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Model(&models.Episode{}).Where("organization_id = ? AND id = ?", organizationID, run.EpisodeID).Updates(map[string]any{"status": "processing", "updated_at": now}).Error
	})
}

func (s *Service) ProcessAvailable(ctx context.Context, owner string, limit int) (int, error) {
	if owner == "" {
		return 0, fmt.Errorf("worker owner is required")
	}
	if limit < 1 || limit > 20 {
		limit = 5
	}
	processed := 0
	for processed < limit {
		runs, err := s.claim(owner, 1)
		if err != nil {
			return processed, err
		}
		if len(runs) == 0 {
			break
		}
		run := runs[0]
		processed++
		if err := s.process(ctx, &run); err != nil {
			if ctx.Err() != nil {
				_ = s.release(run.ID, run.LeaseOwner)
				return processed, ctx.Err()
			}
			if markErr := s.failRun(&run, err); markErr != nil {
				return processed, markErr
			}
		}
	}
	return processed, nil
}

func (s *Service) claim(owner string, limit int) ([]models.ProductionRun, error) {
	now := response.Now()
	var candidates []models.ProductionRun
	if err := s.DB.Where("status = ? AND available_at <= ? AND (lease_expires_at IS NULL OR lease_expires_at < ?)", StatusQueued, now, now).Order("id").Limit(limit).Find(&candidates).Error; err != nil {
		return nil, err
	}
	claimed := make([]models.ProductionRun, 0, len(candidates))
	expires := time.Now().UTC().Add(s.productionLeaseDuration()).Format(time.RFC3339)
	for _, candidate := range candidates {
		updates := map[string]any{"lease_owner": owner, "lease_expires_at": expires, "updated_at": now}
		if candidate.StartedAt == nil {
			updates["started_at"] = now
		}
		result := s.DB.Model(&models.ProductionRun{}).Where("id = ? AND status = ? AND (lease_expires_at IS NULL OR lease_expires_at < ?)", candidate.ID, StatusQueued, now).Updates(updates)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 1 {
			candidate.LeaseOwner, candidate.LeaseExpiresAt = owner, &expires
			claimed = append(claimed, candidate)
		}
	}
	return claimed, nil
}

func (s *Service) process(ctx context.Context, run *models.ProductionRun) error {
	runContext, cancel := context.WithCancel(ctx)
	s.registerRunContext(run.ID, run.LeaseOwner, cancel)
	heartbeatErrors := s.startLeaseHeartbeat(runContext, cancel, run.ID, run.LeaseOwner)
	defer func() {
		cancel()
		s.unregisterRunContext(run.ID, run.LeaseOwner)
	}()
	var active int64
	if err := s.DB.Model(&models.ProductionRun{}).Where("id = ? AND status = ? AND lease_owner = ?", run.ID, StatusQueued, run.LeaseOwner).Count(&active).Error; err != nil {
		return err
	}
	if active != 1 {
		return ErrTerminalRun
	}
	if err := runContext.Err(); err != nil {
		return err
	}
	var err error
	switch run.Stage {
	case StageScript:
		err = s.agentStage(runContext, run, "script_rewriter", "请将本集内容改写为格式化短剧剧本并保存")
	case StageExtract:
		err = s.agentStage(runContext, run, "extractor", "请提取本集角色与场景并去重保存")
	case StageStoryboards:
		err = s.agentStage(runContext, run, "storyboard_breaker", "请根据本集剧本拆解完整分镜并保存")
	case StageFrames:
		err = s.frameStage(runContext, run)
	case StageVideos:
		err = s.videoStage(runContext, run)
	case StageTTS:
		err = s.ttsStage(run)
	case StageCompose:
		err = s.composeStage(run)
	case StageMerge:
		err = s.mergeStage(run)
	default:
		err = fmt.Errorf("unsupported production stage %q", run.Stage)
	}
	if err != nil {
		return err
	}
	cancel()
	if heartbeatErr := <-heartbeatErrors; heartbeatErr != nil {
		return heartbeatErr
	}
	if run.Status != StatusQueued {
		return nil
	}
	return s.release(run.ID, run.LeaseOwner)
}

func (s *Service) cancelRunContext(id uint) {
	s.runMu.Lock()
	cancellations := make([]context.CancelFunc, 0, len(s.runs[id]))
	for _, cancel := range s.runs[id] {
		cancellations = append(cancellations, cancel)
	}
	s.runMu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
}

func (s *Service) registerRunContext(id uint, owner string, cancel context.CancelFunc) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runs[id] == nil {
		s.runs[id] = map[string]context.CancelFunc{}
	}
	if previous := s.runs[id][owner]; previous != nil {
		previous()
	}
	s.runs[id][owner] = cancel
}

func (s *Service) unregisterRunContext(id uint, owner string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	delete(s.runs[id], owner)
	if len(s.runs[id]) == 0 {
		delete(s.runs, id)
	}
}

func (s *Service) productionLeaseDuration() time.Duration {
	if s.leaseDuration > 0 {
		return s.leaseDuration
	}
	return productionLease
}

func (s *Service) startLeaseHeartbeat(ctx context.Context, cancel context.CancelFunc, id uint, owner string) <-chan error {
	result := make(chan error, 1)
	interval := s.productionLeaseDuration() / 3
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	go func() {
		defer close(result)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				result <- nil
				return
			case <-ticker.C:
				if err := s.renewLease(id, owner); err != nil {
					cancel()
					result <- err
					return
				}
			}
		}
	}()
	return result
}

func (s *Service) renewLease(id uint, owner string) error {
	expires := time.Now().UTC().Add(s.productionLeaseDuration()).Format(time.RFC3339)
	result := s.DB.Model(&models.ProductionRun{}).
		Where("id = ? AND status = ? AND lease_owner = ?", id, StatusQueued, owner).
		Updates(map[string]any{"lease_expires_at": expires, "updated_at": response.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTerminalRun
	}
	return nil
}

func (s *Service) agentStage(ctx context.Context, run *models.ProductionRun, agentType, instruction string) error {
	if s.Agents == nil {
		return errors.New("agent runner unavailable")
	}
	if err := s.recordAgentRun(ctx, run, agentType, instruction); err != nil {
		return err
	}
	next := map[string]string{StageScript: StageExtract, StageExtract: StageStoryboards, StageStoryboards: StageFrames}[run.Stage]
	if run.Stage == StageStoryboards {
		var count int64
		if err := s.DB.Model(&models.Storyboard{}).Where("organization_id = ? AND episode_id = ? AND deleted_at IS NULL", run.OrganizationID, run.EpisodeID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return errors.New("分镜 Agent 未生成任何分镜")
		}
	}
	return s.advance(run, next, map[string]int{StageExtract: 12, StageStoryboards: 25, StageFrames: 38}[next], "阶段已完成")
}

func (s *Service) recordAgentRun(ctx context.Context, run *models.ProductionRun, agentType, instruction string) error {
	now := response.Now()
	record := models.AgentRun{OrganizationID: run.OrganizationID, AgentType: agentType, DramaID: run.DramaID, EpisodeID: run.EpisodeID, Status: "running", Input: instruction, StartedAt: now, CreatedAt: now, UpdatedAt: now}
	startedPayload, _ := json.Marshal(map[string]any{"status": "running"})
	if err := retryProductionWrite(func() error {
		return s.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			return tx.Create(&models.AgentRunEvent{OrganizationID: run.OrganizationID, AgentRunID: record.ID, Sequence: 1, EventType: "started", PayloadJSON: string(startedPayload), CreatedAt: now}).Error
		})
	}); err != nil {
		return err
	}
	sequence := 1
	var eventErr error
	observer := func(event agents.RunEvent) {
		if eventErr != nil {
			return
		}
		payload, marshalErr := json.Marshal(event.Payload)
		if marshalErr != nil {
			eventErr = marshalErr
			return
		}
		sequence++
		eventErr = retryProductionWrite(func() error {
			return s.DB.Create(&models.AgentRunEvent{OrganizationID: run.OrganizationID, AgentRunID: record.ID, Sequence: sequence, EventType: event.EventType, ToolName: event.ToolName, PayloadJSON: string(payload), CreatedAt: response.Now()}).Error
		})
	}
	result, err := s.Agents.RunObserved(ctx, run.OrganizationID, agentType, run.DramaID, run.EpisodeID, instruction, observer)
	completed := response.Now()
	if eventErr != nil {
		persistErr := retryProductionWrite(func() error {
			return s.DB.Model(&record).Updates(map[string]any{"status": "failed", "last_error": "failed to persist agent events", "completed_at": completed, "updated_at": completed}).Error
		})
		return errors.Join(eventErr, persistErr)
	}
	if err != nil {
		terminalStatus := "failed"
		if errors.Is(err, context.Canceled) {
			terminalStatus = "canceled"
		}
		terminal, _ := json.Marshal(map[string]any{"status": terminalStatus, "error": err.Error()})
		persistErr := retryProductionWrite(func() error {
			return s.DB.Transaction(func(tx *gorm.DB) error {
				if updateErr := tx.Model(&record).Updates(map[string]any{"status": terminalStatus, "last_error": err.Error(), "completed_at": completed, "updated_at": completed}).Error; updateErr != nil {
					return updateErr
				}
				sequence++
				return tx.Create(&models.AgentRunEvent{OrganizationID: run.OrganizationID, AgentRunID: record.ID, Sequence: sequence, EventType: terminalStatus, PayloadJSON: string(terminal), CreatedAt: completed}).Error
			})
		})
		return errors.Join(err, persistErr)
	}
	payload, _ := json.Marshal(result)
	status := "completed"
	if result.Type == "failed" {
		status = "failed"
	}
	lastError := ""
	if status == "failed" {
		lastError = result.Text
	}
	terminal, _ := json.Marshal(map[string]any{"status": status, "error": lastError})
	if err := retryProductionWrite(func() error {
		return s.DB.Transaction(func(tx *gorm.DB) error {
			if updateErr := tx.Model(&record).Updates(map[string]any{"status": status, "output_json": string(payload), "last_error": lastError, "completed_at": completed, "updated_at": completed}).Error; updateErr != nil {
				return updateErr
			}
			sequence++
			return tx.Create(&models.AgentRunEvent{OrganizationID: run.OrganizationID, AgentRunID: record.ID, Sequence: sequence, EventType: status, PayloadJSON: string(terminal), CreatedAt: completed}).Error
		})
	}); err != nil {
		return err
	}
	if status == "failed" {
		return errors.New(result.Text)
	}
	return nil
}

func retryProductionWrite(operation func() error) error {
	var operationErr error
	for attempt := 0; attempt < 8; attempt++ {
		operationErr = operation()
		if operationErr == nil || !isProductionSQLiteBusy(operationErr) {
			return operationErr
		}
		time.Sleep(time.Duration(1<<attempt) * 5 * time.Millisecond)
	}
	return operationErr
}

func isProductionSQLiteBusy(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked")
}

func (s *Service) frameStage(ctx context.Context, run *models.ProductionRun) error {
	if err := s.childFailure(run.ID, []string{"image_generation"}); err != nil {
		return err
	}
	rows, err := s.storyboards(run)
	if err != nil {
		return err
	}
	episode, err := s.episode(run)
	if err != nil {
		return err
	}
	for _, sb := range rows {
		if sb.FirstFrameImage != "" {
			continue
		}
		active, err := s.hasActiveMediaJob(run.ID, sb.ID, "image_generation")
		if err != nil {
			return err
		}
		if active {
			continue
		}
		var drama models.Drama
		_ = s.DB.Where("organization_id = ? AND id = ?", run.OrganizationID, run.DramaID).First(&drama).Error
		characterNames, sceneNames := prompttemplate.ShotAssetNames(s.DB, run.OrganizationID, run.DramaID, &sb)
		resolution := prompttemplate.FramePrompt(s.DB, run.OrganizationID, drama, episode, sb, "first_frame", "", characterNames, sceneNames)
		prompt := strings.TrimSpace(resolution.Prompt)
		if prompt == "" {
			return fmt.Errorf("分镜 %d 缺少图片提示词", sb.ID)
		}
		id, dramaID := sb.ID, run.DramaID
		record := &models.ImageGeneration{OrganizationID: run.OrganizationID, StoryboardID: &id, DramaID: &dramaID, Prompt: prompt, FrameType: "first_frame", ImageType: "storyboard_frame", Status: "pending"}
		generateErr := s.Images.GenerateProduction(ctx, record, episode.ImageConfigID, run.ID)
		if generateErr != nil {
			return generateErr
		}
	}
	return s.waitOrAdvance(run, []string{"image_generation"}, StageVideos, 52, "首帧已完成")
}

func (s *Service) videoStage(ctx context.Context, run *models.ProductionRun) error {
	if err := s.childFailure(run.ID, []string{"video_generation"}); err != nil {
		return err
	}
	rows, err := s.storyboards(run)
	if err != nil {
		return err
	}
	episode, err := s.episode(run)
	if err != nil {
		return err
	}
	for _, sb := range rows {
		if sb.VideoURL != "" {
			continue
		}
		active, err := s.hasActiveMediaJob(run.ID, sb.ID, "video_generation")
		if err != nil {
			return err
		}
		if active {
			continue
		}
		var drama models.Drama
		_ = s.DB.Where("organization_id = ? AND id = ?", run.OrganizationID, run.DramaID).First(&drama).Error
		characterNames, sceneNames := prompttemplate.ShotAssetNames(s.DB, run.OrganizationID, run.DramaID, &sb)
		resolution := prompttemplate.VideoPrompt(s.DB, run.OrganizationID, drama, episode, sb, "", characterNames, sceneNames)
		prompt := strings.TrimSpace(resolution.Prompt)
		if prompt == "" || firstNonEmpty(sb.FirstFrameImage, sb.ComposedImage) == "" {
			return fmt.Errorf("分镜 %d 缺少视频提示词或首帧", sb.ID)
		}
		id, dramaID := sb.ID, run.DramaID
		record := &models.VideoGeneration{OrganizationID: run.OrganizationID, StoryboardID: &id, DramaID: &dramaID, Prompt: prompt, ImageURL: firstNonEmpty(sb.FirstFrameImage, sb.ComposedImage), FirstFrameURL: sb.FirstFrameImage, LastFrameURL: sb.LastFrameImage, ReferenceImageURLs: sb.ReferenceImages, Duration: sb.Duration}
		generateErr := s.Videos.GenerateProduction(ctx, record, episode.VideoConfigID, run.ID)
		if generateErr != nil {
			return generateErr
		}
	}
	return s.waitOrAdvance(run, []string{"video_generation"}, StageTTS, 66, "视频已完成")
}

func (s *Service) ttsStage(run *models.ProductionRun) error {
	if err := s.childFailure(run.ID, []string{"storyboard_tts"}); err != nil {
		return err
	}
	rows, err := s.storyboards(run)
	if err != nil {
		return err
	}
	var episode models.Episode
	if err := s.DB.Where("organization_id = ? AND id = ?", run.OrganizationID, run.EpisodeID).First(&episode).Error; err != nil {
		return err
	}
	for _, sb := range rows {
		if !generation.HasTTSContent(sb.Dialogue) || sb.TTSAudioURL != "" {
			continue
		}
		if _, err := s.Jobs.CreateQueuedOrganizationProduction(run.OrganizationID, "tts.generate", "storyboard_tts", sb.ID, "", episode.AudioConfigID, run.ID); err != nil {
			return err
		}
	}
	return s.waitOrAdvance(run, []string{"storyboard_tts"}, StageCompose, 76, "配音已完成")
}

func (s *Service) composeStage(run *models.ProductionRun) error {
	if err := s.childFailure(run.ID, []string{"episode_compose"}); err != nil {
		return err
	}
	rows, err := s.storyboards(run)
	if err != nil {
		return err
	}
	allComposed := true
	shots := make([]map[string]any, 0, len(rows))
	for _, sb := range rows {
		if sb.VideoURL == "" {
			return fmt.Errorf("分镜 %d 尚无视频", sb.ID)
		}
		if sb.ComposedVideoURL == "" {
			allComposed = false
		}
		shots = append(shots, map[string]any{"storyboard_id": sb.ID, "video_url": sb.VideoURL, "audio_url": sb.TTSAudioURL, "subtitle_url": sb.SubtitleURL, "output_rel": filepath.ToSlash(filepath.Join("composed", fmt.Sprintf("shot_%d.mp4", sb.ID)))})
	}
	if allComposed {
		return s.advance(run, StageMerge, 88, "镜头合成已完成")
	}
	payload, _ := json.Marshal(map[string]any{"episode_id": run.EpisodeID, "shots": shots})
	if _, err := s.Jobs.CreateQueuedPayloadOrganizationProduction(run.OrganizationID, "episode_compose", "episode_compose", run.EpisodeID, "ffmpeg", nil, string(payload), run.ID); err != nil {
		return err
	}
	return s.waitOrAdvance(run, []string{"episode_compose"}, StageMerge, 88, "镜头合成已完成")
}

func (s *Service) mergeStage(run *models.ProductionRun) error {
	if err := s.childFailure(run.ID, []string{"episode_merge"}); err != nil {
		return err
	}
	var episode models.Episode
	if err := s.DB.Where("organization_id = ? AND id = ?", run.OrganizationID, run.EpisodeID).First(&episode).Error; err != nil {
		return err
	}
	if episode.VideoURL != "" {
		return s.complete(run)
	}
	rows, err := s.storyboards(run)
	if err != nil {
		return err
	}
	inputs := make([]string, 0, len(rows))
	for _, sb := range rows {
		input := firstNonEmpty(sb.ComposedVideoURL, sb.VideoURL)
		if input == "" {
			return fmt.Errorf("分镜 %d 尚未合成", sb.ID)
		}
		inputs = append(inputs, input)
	}
	payload, _ := json.Marshal(map[string]any{"episode_id": run.EpisodeID, "inputs": inputs, "output_rel": filepath.ToSlash(filepath.Join("merged", "episode_"+strconv.FormatUint(uint64(run.EpisodeID), 10)+".mp4"))})
	if _, err := s.Jobs.CreateQueuedPayloadOrganizationProduction(run.OrganizationID, "episode_merge", "episode_merge", run.EpisodeID, "ffmpeg", nil, string(payload), run.ID); err != nil {
		return err
	}
	return s.deferRun(run, "等待整集导出")
}

func (s *Service) episode(run *models.ProductionRun) (models.Episode, error) {
	var episode models.Episode
	err := s.DB.Where("organization_id = ? AND id = ? AND drama_id = ? AND deleted_at IS NULL", run.OrganizationID, run.EpisodeID, run.DramaID).First(&episode).Error
	return episode, err
}

func (s *Service) hasActiveMediaJob(runID, storyboardID uint, targetType string) (bool, error) {
	table := "image_generations"
	if targetType == "video_generation" {
		table = "video_generations"
	}
	var count int64
	err := s.DB.Table("generation_jobs AS jobs").
		Joins("JOIN "+table+" AS media ON media.id = jobs.target_id").
		Where("jobs.production_run_id = ? AND jobs.target_type = ? AND jobs.status NOT IN ? AND media.storyboard_id = ?", runID, targetType, []string{jobs.StatusSucceeded, jobs.StatusFailed, jobs.StatusCanceled}, storyboardID).
		Count(&count).Error
	return count > 0, err
}

func (s *Service) storyboards(run *models.ProductionRun) ([]models.Storyboard, error) {
	var rows []models.Storyboard
	err := s.DB.Where("organization_id = ? AND episode_id = ? AND deleted_at IS NULL", run.OrganizationID, run.EpisodeID).Order("storyboard_number").Find(&rows).Error
	if err == nil && len(rows) == 0 {
		err = errors.New("本集没有分镜")
	}
	return rows, err
}

func (s *Service) waitOrAdvance(run *models.ProductionRun, targetTypes []string, next string, progress int, message string) error {
	if err := s.childFailure(run.ID, targetTypes); err != nil {
		return err
	}
	var active int64
	if err := s.DB.Model(&models.GenerationJob{}).Where("production_run_id = ? AND target_type IN ? AND status NOT IN ?", run.ID, targetTypes, []string{jobs.StatusSucceeded, jobs.StatusFailed, jobs.StatusCanceled}).Count(&active).Error; err != nil {
		return err
	}
	if active > 0 {
		return s.deferRun(run, "等待子任务完成")
	}
	if err := s.ensureStageArtifactsReady(run, next); err != nil {
		return err
	}
	return s.advance(run, next, progress, message)
}

// ensureStageArtifactsReady verifies media side effects landed before advancing.
// Job success alone is not enough if a storyboard still lacks the expected outputs.
func (s *Service) ensureStageArtifactsReady(run *models.ProductionRun, next string) error {
	switch next {
	case StageVideos:
		rows, err := s.storyboards(run)
		if err != nil {
			return err
		}
		for _, sb := range rows {
			if strings.TrimSpace(sb.FirstFrameImage) == "" {
				return fmt.Errorf("分镜 %d 首帧尚未就绪", sb.ID)
			}
		}
	case StageTTS:
		rows, err := s.storyboards(run)
		if err != nil {
			return err
		}
		for _, sb := range rows {
			if strings.TrimSpace(sb.VideoURL) == "" {
				return fmt.Errorf("分镜 %d 视频尚未就绪", sb.ID)
			}
		}
	case StageMerge:
		rows, err := s.storyboards(run)
		if err != nil {
			return err
		}
		for _, sb := range rows {
			if strings.TrimSpace(sb.ComposedVideoURL) == "" {
				return fmt.Errorf("分镜 %d 镜头合成尚未就绪", sb.ID)
			}
		}
	}
	return nil
}

func (s *Service) childFailure(runID uint, targetTypes []string) error {
	var child models.GenerationJob
	if err := s.DB.Where("production_run_id = ? AND target_type IN ? AND status IN ?", runID, targetTypes, []string{jobs.StatusFailed, jobs.StatusCanceled}).Order("id desc").First(&child).Error; err == nil {
		return fmt.Errorf("%s", firstNonEmpty(child.LastError, "子任务失败"))
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

func (s *Service) advance(run *models.ProductionRun, stage string, progress int, message string) error {
	now := response.Now()
	result := s.DB.Model(&models.ProductionRun{}).Where("id = ? AND status = ? AND lease_owner = ?", run.ID, StatusQueued, run.LeaseOwner).Updates(map[string]any{"stage": stage, "progress": progress, "status_message": message, "available_at": now, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTerminalRun
	}
	run.Stage, run.Progress = stage, progress
	return nil
}

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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
