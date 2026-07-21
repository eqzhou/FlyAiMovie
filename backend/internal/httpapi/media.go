package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/ffmpeg"
	"github.com/eqzhou/flyaimovie/internal/services/generation"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	"github.com/eqzhou/flyaimovie/internal/services/mediafetch"
	"github.com/eqzhou/flyaimovie/internal/services/prompttemplate"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
		StoryboardID       *uint    `json:"storyboard_id"`
		DramaID            *uint    `json:"drama_id"`
		Prompt             string   `json:"prompt"`
		ImageURL           string   `json:"image_url"`
		FirstFrameURL      string   `json:"first_frame_url"`
		LastFrameURL       string   `json:"last_frame_url"`
		ReferenceImageURLs []string `json:"reference_image_urls"`
		ReferenceMode      string   `json:"reference_mode"`
		Duration           int      `json:"duration"`
		AspectRatio        string   `json:"aspect_ratio"`
		ConfigID           *uint    `json:"config_id"`
		EpisodeID          *uint    `json:"episode_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	if len(body.ReferenceImageURLs) > 8 {
		response.BadRequest(c, "at most 8 reference_image_urls are allowed")
		return
	}
	if err := validateLocalMediaOwnership(c, body.ReferenceImageURLs...); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	referenceImageURLs := ""
	if len(body.ReferenceImageURLs) > 0 {
		encodedReferences, err := json.Marshal(body.ReferenceImageURLs)
		if err != nil {
			response.BadRequest(c, "invalid reference_image_urls")
			return
		}
		referenceImageURLs = string(encodedReferences)
	}
	// auto-fill from storyboard
	if body.StoryboardID != nil {
		var sb models.Storyboard
		if err := findActiveStoryboard(c, *body.StoryboardID, &sb); err == nil {
			if referenceImageURLs == "" {
				referenceImageURLs = sb.ReferenceImages
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
			if body.Prompt == "" {
				var ep models.Episode
				organizationDB(c).First(&ep, sb.EpisodeID)
				var drama models.Drama
				organizationDB(c).First(&drama, ep.DramaID)
				characterNames, sceneNames := promptAssetNames(c, ep.DramaID, &sb)
				resolution := prompttemplate.VideoPrompt(organizationDB(c), currentOrganizationID(c), drama, ep, sb, "", characterNames, sceneNames)
				body.Prompt = strings.TrimSpace(resolution.Prompt)
			}
		}
	}
	if strings.TrimSpace(body.Prompt) == "" {
		response.BadRequest(c, "prompt empty")
		return
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
		ReferenceImageURLs: referenceImageURLs,
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
		savedRel := rel
		url := s.Store.PublicURL(rel)
		hash, size, err := mediacache.HashFile(s.Store.Abs(rel))
		if err != nil {
			_ = os.Remove(s.Store.Abs(rel))
			response.ServerError(c, "failed to hash image")
			return
		}
		temporaryKey := "upload:" + rel
		cacheObject, reused, err := s.Cache.Put(mediacache.PutInput{OrganizationID: currentOrganizationID(c), Namespace: "image_upload", Key: temporaryKey,
			ContentHash: hash, Kind: "image", LocalPath: rel, PublicURL: url, MimeType: info.MIME, Size: size})
		if err != nil {
			_ = os.Remove(s.Store.Abs(rel))
			response.ServerError(c, "failed to cache image")
			return
		}
		rel, url = cacheObject.LocalPath, cacheObject.PublicURL
		if reused && savedRel != rel {
			_ = os.Remove(s.Store.Abs(savedRel))
		}
		if err := s.bindUploadedImage(c, url, rel, hash, size, temporaryKey); err != nil {
			_ = s.Cache.Release(currentOrganizationID(c), "image_upload", temporaryKey)
			if !reused {
				_ = os.Remove(s.Store.Abs(savedRel))
			}
			response.BadRequest(c, err.Error())
			return
		}
		response.Success(c, gin.H{"url": url, "path": rel, "width": info.Width, "height": info.Height})
	})
	api.POST("/upload/media", s.uploadMedia)
}

func (s *Server) bindUploadedImage(c *gin.Context, url, rel, contentHash string, fileSize int64, temporaryCacheKey string) error {
	return db.DB.Transaction(func(tx *gorm.DB) error {
		return s.bindUploadedImageTx(c, tx, url, rel, contentHash, fileSize, temporaryCacheKey)
	})
}

func (s *Server) bindUploadedImageTx(c *gin.Context, tx *gorm.DB, url, rel, contentHash string, fileSize int64, temporaryCacheKey string) error {
	organizationQuery := func() *gorm.DB {
		if actor, ok := currentAuth(c); ok {
			return tx.Where("organization_id = ?", actor.Organization.ID)
		}
		return tx
	}
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
		if err := organizationQuery().First(&row, *characterID).Error; err != nil {
			return errors.New("character not found")
		}
		if dramaID != nil && row.DramaID != *dramaID {
			return errors.New("character does not belong to drama")
		}
		if episodeID != nil {
			var ep models.Episode
			if err := organizationQuery().First(&ep, *episodeID).Error; err != nil || ep.DramaID != row.DramaID {
				return errors.New("character does not belong to episode")
			}
		}
		if err := organizationQuery().Model(&row).Updates(map[string]any{"image_url": url, "local_path": rel, "updated_at": now}).Error; err != nil {
			return err
		}
		dramaID = &row.DramaID
		category = "character"
	}
	if sceneID != nil {
		var row models.Scene
		if err := organizationQuery().First(&row, *sceneID).Error; err != nil {
			return errors.New("scene not found")
		}
		if dramaID != nil && row.DramaID != *dramaID {
			return errors.New("scene does not belong to drama")
		}
		if err := organizationQuery().Model(&row).Updates(map[string]any{"image_url": url, "local_path": rel, "status": "completed", "updated_at": now}).Error; err != nil {
			return err
		}
		dramaID = &row.DramaID
		category = "scene"
	}
	if propID != nil {
		var row models.Prop
		if err := organizationQuery().First(&row, *propID).Error; err != nil {
			return errors.New("prop not found")
		}
		if dramaID != nil && row.DramaID != *dramaID {
			return errors.New("prop does not belong to drama")
		}
		if err := organizationQuery().Model(&row).Updates(map[string]any{"image_url": url, "local_path": rel, "updated_at": now}).Error; err != nil {
			return err
		}
		dramaID = &row.DramaID
		category = "prop"
	}
	asset := models.Asset{OrganizationID: currentOrganizationID(c), DramaID: dramaID, EpisodeID: episodeID, StoryboardID: storyboardID, Name: name, Type: "image", Category: category,
		URL: url, LocalPath: rel, MimeType: "image", ContentHash: contentHash, FileSize: fileSize, CreatedAt: now, UpdatedAt: now}
	if err := organizationQuery().Create(&asset).Error; err != nil {
		return err
	}
	cache := mediacache.New(tx, s.Store)
	if _, _, err := cache.Put(mediacache.PutInput{OrganizationID: asset.OrganizationID, Namespace: "asset", Key: strconv.FormatUint(uint64(asset.ID), 10),
		ContentHash: contentHash, Kind: "image", LocalPath: rel, PublicURL: url, MimeType: "image", Size: fileSize}); err != nil {
		return err
	}
	return cache.Release(asset.OrganizationID, "image_upload", temporaryCacheKey)
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
	if err := findActiveStoryboard(c, uint(id), &sb); err != nil {
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
	g.POST("/history/:id/assign", s.gridAssignCell)
}

const maxGridRequestBodyBytes = 128 << 10
const maxGridCellPromptRunes = 4_000

func normalizeGridMode(mode string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" {
		return "first_frame", nil
	}
	switch normalized {
	case "first_frame", "first_last", "multi_ref":
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported grid mode %q", normalized)
	}
}

func validateGridStoryboardCount(mode string, rows, cols int, storyboardIDs []uint) error {
	capacity := rows * cols
	if len(storyboardIDs) > capacity {
		return fmt.Errorf("grid accepts at most %d storyboard ids", capacity)
	}
	uniqueCount := len(storyboardIDs)
	if mode == "first_last" {
		if capacity%2 != 0 || len(storyboardIDs) != capacity {
			return errors.New("first_last grid requires an even number of mirrored storyboard ids matching grid cells")
		}
		uniqueCount = capacity / 2
		for index := 0; index < uniqueCount; index++ {
			if storyboardIDs[index] != storyboardIDs[index+uniqueCount] {
				return errors.New("first_last grid requires mirrored storyboard ids for first and last frames")
			}
		}
	}
	seen := make(map[uint]struct{}, uniqueCount)
	for _, storyboardID := range storyboardIDs[:uniqueCount] {
		if _, exists := seen[storyboardID]; exists {
			return errors.New("grid storyboard ids must be unique target slots")
		}
		seen[storyboardID] = struct{}{}
	}
	return nil
}

func validateGridCellPrompts(rows, cols int, cellPrompts []string) error {
	if len(cellPrompts) > rows*cols {
		return fmt.Errorf("cell_prompts accepts at most %d items", rows*cols)
	}
	for _, prompt := range cellPrompts {
		if len([]rune(prompt)) > maxGridCellPromptRunes {
			return fmt.Errorf("each cell_prompts item must be at most %d characters", maxGridCellPromptRunes)
		}
	}
	return nil
}

type gridCellAssignment struct {
	CellIndex    int    `json:"cell_index"`
	StoryboardID uint   `json:"storyboard_id"`
	FrameType    string `json:"frame_type"`
}

var (
	errUnregisteredGridCell  = errors.New("grid cell is not registered to the current organization")
	errGridCellMissing       = errors.New("grid cell does not exist")
	errGridStoryboardMissing = errors.New("storyboard not found")
)

func decodeGridStrings(raw string) []string {
	var values []string
	if json.Unmarshal([]byte(raw), &values) != nil {
		return nil
	}
	return values
}

func decodeGridIDs(raw string) []uint {
	var values []uint
	if json.Unmarshal([]byte(raw), &values) != nil {
		return nil
	}
	return values
}

func deriveGridAssignments(mode string, storyboardIDs []uint, cellCount int, requestedFrameType string) []gridCellAssignment {
	assignments := make([]gridCellAssignment, 0, min(cellCount, len(storyboardIDs)))
	if mode == "first_last" || requestedFrameType == "first_last" {
		half := len(storyboardIDs) / 2
		if half == 0 {
			return assignments
		}
		for index := 0; index < cellCount && index < len(storyboardIDs); index++ {
			frameType := "first_frame"
			if index >= cellCount/2 {
				frameType = "last_frame"
			}
			assignments = append(assignments, gridCellAssignment{CellIndex: index, StoryboardID: storyboardIDs[index%half], FrameType: frameType})
		}
		return assignments
	}
	frameType, err := normalizeStoryboardFrameType(requestedFrameType)
	if err != nil {
		frameType = "first_frame"
	}
	for index := 0; index < cellCount && index < len(storyboardIDs); index++ {
		assignments = append(assignments, gridCellAssignment{CellIndex: index, StoryboardID: storyboardIDs[index], FrameType: frameType})
	}
	return assignments
}

func historyGridAssignments(history models.GridHistory, cellCount int) []gridCellAssignment {
	var assignments []gridCellAssignment
	if json.Unmarshal([]byte(history.AssignmentsJSON), &assignments) == nil {
		valid := make([]gridCellAssignment, 0, len(assignments))
		for _, assignment := range assignments {
			if assignment.CellIndex >= 0 && assignment.CellIndex < cellCount && assignment.StoryboardID > 0 {
				if frameType, err := normalizeStoryboardFrameType(assignment.FrameType); err == nil {
					assignment.FrameType = frameType
					valid = append(valid, assignment)
				}
			}
		}
		if len(valid) > 0 {
			return valid
		}
	}
	return deriveGridAssignments(history.Mode, decodeGridIDs(history.StoryboardIDs), cellCount, history.SplitFrameType)
}

func replaceGridAssignment(assignments []gridCellAssignment, replacement gridCellAssignment) []gridCellAssignment {
	result := make([]gridCellAssignment, 0, len(assignments)+1)
	replaced := false
	for _, assignment := range assignments {
		if assignment.CellIndex == replacement.CellIndex || (assignment.StoryboardID == replacement.StoryboardID && assignment.FrameType == replacement.FrameType) {
			if assignment.CellIndex == replacement.CellIndex && !replaced {
				result = append(result, replacement)
				replaced = true
			}
			continue
		}
		result = append(result, assignment)
	}
	if !replaced {
		result = append(result, replacement)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CellIndex < result[j].CellIndex })
	return result
}

func gridFrameColumn(frameType string) string {
	switch frameType {
	case "last_frame":
		return "last_frame_image"
	case "composed":
		return "composed_image"
	default:
		return "first_frame_image"
	}
}

func gridFrameUpdate(frameType, url string) map[string]any {
	updates := map[string]any{"updated_at": response.Now()}
	switch frameType {
	case "last_frame":
		updates["last_frame_image"] = url
	case "composed":
		updates["composed_image"] = url
	default:
		updates["first_frame_image"] = url
	}
	return updates
}

func (s *Server) gridAssignCell(c *gin.Context) {
	historyID, err := strconv.Atoi(c.Param("id"))
	if err != nil || historyID < 1 {
		response.BadRequest(c, "invalid grid history id")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var body map[string]any
	if err := bindSingleJSON(c, &body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if err := rejectUnknownFields(body, "cell_index", "storyboard_id", "frame_type"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	cellIndex, validIndex := nonNegativeJSONInt(body["cell_index"])
	storyboardID, validStoryboard := positiveJSONInt(body["storyboard_id"])
	frameValue, validFrame := body["frame_type"].(string)
	frameType, frameErr := normalizeStoryboardFrameType(frameValue)
	if !validIndex || !validStoryboard || !validFrame || frameErr != nil {
		response.BadRequest(c, "cell_index, storyboard_id and a valid frame_type are required")
		return
	}
	setAuditResource(c, "grid_cell_assignment", fmt.Sprintf("history:%d;storyboard:%d;cell:%d", historyID, storyboardID, cellIndex))

	var history models.GridHistory
	if err := organizationDB(c).First(&history, historyID).Error; err != nil {
		response.NotFound(c, "grid history not found")
		return
	}
	cells := decodeGridStrings(history.CellsJSON)
	if cellIndex >= len(cells) || strings.TrimSpace(cells[cellIndex]) == "" {
		response.BadRequest(c, "grid cell does not exist")
		return
	}
	storyboardIDValue := uint(storyboardID)
	if err := validateGridOwnership(c, history.DramaID, history.EpisodeID, []uint{storyboardIDValue}); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}

	replacement := gridCellAssignment{CellIndex: cellIndex, StoryboardID: storyboardIDValue, FrameType: frameType}
	organizationID := currentOrganizationID(c)
	committedCellURL := ""
	committedAssignments := []gridCellAssignment(nil)
	err = organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var locked models.GridHistory
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND id = ?", organizationID, historyID).First(&locked).Error; err != nil {
			return err
		}
		lockedCells := decodeGridStrings(locked.CellsJSON)
		if cellIndex >= len(lockedCells) || strings.TrimSpace(lockedCells[cellIndex]) == "" {
			return errGridCellMissing
		}
		committedCellURL = lockedCells[cellIndex]
		var ownedCellCount int64
		if !locked.CellsVerified {
			return errUnregisteredGridCell
		}
		if err := tx.Model(&models.Asset{}).
			Where("organization_id = ? AND url = ? AND category = ? AND grid_history_id = ? AND deleted_at IS NULL", organizationID, committedCellURL, "grid_cell", locked.ID).
			Count(&ownedCellCount).Error; err != nil {
			return err
		}
		if ownedCellCount < 1 {
			return errUnregisteredGridCell
		}
		assignments := historyGridAssignments(locked, len(lockedCells))
		for _, assignment := range assignments {
			if assignment.CellIndex != cellIndex || (assignment.StoryboardID == storyboardIDValue && assignment.FrameType == frameType) {
				continue
			}
			column := gridFrameColumn(assignment.FrameType)
			clear := tx.Model(&models.Storyboard{}).
				Where("organization_id = ? AND id = ? AND deleted_at IS NULL AND "+column+" = ?", organizationID, assignment.StoryboardID, committedCellURL)
			if locked.EpisodeID != nil {
				clear = clear.Where("episode_id = ?", *locked.EpisodeID)
			}
			if err := clear.Update(column, "").Error; err != nil {
				return err
			}
		}
		query := tx.Model(&models.Storyboard{}).Where("organization_id = ? AND id = ? AND deleted_at IS NULL", organizationID, storyboardIDValue)
		if locked.EpisodeID != nil {
			query = query.Where("episode_id = ?", *locked.EpisodeID)
		}
		updated := query.Updates(gridFrameUpdate(frameType, committedCellURL))
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errGridStoryboardMissing
		}
		nextAssignments := replaceGridAssignment(assignments, replacement)
		committedAssignments = nextAssignments
		return tx.Model(&models.GridHistory{}).Where("organization_id = ? AND id = ?", organizationID, locked.ID).Updates(map[string]any{
			"assignments_json": mustJSON(nextAssignments),
			"updated_at":       response.Now(),
		}).Error
	})
	if err != nil {
		switch {
		case errors.Is(err, errUnregisteredGridCell):
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "grid cells from this history must be regenerated before reassignment"})
		case errors.Is(err, errGridStoryboardMissing), errors.Is(err, gorm.ErrRecordNotFound):
			response.NotFound(c, "storyboard not found")
		case errors.Is(err, errGridCellMissing):
			response.BadRequest(c, "grid cell does not exist")
		default:
			response.ServerError(c, "failed to assign grid cell")
		}
		return
	}
	response.Success(c, gin.H{"cell_index": cellIndex, "cell_url": committedCellURL, "storyboard_id": storyboardIDValue, "frame_type": frameType, "assignments": committedAssignments})
}

func (s *Server) gridPrompt(c *gin.Context) {
	var body struct {
		Rows      int    `json:"rows"`
		Cols      int    `json:"cols"`
		Mode      string `json:"mode"`
		EpisodeID *uint  `json:"episode_id"`
		DramaID   *uint  `json:"drama_id"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxGridRequestBodyBytes)
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
	mode, err := normalizeGridMode(body.Mode)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	body.Mode = mode
	if err := validateGridOwnership(c, body.DramaID, body.EpisodeID, nil); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	var shots []models.Storyboard
	var episode models.Episode
	var drama models.Drama
	if body.EpisodeID != nil {
		organizationDB(c).Where("episode_id = ? AND deleted_at IS NULL", *body.EpisodeID).Order("storyboard_number").Find(&shots)
		organizationDB(c).First(&episode, *body.EpisodeID)
		organizationDB(c).First(&drama, episode.DramaID)
	} else if body.DramaID != nil {
		organizationDB(c).First(&drama, *body.DramaID)
	}
	prompt, cells := s.buildGridPrompt(c, currentOrganizationID(c), drama, episode, body.Mode, body.Rows, body.Cols, shots)
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxGridRequestBodyBytes)
	if err := bindSingleJSON(c, &body); err != nil || body.Prompt == "" {
		response.BadRequest(c, "prompt required")
		return
	}
	if body.Rows == 0 {
		body.Rows = 2
	}
	if body.Cols == 0 {
		body.Cols = 2
	}
	mode, err := normalizeGridMode(body.Mode)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	body.Mode = mode
	if body.Rows < 1 || body.Rows > 5 || body.Cols < 1 || body.Cols > 5 || body.Rows*body.Cols > 25 {
		response.BadRequest(c, "grid must be between 1x1 and 5x5")
		return
	}
	if err := validateGridStoryboardCount(body.Mode, body.Rows, body.Cols, body.StoryboardIDs); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := validateGridCellPrompts(body.Rows, body.Cols, body.CellPrompts); err != nil {
		response.BadRequest(c, err.Error())
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxGridRequestBodyBytes)
	if err := bindSingleJSON(c, &body); err != nil {
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
	if body.FrameType != "first_last" {
		normalizedFrameType, err := normalizeStoryboardFrameType(body.FrameType)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		body.FrameType = normalizedFrameType
	}
	if body.Rows < 1 || body.Rows > 5 || body.Cols < 1 || body.Cols > 5 || body.Rows*body.Cols > 25 {
		response.BadRequest(c, "grid must be between 1x1 and 5x5")
		return
	}
	if err := validateGridStoryboardCount(body.FrameType, body.Rows, body.Cols, body.StoryboardIDs); err != nil {
		response.BadRequest(c, err.Error())
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
		if history.Status == "split" || strings.TrimSpace(history.CellsJSON) != "" {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "grid history is already split; reassign individual cells instead"})
			return
		}
		if history.Rows != body.Rows || history.Cols != body.Cols {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "grid layout does not match the generated history"})
			return
		}
		if (history.Mode == "first_last") != (body.FrameType == "first_last") {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "grid frame mode does not match the generated history"})
			return
		}
	}
	if err := validateGridOwnership(c, historyDramaID, historyEpisodeID, body.StoryboardIDs); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	src := firstNonEmpty(body.ImagePath, body.ImageURL)
	if err := validateLocalMediaOwnership(c, src); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
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
	assignments := deriveGridAssignments(history.Mode, body.StoryboardIDs, len(urls), body.FrameType)
	var historyUpdates map[string]any
	if body.HistoryID != nil {
		historyUpdates = map[string]any{
			"cells_json":       mustJSON(urls),
			"assignments_json": mustJSON(assignments),
			"status":           "split",
			"updated_at":       response.Now(),
			"storyboard_ids":   mustJSON(body.StoryboardIDs),
			"image_url":        src,
			"split_frame_type": body.FrameType,
			"cells_verified":   true,
		}
	}
	if err := s.assignGridCellsWithHistory(c, body.StoryboardIDs, urls, body.FrameType, body.HistoryID, historyUpdates); err != nil {
		_ = os.RemoveAll(outDir)
		if errors.Is(err, errGridHistoryAlreadySplit) {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "grid history is already split; reassign individual cells instead"})
			return
		}
		response.ServerError(c, "failed to assign grid cells")
		return
	}
	response.Success(c, gin.H{"cells": urls, "count": len(urls), "assignments": assignments, "cells_verified": body.HistoryID != nil})
}
