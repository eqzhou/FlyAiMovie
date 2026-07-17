package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/ffmpeg"
	"github.com/eqzhou/flyaimovie/internal/services/generation"
	"github.com/eqzhou/flyaimovie/internal/services/mediafetch"
	"github.com/gin-gonic/gin"
)

func (s *Server) registerImages(api *gin.RouterGroup) {
	g := api.Group("/images")
	g.POST("", s.createImage)
	g.GET("", s.listImages)
	g.GET("/:id", s.getImage)
}

func (s *Server) createImage(c *gin.Context) {
	var body struct {
		StoryboardID *uint  `json:"storyboard_id"`
		DramaID      *uint  `json:"drama_id"`
		SceneID      *uint  `json:"scene_id"`
		CharacterID  *uint  `json:"character_id"`
		Prompt       string `json:"prompt"`
		FrameType    string `json:"frame_type"`
		ImageType    string `json:"image_type"`
		Size         string `json:"size"`
		ConfigID     *uint  `json:"config_id"`
		EpisodeID    *uint  `json:"episode_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Prompt == "" {
		response.BadRequest(c, "prompt required")
		return
	}
	if err := validateGenerationOwnership(c, body.StoryboardID, body.DramaID, body.SceneID, body.CharacterID, body.EpisodeID); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	var configID *uint = body.ConfigID
	if body.ConfigID != nil {
		if err := validateAIConfigReferenceFor(c, *body.ConfigID, "image"); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	if configID == nil && body.EpisodeID != nil {
		var ep models.Episode
		if err := organizationDB(c).First(&ep, *body.EpisodeID).Error; err == nil {
			configID = ep.ImageConfigID
		}
	}
	rec := &models.ImageGeneration{
		OrganizationID: currentOrganizationID(c),
		StoryboardID:   body.StoryboardID, DramaID: body.DramaID, SceneID: body.SceneID,
		CharacterID: body.CharacterID, Prompt: body.Prompt, FrameType: body.FrameType,
		ImageType: body.ImageType, Size: body.Size,
	}
	if err := s.Images.Generate(c.Request.Context(), rec, configID); err != nil {
		respondGenerationError(c, err)
		return
	}
	response.Success(c, rec)
}

func (s *Server) listImages(c *gin.Context) {
	q := organizationDB(c).Model(&models.ImageGeneration{}).Order("id desc")
	if v := c.Query("drama_id"); v != "" {
		q = q.Where("drama_id = ?", v)
	}
	if v := c.Query("storyboard_id"); v != "" {
		q = q.Where("storyboard_id = ?", v)
	}
	var rows []models.ImageGeneration
	q.Limit(100).Find(&rows)
	response.Success(c, rows)
}

func (s *Server) getImage(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var rec models.ImageGeneration
	if err := organizationDB(c).First(&rec, id).Error; err != nil {
		response.NotFound(c, "not found")
		return
	}
	response.Success(c, rec)
}

func (s *Server) registerVideos(api *gin.RouterGroup) {
	g := api.Group("/videos")
	g.POST("", s.createVideo)
	g.GET("/:id", s.getVideo)
}

func (s *Server) createVideo(c *gin.Context) {
	var body struct {
		StoryboardID  *uint  `json:"storyboard_id"`
		DramaID       *uint  `json:"drama_id"`
		Prompt        string `json:"prompt"`
		ImageURL      string `json:"image_url"`
		FirstFrameURL string `json:"first_frame_url"`
		LastFrameURL  string `json:"last_frame_url"`
		ReferenceMode string `json:"reference_mode"`
		Duration      int    `json:"duration"`
		AspectRatio   string `json:"aspect_ratio"`
		ConfigID      *uint  `json:"config_id"`
		EpisodeID     *uint  `json:"episode_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	// auto-fill from storyboard
	if body.StoryboardID != nil {
		var sb models.Storyboard
		if err := organizationDB(c).First(&sb, *body.StoryboardID).Error; err == nil {
			if body.Prompt == "" {
				body.Prompt = firstNonEmpty(sb.VideoPrompt, sb.ImagePrompt, sb.Description)
			}
			if body.FirstFrameURL == "" {
				body.FirstFrameURL = sb.FirstFrameImage
			}
			if body.ImageURL == "" {
				body.ImageURL = firstNonEmpty(sb.FirstFrameImage, sb.ComposedImage)
			}
			if body.LastFrameURL == "" {
				body.LastFrameURL = sb.LastFrameImage
			}
			if body.Duration == 0 {
				body.Duration = sb.Duration
			}
			if body.EpisodeID == nil {
				eid := sb.EpisodeID
				body.EpisodeID = &eid
			}
		}
	}
	if err := validateGenerationOwnership(c, body.StoryboardID, body.DramaID, nil, nil, body.EpisodeID); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	var configID *uint = body.ConfigID
	if body.ConfigID != nil {
		if err := validateAIConfigReferenceFor(c, *body.ConfigID, "video"); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	if configID == nil && body.EpisodeID != nil {
		var ep models.Episode
		if err := organizationDB(c).First(&ep, *body.EpisodeID).Error; err == nil {
			configID = ep.VideoConfigID
		}
	}
	rec := &models.VideoGeneration{
		OrganizationID: currentOrganizationID(c),
		StoryboardID:   body.StoryboardID, DramaID: body.DramaID, Prompt: body.Prompt,
		ImageURL: body.ImageURL, FirstFrameURL: body.FirstFrameURL, LastFrameURL: body.LastFrameURL,
		ReferenceMode: body.ReferenceMode, Duration: body.Duration, AspectRatio: body.AspectRatio,
	}
	if err := s.Videos.Generate(c.Request.Context(), rec, configID); err != nil {
		respondGenerationError(c, err)
		return
	}
	response.Success(c, rec)
}

func (s *Server) getVideo(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var rec models.VideoGeneration
	if err := organizationDB(c).First(&rec, id).Error; err != nil {
		response.NotFound(c, "not found")
		return
	}
	response.Success(c, rec)
}

func (s *Server) registerUpload(api *gin.RouterGroup) {
	api.POST("/upload/image", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, mediafetch.MaxImageUploadBytes+(1<<20))
		file, err := c.FormFile("file")
		if err != nil {
			response.BadRequest(c, "file is required")
			return
		}
		if file.Size < 1 || file.Size > mediafetch.MaxImageUploadBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": http.StatusRequestEntityTooLarge, "message": "image exceeds 20 MiB limit"})
			return
		}
		f, err := file.Open()
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		defer f.Close()
		info, err := mediafetch.ValidateImageUpload(f, file.Size)
		if err != nil {
			c.JSON(http.StatusUnsupportedMediaType, gin.H{"code": http.StatusUnsupportedMediaType, "message": err.Error()})
			return
		}
		if _, err := f.Seek(0, 0); err != nil {
			response.ServerError(c, "failed to read image")
			return
		}
		rel, err := s.Images.SaveUpload(f, "upload"+info.Extension)
		if err != nil {
			response.ServerError(c, err.Error())
			return
		}
		url := s.Store.PublicURL(rel)
		if err := s.bindUploadedImage(c, url, rel); err != nil {
			_ = os.Remove(s.Store.Abs(rel))
			response.BadRequest(c, err.Error())
			return
		}
		response.Success(c, gin.H{"url": url, "path": rel, "width": info.Width, "height": info.Height})
	})
	api.POST("/upload/media", s.uploadMedia)
}

func (s *Server) bindUploadedImage(c *gin.Context, url, rel string) error {
	parseID := func(key string) (*uint, error) {
		raw := strings.TrimSpace(c.PostForm(key))
		if raw == "" {
			return nil, nil
		}
		id, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("invalid %s", key)
		}
		value := uint(id)
		return &value, nil
	}
	characterID, err := parseID("character_id")
	if err != nil {
		return err
	}
	sceneID, err := parseID("scene_id")
	if err != nil {
		return err
	}
	propID, err := parseID("prop_id")
	if err != nil {
		return err
	}
	storyboardID, err := parseID("storyboard_id")
	if err != nil {
		return err
	}
	episodeID, err := parseID("episode_id")
	if err != nil {
		return err
	}
	dramaID, err := parseID("drama_id")
	if err != nil {
		return err
	}
	bindingTargets := 0
	for _, target := range []*uint{characterID, sceneID, propID, storyboardID} {
		if target != nil {
			bindingTargets++
		}
	}
	if bindingTargets > 1 {
		return errors.New("upload accepts only one binding target")
	}
	if storyboardID != nil || episodeID != nil || dramaID != nil {
		if err := validateAssetOwnership(c, dramaID, episodeID, storyboardID); err != nil {
			return err
		}
	}
	category := c.PostForm("category")
	if category == "" {
		category = "upload"
	}
	name := c.PostForm("name")
	if name == "" {
		name = "上传图片"
	}
	now := response.Now()
	if characterID != nil {
		var row models.Character
		if err := organizationDB(c).First(&row, *characterID).Error; err != nil {
			return errors.New("character not found")
		}
		if dramaID != nil && row.DramaID != *dramaID {
			return errors.New("character does not belong to drama")
		}
		if episodeID != nil {
			var ep models.Episode
			if err := organizationDB(c).First(&ep, *episodeID).Error; err != nil || ep.DramaID != row.DramaID {
				return errors.New("character does not belong to episode")
			}
		}
		if err := organizationDB(c).Model(&row).Updates(map[string]any{"image_url": url, "local_path": rel, "updated_at": now}).Error; err != nil {
			return err
		}
		dramaID = &row.DramaID
		category = "character"
	}
	if sceneID != nil {
		var row models.Scene
		if err := organizationDB(c).First(&row, *sceneID).Error; err != nil {
			return errors.New("scene not found")
		}
		if dramaID != nil && row.DramaID != *dramaID {
			return errors.New("scene does not belong to drama")
		}
		if err := organizationDB(c).Model(&row).Updates(map[string]any{"image_url": url, "local_path": rel, "status": "completed", "updated_at": now}).Error; err != nil {
			return err
		}
		dramaID = &row.DramaID
		category = "scene"
	}
	if propID != nil {
		var row models.Prop
		if err := organizationDB(c).First(&row, *propID).Error; err != nil {
			return errors.New("prop not found")
		}
		if dramaID != nil && row.DramaID != *dramaID {
			return errors.New("prop does not belong to drama")
		}
		if err := organizationDB(c).Model(&row).Updates(map[string]any{"image_url": url, "local_path": rel, "updated_at": now}).Error; err != nil {
			return err
		}
		dramaID = &row.DramaID
		category = "prop"
	}
	asset := models.Asset{OrganizationID: currentOrganizationID(c), DramaID: dramaID, EpisodeID: episodeID, StoryboardID: storyboardID, Name: name, Type: "image", Category: category, URL: url, LocalPath: rel, MimeType: "image", CreatedAt: now, UpdatedAt: now}
	return organizationDB(c).Create(&asset).Error
}

func validateGenerationOwnership(c *gin.Context, storyboardID, dramaID, sceneID, characterID, episodeID *uint) error {
	var expectedDrama uint
	var expectedEpisode uint
	mergeDrama := func(id uint) error {
		if expectedDrama != 0 && expectedDrama != id {
			return errors.New("generation resources belong to different dramas")
		}
		expectedDrama = id
		return nil
	}
	mergeEpisode := func(id uint) error {
		if expectedEpisode != 0 && expectedEpisode != id {
			return errors.New("generation resources belong to different episodes")
		}
		expectedEpisode = id
		return nil
	}
	loadEpisode := func(id uint) error {
		var episode models.Episode
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", id).First(&episode).Error; err != nil {
			return errors.New("episode not found")
		}
		if err := mergeEpisode(episode.ID); err != nil {
			return err
		}
		return mergeDrama(episode.DramaID)
	}
	if episodeID != nil {
		if err := loadEpisode(*episodeID); err != nil {
			return err
		}
	}
	if storyboardID != nil {
		var storyboard models.Storyboard
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *storyboardID).First(&storyboard).Error; err != nil {
			return errors.New("storyboard not found")
		}
		if err := loadEpisode(storyboard.EpisodeID); err != nil {
			return err
		}
	}
	if sceneID != nil {
		var scene models.Scene
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *sceneID).First(&scene).Error; err != nil {
			return errors.New("scene not found")
		}
		if err := mergeDrama(scene.DramaID); err != nil {
			return err
		}
		if scene.EpisodeID != nil {
			if err := mergeEpisode(*scene.EpisodeID); err != nil {
				return err
			}
		}
	}
	if characterID != nil {
		var character models.Character
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *characterID).First(&character).Error; err != nil {
			return errors.New("character not found")
		}
		if err := mergeDrama(character.DramaID); err != nil {
			return err
		}
	}
	if dramaID != nil {
		var drama models.Drama
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *dramaID).First(&drama).Error; err != nil {
			return errors.New("drama not found")
		}
		if err := mergeDrama(drama.ID); err != nil {
			return err
		}
	}
	return nil
}

func validateGridOwnership(c *gin.Context, dramaID, episodeID *uint, storyboardIDs []uint) error {
	var expectedDrama uint
	var expectedEpisode uint
	mergeDrama := func(id uint) error {
		if expectedDrama != 0 && expectedDrama != id {
			return errors.New("grid resources belong to different dramas")
		}
		expectedDrama = id
		return nil
	}
	mergeEpisode := func(id uint) error {
		if expectedEpisode != 0 && expectedEpisode != id {
			return errors.New("grid resources belong to different episodes")
		}
		expectedEpisode = id
		return nil
	}
	if dramaID != nil {
		var drama models.Drama
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *dramaID).First(&drama).Error; err != nil {
			return errors.New("drama not found")
		}
		if err := mergeDrama(drama.ID); err != nil {
			return err
		}
	}
	if episodeID != nil {
		var episode models.Episode
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *episodeID).First(&episode).Error; err != nil {
			return errors.New("episode not found")
		}
		if err := mergeEpisode(episode.ID); err != nil {
			return err
		}
		if err := mergeDrama(episode.DramaID); err != nil {
			return err
		}
	}
	seen := make(map[uint]struct{}, len(storyboardIDs))
	for _, storyboardID := range storyboardIDs {
		if storyboardID == 0 {
			return errors.New("invalid storyboard id")
		}
		if _, exists := seen[storyboardID]; exists {
			continue
		}
		seen[storyboardID] = struct{}{}
		var storyboard models.Storyboard
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", storyboardID).First(&storyboard).Error; err != nil {
			return errors.New("storyboard not found")
		}
		var episode models.Episode
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", storyboard.EpisodeID).First(&episode).Error; err != nil {
			return errors.New("storyboard episode not found")
		}
		if err := mergeEpisode(episode.ID); err != nil {
			return err
		}
		if err := mergeDrama(episode.DramaID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) registerCompose(api *gin.RouterGroup) {
	g := api.Group("/compose")
	g.POST("/storyboards/:id/compose", s.composeShot)
	g.POST("/episodes/:id/compose-all", s.composeAll)
	g.GET("/episodes/:id/compose-status", s.composeStatus)
}

func (s *Server) composeShot(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var sb models.Storyboard
	if err := organizationDB(c).First(&sb, id).Error; err != nil {
		response.NotFound(c, "not found")
		return
	}
	if sb.VideoURL == "" {
		response.BadRequest(c, "storyboard has no video")
		return
	}
	_, err := generation.EnsureLocalFile(s.Store, sb.VideoURL)
	if err != nil {
		response.BadRequest(c, "video file missing: "+err.Error())
		return
	}
	outRel := filepath.ToSlash(filepath.Join("composed", "shot_"+strconv.Itoa(id)+".mp4"))
	payload, _ := json.Marshal(map[string]any{
		"storyboard_id": sb.ID, "video_url": sb.VideoURL, "audio_url": sb.TTSAudioURL,
		"subtitle_url": sb.SubtitleURL, "output_rel": outRel,
	})
	job, err := s.Jobs.CreateQueuedPayloadOrganization(currentOrganizationID(c), "shot_compose", "storyboard_compose", sb.ID, "ffmpeg", nil, string(payload))
	if err != nil {
		respondGenerationError(c, err)
		return
	}
	response.Success(c, gin.H{"job_id": job.ID, "job": job})
}

func (s *Server) composeAll(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if _, ok := requireEpisode(c, id); !ok {
		return
	}
	var rows []models.Storyboard
	organizationDB(c).Where("episode_id = ? AND deleted_at IS NULL", id).Order("storyboard_number").Find(&rows)
	if len(rows) == 0 {
		response.BadRequest(c, "episode has no storyboards")
		return
	}
	shots := make([]map[string]any, 0, len(rows))
	for _, sb := range rows {
		if sb.VideoURL == "" {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "all storyboards must have videos before compose", "storyboard_id": sb.ID})
			return
		}
		_, err := generation.EnsureLocalFile(s.Store, sb.VideoURL)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "storyboard video file missing", "storyboard_id": sb.ID})
			return
		}
		shots = append(shots, map[string]any{
			"storyboard_id": sb.ID, "video_url": sb.VideoURL, "audio_url": sb.TTSAudioURL,
			"subtitle_url": sb.SubtitleURL,
			"output_rel":   filepath.ToSlash(filepath.Join("composed", fmt.Sprintf("shot_%d.mp4", sb.ID))),
		})
	}
	payload, _ := json.Marshal(map[string]any{"episode_id": uint(id), "shots": shots})
	job, err := s.Jobs.CreateQueuedPayloadOrganization(currentOrganizationID(c), "episode_compose", "episode_compose", uint(id), "ffmpeg", nil, string(payload))
	if err != nil {
		respondGenerationError(c, err)
		return
	}
	response.Success(c, gin.H{"job_id": job.ID, "job": job})
}

func (s *Server) composeStatus(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if _, ok := requireEpisode(c, id); !ok {
		return
	}
	var total, done int64
	organizationDB(c).Model(&models.Storyboard{}).Where("episode_id = ? AND deleted_at IS NULL", id).Count(&total)
	organizationDB(c).Model(&models.Storyboard{}).Where("episode_id = ? AND composed_video_url != '' AND composed_video_url IS NOT NULL", id).Count(&done)
	response.Success(c, gin.H{"total": total, "done": done})
}

func (s *Server) registerMerge(api *gin.RouterGroup) {
	g := api.Group("/merge")
	g.POST("/episodes/:id/merge", s.mergeEpisode)
	g.GET("/episodes/:id/merge", s.mergeStatus)
}

func (s *Server) mergeEpisode(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if _, ok := requireEpisode(c, id); !ok {
		return
	}
	var rows []models.Storyboard
	organizationDB(c).Where("episode_id = ? AND deleted_at IS NULL", id).Order("storyboard_number").Find(&rows)
	paths := make([]string, 0, len(rows))
	inputs := make([]string, 0, len(rows))
	for _, sb := range rows {
		src := firstNonEmpty(sb.ComposedVideoURL, sb.VideoURL)
		if src == "" {
			continue
		}
		abs, err := generation.EnsureLocalFile(s.Store, src)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "all storyboards must have valid videos before export", "storyboard_id": sb.ID})
			return
		}
		paths = append(paths, abs)
		inputs = append(inputs, src)
	}
	if len(paths) == 0 {
		response.BadRequest(c, "no videos to merge")
		return
	}
	if len(paths) != len(rows) {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "all storyboards must have valid videos before export", "available": len(paths), "required": len(rows)})
		return
	}
	outRel := filepath.ToSlash(filepath.Join("merged", "episode_"+strconv.Itoa(id)+".mp4"))
	payload, _ := json.Marshal(map[string]any{"episode_id": uint(id), "inputs": inputs, "output_rel": outRel})
	job, err := s.Jobs.CreateQueuedPayloadOrganization(currentOrganizationID(c), "episode_merge", "episode_merge", uint(id), "ffmpeg", nil, string(payload))
	if err != nil {
		respondGenerationError(c, err)
		return
	}
	response.Success(c, gin.H{"job_id": job.ID, "job": job})
}

func (s *Server) mergeStatus(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var ep models.Episode
	if err := organizationDB(c).First(&ep, id).Error; err != nil {
		response.NotFound(c, "not found")
		return
	}
	response.Success(c, gin.H{"episode_id": id, "video_url": ep.VideoURL, "status": ep.Status})
}

func (s *Server) registerGrid(api *gin.RouterGroup) {
	g := api.Group("/grid")
	g.POST("/prompt", s.gridPrompt)
	g.POST("/generate", s.gridGenerate)
	g.GET("/status/:id", s.gridStatus)
	g.POST("/split", s.gridSplit)
}

func (s *Server) gridPrompt(c *gin.Context) {
	var body struct {
		Rows      int    `json:"rows"`
		Cols      int    `json:"cols"`
		Mode      string `json:"mode"`
		EpisodeID *uint  `json:"episode_id"`
		DramaID   *uint  `json:"drama_id"`
	}
	if err := bindOptionalJSON(c, &body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if body.Rows == 0 {
		body.Rows = 2
	}
	if body.Cols == 0 {
		body.Cols = 2
	}
	if body.Rows < 1 || body.Rows > 5 || body.Cols < 1 || body.Cols > 5 || body.Rows*body.Cols > 25 {
		response.BadRequest(c, "grid must be between 1x1 and 5x5")
		return
	}
	if body.Mode == "" {
		body.Mode = "first_frame"
	}
	if err := validateGridOwnership(c, body.DramaID, body.EpisodeID, nil); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	var shots []models.Storyboard
	if body.EpisodeID != nil {
		organizationDB(c).Where("episode_id = ? AND deleted_at IS NULL", *body.EpisodeID).Order("storyboard_number").Find(&shots)
	}
	prompt, cells := s.buildGridPrompt(body.Mode, body.Rows, body.Cols, shots)
	response.Success(c, gin.H{"grid_prompt": prompt, "mode": body.Mode, "rows": body.Rows, "cols": body.Cols, "cell_prompts": cells})
}

func (s *Server) gridGenerate(c *gin.Context) {
	var body struct {
		Prompt        string   `json:"prompt"`
		DramaID       *uint    `json:"drama_id"`
		EpisodeID     *uint    `json:"episode_id"`
		ConfigID      *uint    `json:"config_id"`
		Size          string   `json:"size"`
		Mode          string   `json:"mode"`
		Rows          int      `json:"rows"`
		Cols          int      `json:"cols"`
		CellPrompts   []string `json:"cell_prompts"`
		StoryboardIDs []uint   `json:"storyboard_ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Prompt == "" {
		response.BadRequest(c, "prompt required")
		return
	}
	if body.Rows == 0 {
		body.Rows = 2
	}
	if body.Cols == 0 {
		body.Cols = 2
	}
	if body.Mode == "" {
		body.Mode = "first_frame"
	}
	if body.Rows < 1 || body.Rows > 5 || body.Cols < 1 || body.Cols > 5 || body.Rows*body.Cols > 25 {
		response.BadRequest(c, "grid must be between 1x1 and 5x5")
		return
	}
	if err := validateGridOwnership(c, body.DramaID, body.EpisodeID, body.StoryboardIDs); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	configID := body.ConfigID
	if body.ConfigID != nil {
		if err := validateAIConfigReferenceFor(c, *body.ConfigID, "image"); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	if configID == nil && body.EpisodeID != nil {
		var ep models.Episode
		if err := organizationDB(c).First(&ep, *body.EpisodeID).Error; err == nil {
			configID = ep.ImageConfigID
		}
	}
	rec := &models.ImageGeneration{OrganizationID: currentOrganizationID(c), DramaID: body.DramaID, Prompt: body.Prompt, ImageType: "grid", Size: body.Size}
	ts := response.Now()
	hist := models.GridHistory{
		OrganizationID: currentOrganizationID(c),
		DramaID:        body.DramaID, EpisodeID: body.EpisodeID, Mode: body.Mode,
		Rows: body.Rows, Cols: body.Cols, Prompt: body.Prompt,
		CellPrompts: mustJSON(body.CellPrompts), StoryboardIDs: mustJSON(body.StoryboardIDs),
		Status: "processing", CreatedAt: ts, UpdatedAt: ts,
	}
	if err := organizationDB(c).Create(&hist).Error; err != nil {
		response.ServerError(c, "failed to create grid history")
		return
	}
	if err := s.Images.Generate(c.Request.Context(), rec, configID); err != nil {
		hist.Status = "failed"
		hist.ErrorMsg = err.Error()
		hist.UpdatedAt = response.Now()
		organizationDB(c).Save(&hist)
		response.BadRequest(c, err.Error())
		return
	}
	hist.ImageGenID = &rec.ID
	hist.ImageURL = rec.ImageURL
	if rec.Status == "completed" {
		hist.Status = "completed"
		now := response.Now()
		hist.CompletedAt = &now
	}
	if rec.Status == "processing" {
		hist.Status = "processing"
	}
	hist.UpdatedAt = response.Now()
	organizationDB(c).Save(&hist)
	response.Success(c, gin.H{"image": rec, "history": hist})
}

func (s *Server) gridStatus(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var rec models.ImageGeneration
	if err := organizationDB(c).First(&rec, id).Error; err != nil {
		response.NotFound(c, "not found")
		return
	}
	response.Success(c, rec)
}

func (s *Server) gridSplit(c *gin.Context) {
	var body struct {
		ImagePath     string `json:"image_path"`
		ImageURL      string `json:"image_url"`
		Rows          int    `json:"rows"`
		Cols          int    `json:"cols"`
		StoryboardIDs []uint `json:"storyboard_ids"`
		FrameType     string `json:"frame_type"`
		HistoryID     *uint  `json:"history_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	if body.Rows == 0 {
		body.Rows = 2
	}
	if body.Cols == 0 {
		body.Cols = 2
	}
	if body.FrameType == "" {
		body.FrameType = "first_frame"
	}
	if body.Rows < 1 || body.Rows > 5 || body.Cols < 1 || body.Cols > 5 || body.Rows*body.Cols > 25 {
		response.BadRequest(c, "grid must be between 1x1 and 5x5")
		return
	}
	var history models.GridHistory
	var historyDramaID, historyEpisodeID *uint
	if body.HistoryID != nil {
		if err := organizationDB(c).First(&history, *body.HistoryID).Error; err != nil {
			response.NotFound(c, "grid history not found")
			return
		}
		historyDramaID, historyEpisodeID = history.DramaID, history.EpisodeID
	}
	if err := validateGridOwnership(c, historyDramaID, historyEpisodeID, body.StoryboardIDs); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	if body.FrameType == "first_last" {
		if len(body.StoryboardIDs) == 0 || len(body.StoryboardIDs)%2 != 0 || len(body.StoryboardIDs) != body.Rows*body.Cols {
			response.BadRequest(c, "first_last grid requires an even number of storyboard ids matching grid cells")
			return
		}
	}
	src := firstNonEmpty(body.ImagePath, body.ImageURL)
	abs, err := generation.EnsureLocalFile(s.Store, src)
	if err != nil {
		abs, err = generation.EnsureLocalFile(s.Store, strings.TrimPrefix(src, "/static/"))
		if err != nil {
			response.BadRequest(c, "image not found")
			return
		}
	}
	outDir := filepath.Join(s.Store.Root, "grid_cells", uuid.NewString())
	paths, err := ffmpeg.SplitGrid(abs, body.Rows, body.Cols, outDir)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	urls := make([]string, 0, len(paths))
	for _, pth := range paths {
		rel, _ := filepath.Rel(s.Store.Root, pth)
		rel = filepath.ToSlash(rel)
		url := s.Store.PublicURL(rel)
		urls = append(urls, url)
	}
	if err := s.assignGridCells(c, body.StoryboardIDs, urls, body.FrameType); err != nil {
		_ = os.RemoveAll(outDir)
		response.ServerError(c, "failed to assign grid cells")
		return
	}
	if body.HistoryID != nil {
		organizationDB(c).Model(&models.GridHistory{}).Where("id = ?", *body.HistoryID).Updates(map[string]any{
			"cells_json": mustJSON(urls), "status": "split", "updated_at": response.Now(),
			"storyboard_ids": mustJSON(body.StoryboardIDs),
		})
	}
	response.Success(c, gin.H{"cells": urls, "count": len(urls)})
}
