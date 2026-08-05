package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/generation"
	"github.com/eqzhou/flyaimovie/internal/services/prompttemplate"
	"github.com/eqzhou/flyaimovie/internal/textutil"
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
		if err := findActiveEpisode(c, *body.EpisodeID, &ep); err == nil {
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
				body.ImageURL = textutil.FirstNonEmpty(sb.FirstFrameImage, sb.ComposedImage)
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
				_ = findActiveEpisode(c, sb.EpisodeID, &ep)
				var drama models.Drama
				_ = findActiveDrama(c, ep.DramaID, &drama)
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
		if err := findActiveEpisode(c, *body.EpisodeID, &ep); err == nil {
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
		src := textutil.FirstNonEmpty(sb.ComposedVideoURL, sb.VideoURL)
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
	if err := findActiveEpisode(c, uint(id), &ep); err != nil {
		response.NotFound(c, "not found")
		return
	}
	response.Success(c, gin.H{"episode_id": id, "video_url": ep.VideoURL, "status": ep.Status})
}
