package httpapi

import (
	"encoding/json"
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
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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
	if body.FrameType == "" {
		body.FrameType = "first_frame"
	}
	var sb models.Storyboard
	if err := organizationDB(c).First(&sb, id).Error; err != nil {
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
	prompt := body.Prompt
	if prompt == "" {
		prompt = firstNonEmpty(sb.ImagePrompt, sb.Description, sb.Action, sb.Title)
	}
	if prompt == "" {
		response.BadRequest(c, "prompt empty")
		return
	}
	// enrich prompt by frame type
	switch body.FrameType {
	case "last_frame":
		prompt = "ending frame, " + prompt
	case "composed", "storyboard":
		prompt = "storyboard panel composition, " + prompt
	default:
		prompt = "opening frame, " + prompt
	}
	var configID *uint = body.ConfigID
	epID := sb.EpisodeID
	if body.EpisodeID != nil {
		epID = *body.EpisodeID
	}
	if configID == nil {
		var ep models.Episode
		if err := organizationDB(c).First(&ep, epID).Error; err == nil {
			configID = ep.ImageConfigID
		}
	}
	sid := sb.ID
	// load drama via episode
	var ep models.Episode
	organizationDB(c).First(&ep, epID)
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
		if err := organizationDB(c).First(&sc, *sb.SceneID).Error; err == nil && sc.ImageURL != "" {
			refs = append(refs, sc.ImageURL)
		}
	}
	var links []models.StoryboardCharacter
	organizationDB(c).Where("storyboard_id = ?", sb.ID).Find(&links)
	for _, l := range links {
		var ch models.Character
		if err := organizationDB(c).First(&ch, l.CharacterID).Error; err == nil && ch.ImageURL != "" {
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
	if body.FrameType == "" {
		body.FrameType = "first_frame"
	}
	ids := body.StoryboardIDs
	if len(ids) == 0 && body.EpisodeID > 0 {
		var rows []models.Storyboard
		organizationDB(c).Where("episode_id = ? AND deleted_at IS NULL", body.EpisodeID).Order("storyboard_number").Find(&rows)
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
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
		if err := organizationDB(c).First(&sb, id).Error; err != nil {
			errs = append(errs, fmt.Sprintf("sb %d: not found", id))
			continue
		}
		prompt := firstNonEmpty(sb.ImagePrompt, sb.Description, sb.Action, sb.Title)
		if prompt == "" {
			errs = append(errs, fmt.Sprintf("sb %d: empty prompt", id))
			continue
		}
		switch body.FrameType {
		case "last_frame":
			prompt = "ending frame, " + prompt
		case "composed", "storyboard":
			prompt = "storyboard panel composition, " + prompt
		default:
			prompt = "opening frame, " + prompt
		}
		var ep models.Episode
		organizationDB(c).First(&ep, sb.EpisodeID)
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
	out := make([]uint, 0)
	for _, id := range ids {
		var sb models.Storyboard
		if err := organizationDB(c).First(&sb, id).Error; err != nil {
			continue
		}
		var ep models.Episode
		organizationDB(c).First(&ep, sb.EpisodeID)
		prompt := firstNonEmpty(sb.VideoPrompt, sb.ImagePrompt, sb.Description)
		if prompt == "" {
			continue
		}
		sid := sb.ID
		did := ep.DramaID
		rec := &models.VideoGeneration{
			OrganizationID: currentOrganizationID(c),
			StoryboardID:   &sid, DramaID: &did, Prompt: prompt,
			ImageURL:      firstNonEmpty(sb.FirstFrameImage, sb.ComposedImage),
			FirstFrameURL: sb.FirstFrameImage, LastFrameURL: sb.LastFrameImage,
			ReferenceMode: body.ReferenceMode, Duration: sb.Duration,
		}
		if err := s.Videos.Generate(c.Request.Context(), rec, body.ConfigID); err == nil {
			out = append(out, rec.ID)
		} else if body.ConfigID == nil {
			if err2 := s.Videos.Generate(c.Request.Context(), rec, ep.VideoConfigID); err2 == nil {
				out = append(out, rec.ID)
			}
		}
	}
	response.Success(c, gin.H{"count": len(out), "ids": out})
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
	ok, fail := 0, 0
	for _, id := range ids {
		var sb models.Storyboard
		if err := organizationDB(c).First(&sb, id).Error; err != nil {
			fail++
			continue
		}
		var ep models.Episode
		organizationDB(c).First(&ep, sb.EpisodeID)
		job, err := s.Jobs.CreateQueuedOrganization(currentOrganizationID(c), "tts.generate", "storyboard_tts", id, "", ep.AudioConfigID)
		if err != nil {
			fail++
		} else {
			_ = job
			ok++
		}
	}
	response.Success(c, gin.H{"ok": ok, "fail": fail})
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
	if err := organizationDB(c).First(&sb, id).Error; err != nil {
		response.NotFound(c, "not found")
		return
	}
	var ep models.Episode
	organizationDB(c).First(&ep, sb.EpisodeID)
	prompt := firstNonEmpty(body.Prompt, sb.VideoPrompt, sb.ImagePrompt, sb.Description)
	sid := sb.ID
	did := ep.DramaID
	rec := &models.VideoGeneration{
		OrganizationID: currentOrganizationID(c),
		StoryboardID:   &sid, DramaID: &did, Prompt: prompt,
		ImageURL:      firstNonEmpty(sb.FirstFrameImage, sb.ComposedImage),
		FirstFrameURL: sb.FirstFrameImage, LastFrameURL: sb.LastFrameImage,
		ReferenceMode: body.ReferenceMode, Duration: sb.Duration,
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
func (s *Server) buildGridPrompt(mode string, rows, cols int, shots []models.Storyboard) (string, []string) {
	if rows <= 0 {
		rows = 2
	}
	if cols <= 0 {
		cols = 2
	}
	cells := make([]string, 0, rows*cols)
	for i := 0; i < rows*cols; i++ {
		if i < len(shots) {
			sb := shots[i]
			p := firstNonEmpty(sb.ImagePrompt, sb.Description, sb.Action, sb.Title)
			if p == "" {
				p = "cinematic shot"
			}
			cells = append(cells, p)
		} else {
			cells = append(cells, "empty cinematic panel, dark, no text")
		}
	}
	var b strings.Builder
	b.WriteString("Create a seamless ")
	b.WriteString(strconv.Itoa(rows))
	b.WriteString("x")
	b.WriteString(strconv.Itoa(cols))
	b.WriteString(" storyboard grid with exactly ")
	b.WriteString(strconv.Itoa(rows * cols))
	b.WriteString(" equal panels, consistent art style, cinematic lighting, high detail, no text, no watermark, no borders labels.\n")
	if mode == "first_last" {
		b.WriteString("For each shot, create two panels in order: opening frame first, ending frame second. Keep the same characters and composition across the pair.\n")
	} else if mode == "multi_ref" {
		b.WriteString("Maintain character identity consistency across panels.\n")
	} else {
		b.WriteString("Each panel is a first-frame composition.\n")
	}
	for i, cell := range cells {
		b.WriteString("Panel ")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(": ")
		b.WriteString(cell)
		b.WriteString("\n")
	}
	return b.String(), cells
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// re-export helpers for split assignment of last frames
func (s *Server) assignGridCells(c *gin.Context, ids []uint, urls []string, frameType string) error {
	if frameType == "" {
		frameType = "first_frame"
	}
	if frameType == "first_last" && (len(ids)%2 != 0 || len(urls) < len(ids)) {
		return fmt.Errorf("invalid first_last grid assignment")
	}
	return organizationDB(c).Transaction(func(tx *gorm.DB) error {
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
		return nil
	})
}

// ensure ffmpeg helpers still reachable
var _ = ffmpeg.SplitGrid
var _ = generation.EnsureLocalFile
var _ = filepath.Join
