package httpapi

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
)

func (s *Server) registerEpisodes(api *gin.RouterGroup) {
	g := api.Group("/episodes")
	g.POST("", s.createEpisode)
	g.PUT("/:id", s.updateEpisode)
	g.GET("/:id/characters", s.episodeCharacters)
	g.GET("/:id/scenes", s.episodeScenes)
	g.GET("/:id/storyboards", s.episodeStoryboards)
	g.GET("/:id/pipeline-status", s.pipelineStatus)
}

func (s *Server) createEpisode(c *gin.Context) {
	var body struct {
		DramaID       uint   `json:"drama_id"`
		Title         string `json:"title"`
		ImageConfigID *uint  `json:"image_config_id"`
		VideoConfigID *uint  `json:"video_config_id"`
		AudioConfigID *uint  `json:"audio_config_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.DramaID == 0 {
		response.BadRequest(c, "drama_id required")
		return
	}
	if body.ImageConfigID == nil || body.VideoConfigID == nil || body.AudioConfigID == nil {
		response.BadRequest(c, "image_config_id, video_config_id and audio_config_id are required")
		return
	}
	var drama models.Drama
	if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", body.DramaID).First(&drama).Error; err != nil {
		response.BadRequest(c, "drama not found")
		return
	}
	for _, item := range []struct {
		id          *uint
		serviceType string
	}{{body.ImageConfigID, "image"}, {body.VideoConfigID, "video"}, {body.AudioConfigID, "audio"}} {
		if err := validateAIConfigReferenceFor(c, *item.id, item.serviceType); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	var existing []models.Episode
	organizationDB(c).Where("drama_id = ?", body.DramaID).Order("episode_number").Find(&existing)
	next := 1
	for _, e := range existing {
		if e.EpisodeNumber >= next {
			next = e.EpisodeNumber + 1
		}
	}
	ts := response.Now()
	title := body.Title
	if title == "" {
		title = "第" + strconv.Itoa(next) + "集"
	}
	ep := models.Episode{
		OrganizationID: currentOrganizationID(c), DramaID: body.DramaID, EpisodeNumber: next, Title: title,
		ImageConfigID: body.ImageConfigID, VideoConfigID: body.VideoConfigID, AudioConfigID: body.AudioConfigID,
		Status: "draft", CreatedAt: ts, UpdatedAt: ts,
	}
	if err := organizationDB(c).Create(&ep).Error; err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{
		"id": ep.ID, "episode_number": ep.EpisodeNumber, "title": ep.Title,
		"image_config_id": ep.ImageConfigID, "video_config_id": ep.VideoConfigID, "audio_config_id": ep.AudioConfigID,
	})
}

func (s *Server) updateEpisode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid episode id")
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	var episode models.Episode
	if err := organizationDB(c).First(&episode, id).Error; err != nil {
		response.NotFound(c, "episode not found")
		return
	}
	updates := map[string]any{"updated_at": response.Now()}
	for _, k := range []string{"content", "script_content", "title", "description", "status"} {
		maxRunes := maxTextRunes
		if k == "title" || k == "status" {
			maxRunes = maxNameRunes
		}
		v, ok, fieldErr := stringUpdate(body, k, maxRunes)
		if fieldErr != nil {
			response.BadRequest(c, fieldErr.Error())
			return
		}
		if ok {
			if k == "title" {
				v = strings.TrimSpace(v)
				if v == "" {
					response.BadRequest(c, "title must not be empty")
					return
				}
			}
			updates[k] = v
		}
	}
	configTypes := map[string]string{"image_config_id": "image", "video_config_id": "video", "audio_config_id": "audio"}
	for _, k := range []string{"image_config_id", "video_config_id", "audio_config_id"} {
		if v, ok := body[k]; ok {
			number, valid := positiveJSONInt(v)
			if !valid {
				response.BadRequest(c, k+" must be a positive id")
				return
			}
			configID := uint(number)
			if err := validateAIConfigReferenceFor(c, configID, configTypes[k]); err != nil {
				response.BadRequest(c, err.Error())
				return
			}
			updates[k] = configID
		}
	}
	if len(updates) == 1 {
		response.BadRequest(c, "at least one episode field is required")
		return
	}
	if err := organizationDB(c).Model(&episode).Updates(updates).Error; err != nil {
		response.ServerError(c, "failed to update episode")
		return
	}
	response.Success(c, nil)
}

func validateAIConfigReference(id uint, serviceType string) error {
	var config models.AIServiceConfig
	if err := db.DB.Where("id = ? AND service_type = ? AND is_active = ?", id, serviceType, true).First(&config).Error; err != nil {
		return fmt.Errorf("active %s AI config %d not found", serviceType, id)
	}
	return nil
}

func validateAIConfigReferenceFor(c *gin.Context, id uint, serviceType string) error {
	var aiConfig models.AIServiceConfig
	if err := organizationDB(c).Where("id = ? AND service_type = ? AND is_active = ?", id, serviceType, true).First(&aiConfig).Error; err != nil {
		return fmt.Errorf("active %s AI config %d not found", serviceType, id)
	}
	return nil
}

func requireEpisode(c *gin.Context, id int) (models.Episode, bool) {
	var episode models.Episode
	if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", id).First(&episode).Error; err != nil {
		response.NotFound(c, "episode not found")
		return episode, false
	}
	return episode, true
}

func (s *Server) episodeCharacters(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if _, ok := requireEpisode(c, id); !ok {
		return
	}
	var links []models.EpisodeCharacter
	organizationDB(c).Where("episode_id = ?", id).Find(&links)
	ids := make([]uint, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.CharacterID)
	}
	var chars []models.Character
	if len(ids) > 0 {
		organizationDB(c).Where("id IN ? AND deleted_at IS NULL", ids).Find(&chars)
	}
	response.Success(c, chars)
}

func (s *Server) episodeScenes(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if _, ok := requireEpisode(c, id); !ok {
		return
	}
	var links []models.EpisodeScene
	organizationDB(c).Where("episode_id = ?", id).Find(&links)
	ids := make([]uint, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.SceneID)
	}
	var scenes []models.Scene
	// Linked scenes + scenes owned by this episode
	q := organizationDB(c).Where("deleted_at IS NULL")
	if len(ids) > 0 {
		q = q.Where("id IN ? OR episode_id = ?", ids, id)
	} else {
		q = q.Where("episode_id = ?", id)
	}
	q.Find(&scenes)
	// Dedup by id
	seen := map[uint]bool{}
	out := make([]models.Scene, 0, len(scenes))
	for _, sc := range scenes {
		if seen[sc.ID] {
			continue
		}
		seen[sc.ID] = true
		out = append(out, sc)
	}
	response.Success(c, out)
}

func (s *Server) episodeStoryboards(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if _, ok := requireEpisode(c, id); !ok {
		return
	}
	var rows []models.Storyboard
	organizationDB(c).Where("episode_id = ? AND deleted_at IS NULL", id).Order("storyboard_number").Find(&rows)
	out := make([]gin.H, 0, len(rows))
	for _, sb := range rows {
		var links []models.StoryboardCharacter
		organizationDB(c).Where("storyboard_id = ?", sb.ID).Find(&links)
		cids := make([]uint, 0, len(links))
		for _, l := range links {
			cids = append(cids, l.CharacterID)
		}
		var chars []models.Character
		if len(cids) > 0 {
			organizationDB(c).Where("id IN ?", cids).Find(&chars)
		}
		out = append(out, gin.H{
			"id": sb.ID, "episode_id": sb.EpisodeID, "scene_id": sb.SceneID,
			"storyboard_number": sb.StoryboardNumber, "title": sb.Title, "location": sb.Location,
			"time": sb.Time, "shot_type": sb.ShotType, "angle": sb.Angle, "movement": sb.Movement,
			"action": sb.Action, "result": sb.Result, "atmosphere": sb.Atmosphere,
			"image_prompt": sb.ImagePrompt, "video_prompt": sb.VideoPrompt, "bgm_prompt": sb.BGMPrompt,
			"sound_effect": sb.SoundEffect, "dialogue": sb.Dialogue, "description": sb.Description,
			"duration": sb.Duration, "composed_image": sb.ComposedImage, "first_frame_image": sb.FirstFrameImage,
			"last_frame_image": sb.LastFrameImage, "reference_images": sb.ReferenceImages,
			"video_url": sb.VideoURL, "tts_audio_url": sb.TTSAudioURL, "subtitle_url": sb.SubtitleURL,
			"composed_video_url": sb.ComposedVideoURL, "status": sb.Status,
			"character_ids": cids, "characters": chars,
			"created_at": sb.CreatedAt, "updated_at": sb.UpdatedAt,
		})
	}
	response.Success(c, out)
}

func (s *Server) pipelineStatus(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	ep, ok := requireEpisode(c, id)
	if !ok {
		return
	}
	var chars int64
	var scenes int64
	var sbs int64
	var withVideo int64
	var withTTS int64
	var composed int64
	organizationDB(c).Model(&models.EpisodeCharacter{}).Where("episode_id = ?", id).Count(&chars)
	organizationDB(c).Model(&models.EpisodeScene{}).Where("episode_id = ?", id).Count(&scenes)
	var ownedScenes int64
	organizationDB(c).Model(&models.Scene{}).Where("episode_id = ? AND deleted_at IS NULL", id).Count(&ownedScenes)
	if ownedScenes > scenes {
		scenes = ownedScenes
	}
	organizationDB(c).Model(&models.Storyboard{}).Where("episode_id = ? AND deleted_at IS NULL", id).Count(&sbs)
	organizationDB(c).Model(&models.Storyboard{}).Where("episode_id = ? AND video_url != '' AND video_url IS NOT NULL", id).Count(&withVideo)
	organizationDB(c).Model(&models.Storyboard{}).Where("episode_id = ? AND tts_audio_url != '' AND tts_audio_url IS NOT NULL", id).Count(&withTTS)
	organizationDB(c).Model(&models.Storyboard{}).Where("episode_id = ? AND composed_video_url != '' AND composed_video_url IS NOT NULL", id).Count(&composed)
	response.Success(c, gin.H{
		"episode_id":  id,
		"has_script":  ep.ScriptContent != "" || ep.Content != "",
		"characters":  chars,
		"scenes":      scenes,
		"storyboards": sbs,
		"with_video":  withVideo,
		"with_tts":    withTTS,
		"composed":    composed,
		"final_video": ep.VideoURL,
	})
}
