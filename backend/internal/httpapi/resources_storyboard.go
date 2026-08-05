package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (s *Server) registerStoryboards(api *gin.RouterGroup) {
	g := api.Group("/storyboards")
	g.POST("", s.createStoryboard)
	g.PUT("/:id", s.updateStoryboard)
	g.DELETE("/:id", s.deleteStoryboard)
	g.POST("/:id/copy", s.copyStoryboard)
	g.POST("/:id/move", s.moveStoryboard)
	g.POST("/:id/generate-tts", s.storyboardTTS)
}

func (s *Server) createStoryboard(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	episodeNumber, valid := positiveJSONInt(body["episode_id"])
	if !valid {
		response.BadRequest(c, "episode_id required")
		return
	}
	epID := uint(episodeNumber)
	characterIDs := []uint(nil)
	if value, exists := body["character_ids"]; exists {
		characterIDs, valid = positiveJSONIDs(value)
		if !valid {
			response.BadRequest(c, "character_ids must contain only positive integer ids")
			return
		}
	}
	sceneID := uint(0)
	if value, exists := body["scene_id"]; exists && value != nil {
		number, valid := positiveJSONInt(value)
		if !valid {
			response.BadRequest(c, "scene_id must be a positive integer or null")
			return
		}
		sceneID = uint(number)
	}
	duration := 12
	if value, exists := body["duration"]; exists {
		duration, valid = positiveJSONInt(value)
		if !valid || duration > 3600 {
			response.BadRequest(c, "duration must be an integer between 1 and 3600")
			return
		}
	}
	stringFields := map[string]string{}
	for _, key := range []string{"title", "location", "time", "shot_type", "angle", "movement", "action", "result",
		"atmosphere", "image_prompt", "video_prompt", "bgm_prompt", "sound_effect", "dialogue", "description", "reference_images"} {
		value, exists, fieldErr := stringUpdate(body, key, maxTextRunes)
		if fieldErr != nil {
			response.BadRequest(c, fieldErr.Error())
			return
		}
		if exists {
			stringFields[key] = value
		}
	}
	if references := stringFields["reference_images"]; references != "" {
		if err := validateReferenceMediaOwnership(c, references); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	var sb models.Storyboard
	err := organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var episode models.Episode
		if err := activeTx(tx).First(&episode, epID).Error; err != nil {
			return err
		}
		if err := validateStoryboardResources(tx, episode.DramaID, sceneID, characterIDs); err != nil {
			return err
		}
		var count int64
		if err := activeTx(tx).Model(&models.Storyboard{}).Where("episode_id = ?", epID).Count(&count).Error; err != nil {
			return err
		}
		ts := response.Now()
		sb = models.Storyboard{
			OrganizationID: currentOrganizationID(c), EpisodeID: epID, StoryboardNumber: int(count) + 1,
			Title: stringFields["title"], Location: stringFields["location"], Time: stringFields["time"],
			ShotType: stringFields["shot_type"], Angle: stringFields["angle"], Movement: stringFields["movement"],
			Action: stringFields["action"], Result: stringFields["result"], Atmosphere: stringFields["atmosphere"],
			ImagePrompt: stringFields["image_prompt"], VideoPrompt: stringFields["video_prompt"],
			BGMPrompt: stringFields["bgm_prompt"], SoundEffect: stringFields["sound_effect"],
			Dialogue: stringFields["dialogue"], Description: stringFields["description"],
			ReferenceImages: stringFields["reference_images"],
			Duration:        duration, Status: "pending", CreatedAt: ts, UpdatedAt: ts,
		}
		if sceneID > 0 {
			sb.SceneID = &sceneID
		}
		if err := tx.Create(&sb).Error; err != nil {
			return err
		}
		for _, characterID := range characterIDs {
			if err := tx.Create(&models.StoryboardCharacter{OrganizationID: currentOrganizationID(c), StoryboardID: sb.ID, CharacterID: characterID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errOwnershipMismatch) {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "scene or character does not belong to episode drama"})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.BadRequest(c, "episode, scene or character not found")
			return
		}
		response.ServerError(c, "failed to create storyboard")
		return
	}
	response.Created(c, sb)
}

func (s *Server) updateStoryboard(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid storyboard id")
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	updates := map[string]any{"updated_at": response.Now()}
	stringKeys := []string{"title", "location", "time", "shot_type", "angle", "movement", "action", "result",
		"atmosphere", "image_prompt", "video_prompt", "bgm_prompt", "sound_effect", "dialogue",
		"description", "status", "first_frame_image", "last_frame_image", "composed_image",
		"video_url", "tts_audio_url", "reference_images"}
	allowedFields := append([]string{"duration", "character_ids", "scene_id"}, stringKeys...)
	if err := rejectUnknownFields(body, allowedFields...); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	for _, k := range stringKeys {
		v, ok, fieldErr := stringUpdate(body, k, maxTextRunes)
		if fieldErr != nil {
			response.BadRequest(c, fieldErr.Error())
			return
		}
		if ok {
			if k == "reference_images" {
				if err := validateReferenceMediaOwnership(c, v); err != nil {
					response.BadRequest(c, err.Error())
					return
				}
			} else if (k == "first_frame_image" || k == "last_frame_image" || k == "composed_image" || k == "video_url" || k == "tts_audio_url") && v != "" {
				if err := validateLocalMediaOwnership(c, v); err != nil {
					response.BadRequest(c, err.Error())
					return
				}
			}
			updates[k] = v
		}
	}
	if value, ok := body["duration"]; ok {
		duration, valid := positiveJSONInt(value)
		if !valid || duration > 3600 {
			response.BadRequest(c, "duration must be an integer between 1 and 3600")
			return
		}
		updates["duration"] = duration
	}
	if _, ok := body["character_ids"]; ok {
		if _, valid := body["character_ids"].([]any); !valid {
			response.BadRequest(c, "character_ids must be an array")
			return
		}
	}
	characterValue, replaceCharacters := body["character_ids"]
	characterIDs := []uint(nil)
	if replaceCharacters {
		var valid bool
		characterIDs, valid = positiveJSONIDs(characterValue)
		if !valid {
			response.BadRequest(c, "character_ids must contain only positive integer ids")
			return
		}
	}
	if value, hasScene := body["scene_id"]; hasScene && value != nil {
		if _, valid := positiveJSONInt(value); !valid {
			response.BadRequest(c, "scene_id must be a positive integer or null")
			return
		}
	}
	if len(updates) == 1 && !replaceCharacters {
		if _, hasScene := body["scene_id"]; !hasScene {
			response.BadRequest(c, "at least one storyboard field is required")
			return
		}
	}
	err = organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var storyboard models.Storyboard
		if err := activeTx(tx).First(&storyboard, id).Error; err != nil {
			return err
		}
		var episode models.Episode
		if err := activeTx(tx).First(&episode, storyboard.EpisodeID).Error; err != nil {
			return err
		}
		sceneID := uint(0)
		if storyboard.SceneID != nil {
			sceneID = *storyboard.SceneID
		}
		if value, ok := body["scene_id"]; ok {
			if value == nil {
				sceneID = 0
				updates["scene_id"] = nil
			} else {
				number, _ := positiveJSONInt(value)
				sceneID = uint(number)
				updates["scene_id"] = sceneID
			}
		}
		if err := validateStoryboardResources(tx, episode.DramaID, sceneID, characterIDs); err != nil && (sceneID > 0 || replaceCharacters) {
			return err
		}
		if err := tx.Model(&storyboard).Updates(updates).Error; err != nil {
			return err
		}
		if replaceCharacters {
			if err := tx.Where("storyboard_id = ?", id).Delete(&models.StoryboardCharacter{}).Error; err != nil {
				return err
			}
			for _, characterID := range characterIDs {
				if err := tx.Create(&models.StoryboardCharacter{OrganizationID: currentOrganizationID(c), StoryboardID: uint(id), CharacterID: characterID}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errOwnershipMismatch) {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "scene or character does not belong to storyboard drama"})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(c, "storyboard, scene or character not found")
			return
		}
		response.ServerError(c, "failed to update storyboard")
		return
	}
	response.Success(c, nil)
}

func (s *Server) deleteStoryboard(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	now := response.Now()
	result := organizationDB(c).Model(&models.Storyboard{}).Where("id = ? AND deleted_at IS NULL", id).Update("deleted_at", now)
	if result.RowsAffected == 0 {
		response.NotFound(c, "storyboard not found")
		return
	}
	response.Success(c, nil)
}

func (s *Server) copyStoryboard(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid storyboard id")
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	_ = bindOptionalJSON(c, &body)

	var created models.Storyboard
	err = organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var source models.Storyboard
		if err := activeTx(tx).First(&source, id).Error; err != nil {
			return err
		}
		var episode models.Episode
		if err := activeTx(tx).First(&episode, source.EpisodeID).Error; err != nil {
			return err
		}

		var maxNumber int
		if err := activeTx(tx).Model(&models.Storyboard{}).Where("episode_id = ?", source.EpisodeID).
			Select("COALESCE(MAX(storyboard_number), 0)").Scan(&maxNumber).Error; err != nil {
			return err
		}

		now := response.Now()
		title := strings.TrimSpace(body.Title)
		if title == "" {
			title = source.Title
			if title == "" {
				title = fmt.Sprintf("镜头 %d", maxNumber+1)
			}
			if !strings.HasSuffix(title, "（副本）") && !strings.HasSuffix(title, "(副本)") {
				title = title + "（副本）"
			}
		}
		if len([]rune(title)) > maxNameRunes {
			return fmt.Errorf("title is too long")
		}

		// Copy content/settings only; generated media stays empty on the new shot.
		created = models.Storyboard{
			OrganizationID:   currentOrganizationID(c),
			EpisodeID:        source.EpisodeID,
			SceneID:          source.SceneID,
			StoryboardNumber: maxNumber + 1,
			Title:            title,
			Location:         source.Location,
			Time:             source.Time,
			ShotType:         source.ShotType,
			Angle:            source.Angle,
			Movement:         source.Movement,
			Action:           source.Action,
			Result:           source.Result,
			Atmosphere:       source.Atmosphere,
			ImagePrompt:      source.ImagePrompt,
			VideoPrompt:      source.VideoPrompt,
			BGMPrompt:        source.BGMPrompt,
			SoundEffect:      source.SoundEffect,
			Dialogue:         source.Dialogue,
			Description:      source.Description,
			Duration:         source.Duration,
			ReferenceImages:  source.ReferenceImages,
			Status:           "pending",
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}

		var links []models.StoryboardCharacter
		if err := tx.Where("storyboard_id = ?", source.ID).Find(&links).Error; err != nil {
			return err
		}
		for _, link := range links {
			if err := tx.Create(&models.StoryboardCharacter{
				OrganizationID: currentOrganizationID(c),
				StoryboardID:   created.ID,
				CharacterID:    link.CharacterID,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.NotFound(c, "storyboard not found")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "too long") {
			response.BadRequest(c, err.Error())
			return
		}
		response.ServerError(c, "failed to copy storyboard")
		return
	}
	response.Created(c, created)
}

func (s *Server) moveStoryboard(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid storyboard id")
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
		var current models.Storyboard
		if err := activeTx(tx).First(&current, id).Error; err != nil {
			return err
		}
		var sibling models.Storyboard
		q := activeTx(tx).Where("episode_id = ?", current.EpisodeID)
		if direction == "up" {
			err = q.Where("storyboard_number < ?", current.StoryboardNumber).Order("storyboard_number desc").First(&sibling).Error
		} else {
			err = q.Where("storyboard_number > ?", current.StoryboardNumber).Order("storyboard_number asc").First(&sibling).Error
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("storyboard already at boundary")
		}
		if err != nil {
			return err
		}
		now := response.Now()
		// Capture numbers before Updates, because GORM mutates the model in memory.
		currentNumber := current.StoryboardNumber
		siblingNumber := sibling.StoryboardNumber
		temp := -int(current.ID)
		if err := tx.Model(&current).Updates(map[string]any{"storyboard_number": temp, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&sibling).Updates(map[string]any{"storyboard_number": currentNumber, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&current).Updates(map[string]any{"storyboard_number": siblingNumber, "updated_at": now}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.NotFound(c, "storyboard not found")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "boundary") {
			response.BadRequest(c, err.Error())
			return
		}
		response.ServerError(c, "failed to move storyboard")
		return
	}
	response.Success(c, nil)
}

func (s *Server) storyboardTTS(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var sb models.Storyboard
	if err := findActiveStoryboard(c, uint(id), &sb); err != nil {
		response.BadRequest(c, "not found")
		return
	}
	var ep models.Episode
	_ = findActiveEpisode(c, sb.EpisodeID, &ep)
	job, err := s.Jobs.CreateQueuedOrganization(currentOrganizationID(c), "tts.generate", "storyboard_tts", uint(id), "", ep.AudioConfigID)
	if err != nil {
		respondGenerationError(c, err)
		return
	}
	response.Success(c, gin.H{"job_id": job.ID, "status": job.Status})
}
