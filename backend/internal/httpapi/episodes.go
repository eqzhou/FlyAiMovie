package httpapi

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (s *Server) registerEpisodes(api *gin.RouterGroup) {
	g := api.Group("/episodes")
	g.POST("", s.createEpisode)
	g.PUT("/:id", s.updateEpisode)
	g.DELETE("/:id", s.deleteEpisode)
	g.POST("/:id/copy", s.copyEpisode)
	g.POST("/:id/move", s.moveEpisode)
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
	if err := rejectUnknownFields(body, "content", "script_content", "title", "description", "status", "image_config_id", "video_config_id", "audio_config_id"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	var episode models.Episode
	if err := findActiveEpisode(c, uint(id), &episode); err != nil {
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

func (s *Server) deleteEpisode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid episode id")
		return
	}
	now := response.Now()
	err = organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var episode models.Episode
		if err := activeTx(tx).First(&episode, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&episode).Updates(map[string]any{
			"deleted_at": now,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		// Soft-delete dependents owned by this episode so they cannot keep mutating.
		if err := tx.Model(&models.Storyboard{}).Where("episode_id = ? AND deleted_at IS NULL", episode.ID).Updates(map[string]any{
			"deleted_at": now,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Scene{}).Where("episode_id = ? AND deleted_at IS NULL", episode.ID).Updates(map[string]any{
			"deleted_at": now,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		// Cancel active production runs so workers stop advancing a deleted episode.
		if err := tx.Model(&models.ProductionRun{}).Where("episode_id = ? AND status = ?", episode.ID, "queued").Updates(map[string]any{
			"status":              "canceled",
			"status_message":      "剧集已删除",
			"cancel_requested_at": now,
			"completed_at":        now,
			"lease_owner":         "",
			"lease_expires_at":    nil,
			"updated_at":          now,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.NotFound(c, "episode not found")
		return
	}
	if err != nil {
		response.ServerError(c, "failed to delete episode")
		return
	}
	response.Success(c, nil)
}

func (s *Server) copyEpisode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid episode id")
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	_ = bindOptionalJSON(c, &body)

	var created models.Episode
	err = organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var source models.Episode
		if err := activeTx(tx).First(&source, id).Error; err != nil {
			return err
		}
		var drama models.Drama
		if err := activeTx(tx).First(&drama, source.DramaID).Error; err != nil {
			return err
		}

		var existing []models.Episode
		if err := tx.Where("drama_id = ? AND deleted_at IS NULL", source.DramaID).Order("episode_number").Find(&existing).Error; err != nil {
			return err
		}
		next := 1
		for _, e := range existing {
			if e.EpisodeNumber >= next {
				next = e.EpisodeNumber + 1
			}
		}

		now := response.Now()
		title := strings.TrimSpace(body.Title)
		if title == "" {
			title = source.Title
			if title == "" {
				title = "第" + strconv.Itoa(next) + "集"
			}
			if !strings.HasSuffix(title, "（副本）") && !strings.HasSuffix(title, "(副本)") {
				title = title + "（副本）"
			}
		}
		if len([]rune(title)) > maxNameRunes {
			return fmt.Errorf("title is too long")
		}

		created = models.Episode{
			OrganizationID: currentOrganizationID(c),
			DramaID:        source.DramaID,
			EpisodeNumber:  next,
			Title:          title,
			Content:        source.Content,
			ScriptContent:  source.ScriptContent,
			Description:    source.Description,
			Duration:       0,
			Status:         "draft",
			ImageConfigID:  source.ImageConfigID,
			VideoConfigID:  source.VideoConfigID,
			AudioConfigID:  source.AudioConfigID,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}

		var charLinks []models.EpisodeCharacter
		if err := tx.Where("episode_id = ?", source.ID).Find(&charLinks).Error; err != nil {
			return err
		}
		for _, link := range charLinks {
			var character models.Character
			if err := activeTx(tx).First(&character, link.CharacterID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}
			if err := tx.Create(&models.EpisodeCharacter{
				OrganizationID: currentOrganizationID(c),
				EpisodeID:      created.ID,
				CharacterID:    character.ID,
				CreatedAt:      now,
			}).Error; err != nil {
				return err
			}
		}

		sceneIDMap := map[uint]uint{}
		var ownedScenes []models.Scene
		if err := tx.Where("episode_id = ? AND deleted_at IS NULL", source.ID).Order("id").Find(&ownedScenes).Error; err != nil {
			return err
		}
		for _, scene := range ownedScenes {
			oldID := scene.ID
			scene.ID = 0
			scene.OrganizationID = currentOrganizationID(c)
			scene.DramaID = created.DramaID
			epID := created.ID
			scene.EpisodeID = &epID
			scene.CreatedAt = now
			scene.UpdatedAt = now
			scene.DeletedAt = nil
			if err := tx.Create(&scene).Error; err != nil {
				return err
			}
			sceneIDMap[oldID] = scene.ID
		}

		var sceneLinks []models.EpisodeScene
		if err := tx.Where("episode_id = ?", source.ID).Find(&sceneLinks).Error; err != nil {
			return err
		}
		for _, link := range sceneLinks {
			if _, owned := sceneIDMap[link.SceneID]; owned {
				continue
			}
			var scene models.Scene
			if err := activeTx(tx).First(&scene, link.SceneID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}
			if err := tx.Create(&models.EpisodeScene{
				OrganizationID: currentOrganizationID(c),
				EpisodeID:      created.ID,
				SceneID:        scene.ID,
				CreatedAt:      now,
			}).Error; err != nil {
				return err
			}
		}

		var shots []models.Storyboard
		if err := tx.Where("episode_id = ? AND deleted_at IS NULL", source.ID).Order("storyboard_number").Find(&shots).Error; err != nil {
			return err
		}
		for _, shot := range shots {
			oldShotID := shot.ID
			var sceneID *uint
			if shot.SceneID != nil {
				if mapped, ok := sceneIDMap[*shot.SceneID]; ok {
					sceneID = &mapped
				} else {
					var shared models.Scene
					if err := activeTx(tx).First(&shared, *shot.SceneID).Error; err == nil {
						sceneID = shot.SceneID
					}
				}
			}
			copyShot := models.Storyboard{
				OrganizationID:   currentOrganizationID(c),
				EpisodeID:        created.ID,
				SceneID:          sceneID,
				StoryboardNumber: shot.StoryboardNumber,
				Title:            shot.Title,
				Location:         shot.Location,
				Time:             shot.Time,
				ShotType:         shot.ShotType,
				Angle:            shot.Angle,
				Movement:         shot.Movement,
				Action:           shot.Action,
				Result:           shot.Result,
				Atmosphere:       shot.Atmosphere,
				ImagePrompt:      shot.ImagePrompt,
				VideoPrompt:      shot.VideoPrompt,
				BGMPrompt:        shot.BGMPrompt,
				SoundEffect:      shot.SoundEffect,
				Dialogue:         shot.Dialogue,
				Description:      shot.Description,
				Duration:         shot.Duration,
				ReferenceImages:  shot.ReferenceImages,
				Status:           "pending",
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if err := tx.Create(&copyShot).Error; err != nil {
				return err
			}
			var sbChars []models.StoryboardCharacter
			if err := tx.Where("storyboard_id = ?", oldShotID).Find(&sbChars).Error; err != nil {
				return err
			}
			for _, link := range sbChars {
				var character models.Character
				if err := activeTx(tx).First(&character, link.CharacterID).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						continue
					}
					return err
				}
				if err := tx.Create(&models.StoryboardCharacter{
					OrganizationID: currentOrganizationID(c),
					StoryboardID:   copyShot.ID,
					CharacterID:    character.ID,
				}).Error; err != nil {
					return err
				}
			}
		}

		if drama.TotalEpisodes < next {
			if err := tx.Model(&drama).Updates(map[string]any{
				"total_episodes": next,
				"updated_at":     now,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.NotFound(c, "episode not found")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "too long") {
			response.BadRequest(c, err.Error())
			return
		}
		response.ServerError(c, "failed to copy episode")
		return
	}
	response.Created(c, gin.H{
		"id": created.ID, "episode_number": created.EpisodeNumber, "title": created.Title,
		"drama_id": created.DramaID, "status": created.Status,
		"image_config_id": created.ImageConfigID, "video_config_id": created.VideoConfigID, "audio_config_id": created.AudioConfigID,
	})
}

func (s *Server) moveEpisode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid episode id")
		return
	}
	var body struct {
		Direction string `json:"direction"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	direction := strings.ToLower(strings.TrimSpace(body.Direction))
	if direction != "up" && direction != "down" {
		response.BadRequest(c, "direction must be up or down")
		return
	}

	err = organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var current models.Episode
		if err := activeTx(tx).First(&current, id).Error; err != nil {
			return err
		}
		var sibling models.Episode
		q := activeTx(tx).Where("drama_id = ?", current.DramaID)
		if direction == "up" {
			err = q.Where("episode_number < ?", current.EpisodeNumber).Order("episode_number desc").First(&sibling).Error
		} else {
			err = q.Where("episode_number > ?", current.EpisodeNumber).Order("episode_number asc").First(&sibling).Error
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("episode already at boundary")
		}
		if err != nil {
			return err
		}
		now := response.Now()
		// Capture numbers before Updates, because GORM mutates the model in memory.
		currentNumber := current.EpisodeNumber
		siblingNumber := sibling.EpisodeNumber
		// temporary number avoids unique collisions if a composite unique index exists
		temp := -int(current.ID)
		if err := tx.Model(&current).Updates(map[string]any{"episode_number": temp, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&sibling).Updates(map[string]any{"episode_number": currentNumber, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&current).Updates(map[string]any{"episode_number": siblingNumber, "updated_at": now}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.NotFound(c, "episode not found")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "boundary") {
			response.BadRequest(c, err.Error())
			return
		}
		response.ServerError(c, "failed to move episode")
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
