package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/ffmpeg"
	"github.com/eqzhou/flyaimovie/internal/services/generation"
	"github.com/eqzhou/flyaimovie/internal/services/prompttemplate"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Server) registerPipelineExtras(api *gin.RouterGroup) {
	// storyboard frame / batch ops
	api.POST("/storyboards/:id/generate-frame", s.storyboardGenerateFrame)
	api.POST("/storyboards/batch-generate-frames", s.batchGenerateFrames)
	api.POST("/storyboards/batch-generate-videos", s.batchGenerateVideos)
	api.POST("/storyboards/batch-generate-tts", s.batchGenerateTTS)
	api.POST("/storyboards/:id/generate-video", s.storyboardGenerateVideo)

	// list videos
	api.GET("/videos", s.listVideos)

	// grid history
	api.GET("/grid/history", s.gridHistory)
	api.GET("/grid/history/:id", s.gridHistoryGet)

}

func (s *Server) registerWebhooks(api *gin.RouterGroup) {
	api.POST("/webhooks/vidu", s.viduWebhook)
	api.POST("/webhooks/generic", s.genericWebhook)
}

func (s *Server) storyboardGenerateFrame(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var body struct {
		FrameType string `json:"frame_type"` // first_frame|last_frame|composed
		Prompt    string `json:"prompt"`
		ConfigID  *uint  `json:"config_id"`
		EpisodeID *uint  `json:"episode_id"`
	}
	if err := bindOptionalJSON(c, &body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	frameType, err := normalizeStoryboardFrameType(body.FrameType)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	body.FrameType = frameType
	var sb models.Storyboard
	if err := findActiveStoryboard(c, uint(id), &sb); err != nil {
		response.NotFound(c, "storyboard not found")
		return
	}
	if body.EpisodeID != nil && *body.EpisodeID != sb.EpisodeID {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "storyboard does not belong to episode"})
		return
	}
	if body.ConfigID != nil {
		if err := validateAIConfigReferenceFor(c, *body.ConfigID, "image"); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	epID := sb.EpisodeID
	if body.EpisodeID != nil {
		epID = *body.EpisodeID
	}
	var ep models.Episode
	if err := findActiveEpisode(c, epID, &ep); err != nil {
		response.NotFound(c, "episode not found")
		return
	}
	var drama models.Drama
	_ = findActiveDrama(c, ep.DramaID, &drama)
	characterNames, sceneNames := promptAssetNames(c, ep.DramaID, &sb)
	resolution := prompttemplate.FramePrompt(organizationDB(c), currentOrganizationID(c), drama, ep, sb, body.FrameType, body.Prompt, characterNames, sceneNames)
	prompt := strings.TrimSpace(resolution.Prompt)
	if prompt == "" {
		response.BadRequest(c, "prompt empty")
		return
	}
	var configID *uint = body.ConfigID
	if configID == nil {
		configID = ep.ImageConfigID
	}
	sid := sb.ID
	did := ep.DramaID
	rec := &models.ImageGeneration{
		OrganizationID: currentOrganizationID(c),
		StoryboardID:   &sid, DramaID: &did, Prompt: prompt,
		FrameType: body.FrameType, ImageType: "storyboard_frame", Status: "pending",
	}
	// reference: character/scene images if available
	refs := []string{}
	if sb.SceneID != nil {
		var sc models.Scene
		if err := findActiveScene(c, *sb.SceneID, &sc); err == nil && sc.ImageURL != "" {
			refs = append(refs, sc.ImageURL)
		}
	}
	var links []models.StoryboardCharacter
	organizationDB(c).Where("storyboard_id = ?", sb.ID).Find(&links)
	for _, l := range links {
		var ch models.Character
		if err := findActiveCharacter(c, l.CharacterID, &ch); err == nil && ch.ImageURL != "" {
			refs = append(refs, ch.ImageURL)
		}
	}
	if len(refs) > 0 {
		rec.ReferenceImages = strings.Join(refs, ",")
	}
	if err := s.Images.Generate(c.Request.Context(), rec, configID); err != nil {
		respondGenerationError(c, err)
		return
	}
	response.Success(c, rec)
}

func (s *Server) batchGenerateFrames(c *gin.Context) {
	var body struct {
		StoryboardIDs []uint `json:"storyboard_ids"`
		EpisodeID     uint   `json:"episode_id"`
		FrameType     string `json:"frame_type"`
		ConfigID      *uint  `json:"config_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	if len(body.StoryboardIDs) == 0 && body.EpisodeID == 0 {
		response.BadRequest(c, "storyboard_ids or episode_id is required")
		return
	}
	frameType, err := normalizeStoryboardFrameType(body.FrameType)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	body.FrameType = frameType
	ids := body.StoryboardIDs
	if len(ids) == 0 && body.EpisodeID > 0 {
		var rows []models.Storyboard
		organizationDB(c).Where("episode_id = ? AND deleted_at IS NULL", body.EpisodeID).Order("storyboard_number").Find(&rows)
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
	}
	if !ensureBatchStoryboardEpisode(c, ids, body.EpisodeID) {
		return
	}
	if body.ConfigID != nil {
		if err := validateAIConfigReferenceFor(c, *body.ConfigID, "image"); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	out := make([]uint, 0)
	errs := make([]string, 0)
	for _, id := range ids {
		var sb models.Storyboard
		if err := findActiveStoryboard(c, id, &sb); err != nil {
			errs = append(errs, fmt.Sprintf("sb %d: not found", id))
			continue
		}
		var ep models.Episode
		if err := findActiveEpisode(c, sb.EpisodeID, &ep); err != nil {
			errs = append(errs, fmt.Sprintf("sb %d: episode not found", id))
			continue
		}
		var drama models.Drama
		_ = findActiveDrama(c, ep.DramaID, &drama)
		characterNames, sceneNames := promptAssetNames(c, ep.DramaID, &sb)
		resolution := prompttemplate.FramePrompt(organizationDB(c), currentOrganizationID(c), drama, ep, sb, body.FrameType, "", characterNames, sceneNames)
		prompt := strings.TrimSpace(resolution.Prompt)
		if prompt == "" {
			errs = append(errs, fmt.Sprintf("sb %d: empty prompt", id))
			continue
		}
		configID := body.ConfigID
		if configID == nil {
			configID = ep.ImageConfigID
		}
		sid := sb.ID
		did := ep.DramaID
		rec := &models.ImageGeneration{
			OrganizationID: currentOrganizationID(c),
			StoryboardID:   &sid, DramaID: &did, Prompt: prompt,
			FrameType: body.FrameType, ImageType: "storyboard_frame",
		}
		if err := s.Images.Generate(c.Request.Context(), rec, configID); err == nil {
			out = append(out, rec.ID)
		} else {
			errs = append(errs, fmt.Sprintf("sb %d: %s", id, err.Error()))
		}
	}
	response.Success(c, gin.H{"count": len(out), "ids": out, "errors": errs})
}

func normalizeStoryboardFrameType(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "first_frame":
		return "first_frame", nil
	case "last_frame":
		return "last_frame", nil
	case "composed", "storyboard":
		return "composed", nil
	default:
		return "", fmt.Errorf("frame_type must be first_frame, last_frame or composed")
	}
}

func ensureBatchStoryboardEpisode(c *gin.Context, ids []uint, episodeID uint) bool {
	if episodeID == 0 || len(ids) == 0 {
		return true
	}
	var active int64
	if err := organizationDB(c).Model(&models.Storyboard{}).
		Where("id IN ? AND deleted_at IS NULL AND episode_id = ?", ids, episodeID).
		Count(&active).Error; err != nil {
		response.ServerError(c, "failed to validate storyboard ownership")
		return false
	}
	if int(active) != len(uniqueUints(ids)) {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "storyboard does not belong to episode"})
		return false
	}
	return true
}

func uniqueUints(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (s *Server) batchGenerateVideos(c *gin.Context) {
	var body struct {
		StoryboardIDs []uint `json:"storyboard_ids"`
		EpisodeID     uint   `json:"episode_id"`
		ConfigID      *uint  `json:"config_id"`
		ReferenceMode string `json:"reference_mode"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if len(body.StoryboardIDs) == 0 && body.EpisodeID == 0 {
		response.BadRequest(c, "storyboard_ids or episode_id is required")
		return
	}
	if body.ConfigID != nil {
		if err := validateAIConfigReferenceFor(c, *body.ConfigID, "video"); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	ids := body.StoryboardIDs
	if len(ids) == 0 && body.EpisodeID > 0 {
		var rows []models.Storyboard
		organizationDB(c).Where("episode_id = ? AND deleted_at IS NULL", body.EpisodeID).Order("storyboard_number").Find(&rows)
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
	}
	if !ensureBatchStoryboardEpisode(c, ids, body.EpisodeID) {
		return
	}
	out := make([]uint, 0)
	errs := make([]string, 0)
	for _, id := range ids {
		var sb models.Storyboard
		if err := findActiveStoryboard(c, id, &sb); err != nil {
			errs = append(errs, fmt.Sprintf("sb %d: not found", id))
			continue
		}
		var ep models.Episode
		if err := findActiveEpisode(c, sb.EpisodeID, &ep); err != nil {
			errs = append(errs, fmt.Sprintf("sb %d: episode not found", id))
			continue
		}
		var drama models.Drama
		_ = findActiveDrama(c, ep.DramaID, &drama)
		characterNames, sceneNames := promptAssetNames(c, ep.DramaID, &sb)
		resolution := prompttemplate.VideoPrompt(organizationDB(c), currentOrganizationID(c), drama, ep, sb, "", characterNames, sceneNames)
		prompt := strings.TrimSpace(resolution.Prompt)
		if prompt == "" {
			errs = append(errs, fmt.Sprintf("sb %d: empty prompt", id))
			continue
		}
		if firstNonEmpty(sb.FirstFrameImage, sb.ComposedImage) == "" {
			errs = append(errs, fmt.Sprintf("sb %d: missing first frame", id))
			continue
		}
		sid := sb.ID
		did := ep.DramaID
		rec := &models.VideoGeneration{
			OrganizationID: currentOrganizationID(c),
			StoryboardID:   &sid, DramaID: &did, Prompt: prompt,
			ImageURL:      firstNonEmpty(sb.FirstFrameImage, sb.ComposedImage),
			FirstFrameURL: sb.FirstFrameImage, LastFrameURL: sb.LastFrameImage,
			ReferenceMode: body.ReferenceMode, ReferenceImageURLs: sb.ReferenceImages, Duration: sb.Duration,
		}
		configID := body.ConfigID
		if configID == nil {
			configID = ep.VideoConfigID
		}
		if err := s.Videos.Generate(c.Request.Context(), rec, configID); err != nil {
			errs = append(errs, fmt.Sprintf("sb %d: %s", id, err.Error()))
			continue
		}
		out = append(out, rec.ID)
	}
	response.Success(c, gin.H{"count": len(out), "ids": out, "errors": errs})
}

func (s *Server) batchGenerateTTS(c *gin.Context) {
	var body struct {
		StoryboardIDs []uint `json:"storyboard_ids"`
		EpisodeID     uint   `json:"episode_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if len(body.StoryboardIDs) == 0 && body.EpisodeID == 0 {
		response.BadRequest(c, "storyboard_ids or episode_id is required")
		return
	}
	ids := body.StoryboardIDs
	if len(ids) == 0 && body.EpisodeID > 0 {
		var rows []models.Storyboard
		organizationDB(c).Where("episode_id = ? AND deleted_at IS NULL", body.EpisodeID).Order("storyboard_number").Find(&rows)
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
	}
	if !ensureBatchStoryboardEpisode(c, ids, body.EpisodeID) {
		return
	}
	ok, skipped, fail := 0, 0, 0
	for _, id := range ids {
		var sb models.Storyboard
		if err := findActiveStoryboard(c, id, &sb); err != nil {
			fail++
			continue
		}
		if !generation.HasTTSContent(sb.Dialogue) {
			skipped++
			continue
		}
		var ep models.Episode
		_ = findActiveEpisode(c, sb.EpisodeID, &ep)
		job, err := s.Jobs.CreateQueuedOrganization(currentOrganizationID(c), "tts.generate", "storyboard_tts", id, "", ep.AudioConfigID)
		if err != nil {
			fail++
		} else {
			_ = job
			ok++
		}
	}
	response.Success(c, gin.H{"ok": ok, "skipped": skipped, "fail": fail})
}

func (s *Server) storyboardGenerateVideo(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var body struct {
		Prompt        string `json:"prompt"`
		ReferenceMode string `json:"reference_mode"`
		ConfigID      *uint  `json:"config_id"`
	}
	if err := bindOptionalJSON(c, &body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if body.ConfigID != nil {
		if err := validateAIConfigReferenceFor(c, *body.ConfigID, "video"); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	var sb models.Storyboard
	if err := findActiveStoryboard(c, uint(id), &sb); err != nil {
		response.NotFound(c, "not found")
		return
	}
	var ep models.Episode
	if err := findActiveEpisode(c, sb.EpisodeID, &ep); err != nil {
		response.NotFound(c, "episode not found")
		return
	}
	var drama models.Drama
	_ = findActiveDrama(c, ep.DramaID, &drama)
	characterNames, sceneNames := promptAssetNames(c, ep.DramaID, &sb)
	resolution := prompttemplate.VideoPrompt(organizationDB(c), currentOrganizationID(c), drama, ep, sb, body.Prompt, characterNames, sceneNames)
	prompt := strings.TrimSpace(resolution.Prompt)
	if prompt == "" {
		response.BadRequest(c, "prompt empty")
		return
	}
	sid := sb.ID
	did := ep.DramaID
	rec := &models.VideoGeneration{
		OrganizationID: currentOrganizationID(c),
		StoryboardID:   &sid, DramaID: &did, Prompt: prompt,
		ImageURL:      firstNonEmpty(sb.FirstFrameImage, sb.ComposedImage),
		FirstFrameURL: sb.FirstFrameImage, LastFrameURL: sb.LastFrameImage,
		ReferenceMode: body.ReferenceMode, ReferenceImageURLs: sb.ReferenceImages, Duration: sb.Duration,
	}
	cfg := body.ConfigID
	if cfg == nil {
		cfg = ep.VideoConfigID
	}
	if err := s.Videos.Generate(c.Request.Context(), rec, cfg); err != nil {
		respondGenerationError(c, err)
		return
	}
	response.Success(c, rec)
}

func (s *Server) listVideos(c *gin.Context) {
	q := organizationDB(c).Model(&models.VideoGeneration{}).Order("id desc")
	if v := c.Query("drama_id"); v != "" {
		q = q.Where("drama_id = ?", v)
	}
	if v := c.Query("storyboard_id"); v != "" {
		q = q.Where("storyboard_id = ?", v)
	}
	if v := c.Query("status"); v != "" {
		q = q.Where("status = ?", v)
	}
	var rows []models.VideoGeneration
	q.Limit(100).Find(&rows)
	response.Success(c, rows)
}

func (s *Server) gridHistory(c *gin.Context) {
	q := organizationDB(c).Model(&models.GridHistory{}).Order("id desc")
	if v := c.Query("episode_id"); v != "" {
		q = q.Where("episode_id = ?", v)
	}
	if v := c.Query("drama_id"); v != "" {
		q = q.Where("drama_id = ?", v)
	}
	var rows []models.GridHistory
	q.Limit(50).Find(&rows)
	response.Success(c, rows)
}

func (s *Server) gridHistoryGet(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var row models.GridHistory
	if err := organizationDB(c).First(&row, id).Error; err != nil {
		response.NotFound(c, "not found")
		return
	}
	response.Success(c, row)
}

func (s *Server) viduWebhook(c *gin.Context) {
	raw, duplicate, ok := s.verifyWebhook(c)
	if !ok {
		return
	}
	if duplicate {
		response.Success(c, gin.H{"duplicate": true})
		return
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(c, "invalid webhook body")
		return
	}
	taskID := asString(body["task_id"])
	if taskID == "" {
		taskID = asString(body["id"])
	}
	if taskID == "" {
		if d, ok := body["data"].(map[string]any); ok {
			taskID = asString(d["task_id"])
			if taskID == "" {
				taskID = asString(d["id"])
			}
		}
	}
	status := strings.ToLower(asString(body["state"]))
	if status == "" {
		status = strings.ToLower(asString(body["status"]))
	}
	videoURL := asString(body["video_url"])
	if videoURL == "" {
		if creations, ok := body["creations"].([]any); ok && len(creations) > 0 {
			if m, ok := creations[0].(map[string]any); ok {
				videoURL = asString(m["url"])
			}
		}
	}
	if taskID == "" {
		response.BadRequest(c, "task_id required")
		return
	}
	var matches []models.VideoGeneration
	if err := db.DB.Where("task_id = ?", taskID).Limit(2).Find(&matches).Error; err != nil || len(matches) != 1 {
		response.Success(c, gin.H{"ignored": true})
		return
	}
	rec := matches[0]
	switch status {
	case "success", "succeeded", "completed", "done":
		if videoURL != "" {
			_ = s.Videos.Finalize(c.Request.Context(), &rec, videoURL)
		}
	case "failed", "error":
		rec.Status = "failed"
		rec.ErrorMsg = asString(body["err_code"]) + " " + asString(body["err_msg"])
		rec.UpdatedAt = response.Now()
		db.DB.Save(&rec)
	default:
		rec.Status = "processing"
		rec.UpdatedAt = response.Now()
		db.DB.Save(&rec)
	}
	response.Success(c, gin.H{"ok": true, "task_id": taskID})
}

func (s *Server) genericWebhook(c *gin.Context) {
	raw, duplicate, ok := s.verifyWebhook(c)
	if !ok {
		return
	}
	if duplicate {
		response.Success(c, gin.H{"duplicate": true})
		return
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(c, "invalid webhook body")
		return
	}
	taskID := asString(body["task_id"])
	if taskID == "" {
		taskID = asString(body["id"])
	}
	kind := asString(body["type"])
	status := strings.ToLower(asString(body["status"]))
	url := asString(body["url"])
	if url == "" {
		url = asString(body["video_url"])
	}
	if url == "" {
		url = asString(body["image_url"])
	}
	if taskID == "" {
		response.BadRequest(c, "task_id required")
		return
	}
	if kind == "" {
		var imageCount, videoCount int64
		db.DB.Model(&models.ImageGeneration{}).Where("task_id = ?", taskID).Count(&imageCount)
		db.DB.Model(&models.VideoGeneration{}).Where("task_id = ?", taskID).Count(&videoCount)
		if imageCount+videoCount != 1 {
			response.Success(c, gin.H{"ignored": true, "reason": "ambiguous task"})
			return
		}
		if imageCount == 1 {
			kind = "image"
		} else {
			kind = "video"
		}
	}
	if kind == "image" || kind == "" {
		var matches []models.ImageGeneration
		if err := db.DB.Where("task_id = ?", taskID).Limit(2).Find(&matches).Error; err == nil && len(matches) == 1 {
			rec := matches[0]
			if status == "completed" || status == "success" {
				if url != "" {
					_ = s.Images.Finalize(c.Request.Context(), &rec, url)
				}
			} else if status == "failed" {
				rec.Status = "failed"
				rec.ErrorMsg = asString(body["error"])
				rec.UpdatedAt = response.Now()
				db.DB.Save(&rec)
			}
		}
	}
	if kind == "video" || kind == "" {
		var matches []models.VideoGeneration
		if err := db.DB.Where("task_id = ?", taskID).Limit(2).Find(&matches).Error; err == nil && len(matches) == 1 {
			rec := matches[0]
			if (status == "completed" || status == "success") && url != "" {
				_ = s.Videos.Finalize(c.Request.Context(), &rec, url)
			} else if status == "failed" {
				rec.Status = "failed"
				rec.ErrorMsg = asString(body["error"])
				rec.UpdatedAt = response.Now()
				db.DB.Save(&rec)
			}
		}
	}
	response.Success(c, gin.H{"ok": true})
}

// Enhance grid endpoints: prompt with agent + history persistence helpers used from media.go overrides.
func (s *Server) buildGridPrompt(c *gin.Context, organizationID uint, drama models.Drama, episode models.Episode, mode string, rows, cols int, shots []models.Storyboard) (string, []string) {
	characterNames, sceneNames := promptAssetNames(c, drama.ID, nil)
	resolution, cells := prompttemplate.GridPrompt(organizationDB(c), organizationID, drama, episode, mode, rows, cols, shots, characterNames, sceneNames)
	return resolution.Prompt, cells
}

func promptAssetNames(c *gin.Context, dramaID uint, storyboard *models.Storyboard) ([]string, []string) {
	return prompttemplate.ShotAssetNames(organizationDB(c), currentOrganizationID(c), dramaID, storyboard)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// re-export helpers for split assignment of last frames
var errGridHistoryAlreadySplit = errors.New("grid history is already split")

func (s *Server) assignGridCells(c *gin.Context, ids []uint, urls []string, frameType string) error {
	return s.assignGridCellsWithHistory(c, ids, urls, frameType, nil, nil)
}

func (s *Server) assignGridCellsWithHistory(c *gin.Context, ids []uint, urls []string, frameType string, historyID *uint, historyUpdates map[string]any) error {
	if frameType == "" {
		frameType = "first_frame"
	}
	if frameType == "first_last" && (len(ids)%2 != 0 || len(urls) < len(ids)) {
		return fmt.Errorf("invalid first_last grid assignment")
	}
	return organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var lockedHistory models.GridHistory
		if historyID != nil {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("organization_id = ? AND id = ?", currentOrganizationID(c), *historyID).
				First(&lockedHistory).Error; err != nil {
				return err
			}
			if historyUpdates != nil && (lockedHistory.Status == "split" || strings.TrimSpace(lockedHistory.CellsJSON) != "") {
				return errGridHistoryAlreadySplit
			}
		}
		for i, url := range urls {
			if i >= len(ids) {
				break
			}
			targetIndex := i
			if frameType == "first_last" {
				half := len(ids) / 2
				targetIndex = i % half
			}
			updates := map[string]any{"updated_at": response.Now()}
			switch frameType {
			case "first_last":
				if i < len(urls)/2 {
					updates["first_frame_image"] = url
				} else {
					updates["last_frame_image"] = url
				}
			case "last_frame":
				updates["last_frame_image"] = url
			case "composed", "storyboard":
				updates["composed_image"] = url
			default:
				updates["first_frame_image"] = url
			}
			result := tx.Model(&models.Storyboard{}).Where("organization_id = ? AND id = ? AND deleted_at IS NULL", currentOrganizationID(c), ids[targetIndex]).Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("storyboard %d not found", ids[targetIndex])
			}
		}
		if historyID != nil {
			assets := make([]models.Asset, 0, len(urls))
			createdAt := response.Now()
			for index, url := range urls {
				assets = append(assets, models.Asset{
					OrganizationID: currentOrganizationID(c), DramaID: lockedHistory.DramaID, EpisodeID: lockedHistory.EpisodeID,
					Name: fmt.Sprintf("宫格切片 #%d", index+1), Type: "image", Category: "grid_cell",
					URL: url, LocalPath: strings.TrimPrefix(url, "/static/"), MimeType: "image/png", ProbeStatus: "completed",
					GridHistoryID: historyID, CreatedAt: createdAt, UpdatedAt: createdAt,
				})
			}
			if len(assets) > 0 {
				if err := tx.Create(&assets).Error; err != nil {
					return err
				}
			}
			result := tx.Model(&models.GridHistory{}).
				Where("organization_id = ? AND id = ?", currentOrganizationID(c), *historyID).
				Updates(historyUpdates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("grid history %d not found", *historyID)
			}
		}
		return nil
	})
}

// ensure ffmpeg helpers still reachable
var _ = ffmpeg.SplitGrid
var _ = generation.EnsureLocalFile
var _ = filepath.Join
