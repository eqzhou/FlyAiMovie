package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/prompttemplate"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var errOwnershipMismatch = errors.New("resource ownership mismatch")

func (s *Server) registerCharacters(api *gin.RouterGroup) {
	g := api.Group("/characters")
	g.POST("", s.createCharacter)
	g.PUT("/:id", s.updateCharacter)
	g.DELETE("/:id", s.deleteCharacter)
	g.POST("/:id/generate-voice-sample", s.characterVoiceSample)
	g.POST("/:id/generate-image", s.characterGenerateImage)
	g.POST("/batch-generate-images", s.characterBatchImages)
}

func (s *Server) createCharacter(c *gin.Context) {
	var body struct {
		DramaID     uint   `json:"drama_id"`
		EpisodeID   *uint  `json:"episode_id"`
		Name        string `json:"name"`
		Role        string `json:"role"`
		Description string `json:"description"`
		Appearance  string `json:"appearance"`
		Personality string `json:"personality"`
		VoiceStyle  string `json:"voice_style"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.DramaID == 0 || strings.TrimSpace(body.Name) == "" {
		response.BadRequest(c, "drama_id and name are required")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if len([]rune(body.Name)) > 200 {
		response.BadRequest(c, "character name is too long")
		return
	}

	var created models.Character
	err := organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var drama models.Drama
		if err := tx.First(&drama, body.DramaID).Error; err != nil {
			return err
		}
		if body.EpisodeID != nil {
			var episode models.Episode
			if err := tx.First(&episode, *body.EpisodeID).Error; err != nil {
				return err
			}
			if episode.DramaID != body.DramaID {
				return errOwnershipMismatch
			}
		}
		now := response.Now()
		created = models.Character{
			OrganizationID: currentOrganizationID(c), DramaID: body.DramaID, Name: body.Name, Role: strings.TrimSpace(body.Role),
			Description: body.Description, Appearance: body.Appearance, Personality: body.Personality,
			VoiceStyle: body.VoiceStyle, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		if body.EpisodeID != nil {
			return tx.Create(&models.EpisodeCharacter{OrganizationID: currentOrganizationID(c), EpisodeID: *body.EpisodeID, CharacterID: created.ID, CreatedAt: now}).Error
		}
		return nil
	})
	if err != nil {
		if err == errOwnershipMismatch {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "episode does not belong to drama"})
			return
		}
		if err == gorm.ErrRecordNotFound {
			response.BadRequest(c, "drama or episode not found")
			return
		}
		response.ServerError(c, "failed to create character")
		return
	}
	response.Created(c, created)
}

func (s *Server) updateCharacter(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid character id")
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if err := rejectUnknownFields(body, "name", "role", "description", "appearance", "personality", "voice_style", "voiceStyle", "voice_provider", "image_url", "local_path", "reference_images", "seed_value", "sort_order"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	updates := map[string]any{"updated_at": response.Now()}
	fields := map[string]string{
		"name": "name", "role": "role", "description": "description", "appearance": "appearance",
		"personality": "personality", "voice_style": "voice_style", "voiceStyle": "voice_style",
		"voice_provider": "voice_provider", "image_url": "image_url", "local_path": "local_path",
		"reference_images": "reference_images", "seed_value": "seed_value",
	}
	for k, col := range fields {
		maxRunes := maxTextRunes
		if col == "name" {
			maxRunes = maxNameRunes
		}
		v, ok, err := stringUpdate(body, k, maxRunes)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		if ok {
			if col == "name" {
				v = strings.TrimSpace(v)
				if v == "" {
					response.BadRequest(c, "name must not be empty")
					return
				}
			}
			if (col == "image_url" || col == "local_path") && v != "" {
				if err := validateLocalMediaOwnership(c, v); err != nil {
					response.BadRequest(c, err.Error())
					return
				}
			}
			if col == "reference_images" && v != "" {
				if err := validateReferenceMediaOwnership(c, v); err != nil {
					response.BadRequest(c, err.Error())
					return
				}
			}
			updates[col] = v
			if col == "voice_style" {
				updates["voice_sample_url"] = ""
			}
		}
	}
	if value, ok := body["sort_order"]; ok {
		sortOrder, valid := nonNegativeJSONInt(value)
		if !valid {
			response.BadRequest(c, "sort_order must be a non-negative integer")
			return
		}
		updates["sort_order"] = sortOrder
	}
	if len(updates) == 1 {
		response.BadRequest(c, "at least one character field is required")
		return
	}
	result := organizationDB(c).Model(&models.Character{}).Where("id = ? AND deleted_at IS NULL", id).Updates(updates)
	if result.RowsAffected == 0 {
		response.NotFound(c, "character not found")
		return
	}
	response.Success(c, nil)
}

func (s *Server) deleteCharacter(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	now := response.Now()
	result := organizationDB(c).Model(&models.Character{}).Where("id = ? AND deleted_at IS NULL", id).Update("deleted_at", now)
	if result.RowsAffected == 0 {
		response.NotFound(c, "character not found")
		return
	}
	response.Success(c, nil)
}

func (s *Server) characterVoiceSample(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var body struct {
		EpisodeID uint `json:"episode_id"`
	}
	if err := bindOptionalJSON(c, &body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	var ch models.Character
	if err := organizationDB(c).First(&ch, id).Error; err != nil {
		response.BadRequest(c, "Character not found")
		return
	}
	if ch.VoiceStyle == "" {
		response.BadRequest(c, "请先分配音色")
		return
	}
	var ep models.Episode
	if body.EpisodeID > 0 {
		if err := organizationDB(c).Where("id = ? AND drama_id = ?", body.EpisodeID, ch.DramaID).First(&ep).Error; err != nil {
			response.BadRequest(c, "episode does not belong to character drama")
			return
		}
	}
	url, err := s.TTS.GenerateVoiceSampleOrganization(c.Request.Context(), currentOrganizationID(c), ch.Name, ch.VoiceStyle, ep.AudioConfigID)
	if err != nil {
		response.BadRequest(c, "TTS 生成失败: "+err.Error())
		return
	}
	organizationDB(c).Model(&ch).Updates(map[string]any{"voice_sample_url": url, "updated_at": response.Now()})
	response.Success(c, gin.H{"voice_sample_url": url})
}

func (s *Server) characterGenerateImage(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var body struct {
		EpisodeID uint `json:"episode_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	var ch models.Character
	if err := organizationDB(c).First(&ch, id).Error; err != nil {
		response.BadRequest(c, "Character not found")
		return
	}
	var ep models.Episode
	if body.EpisodeID == 0 {
		response.BadRequest(c, "episode_id is required")
		return
	}
	if err := organizationDB(c).First(&ep, body.EpisodeID).Error; err != nil {
		response.BadRequest(c, "Episode not found")
		return
	}
	if ep.DramaID != ch.DramaID {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "character does not belong to episode drama"})
		return
	}
	var drama models.Drama
	organizationDB(c).First(&drama, ch.DramaID)
	resolution := prompttemplate.CharacterImagePrompt(organizationDB(c), currentOrganizationID(c), drama, ep, ch, "")
	prompt := strings.TrimSpace(resolution.Prompt)
	if prompt == "" {
		response.BadRequest(c, "prompt empty")
		return
	}
	cid := ch.ID
	did := ch.DramaID
	rec := &models.ImageGeneration{
		OrganizationID: currentOrganizationID(c), CharacterID: &cid, DramaID: &did, Prompt: prompt, ImageType: "character", Status: "pending",
	}
	if err := s.Images.Generate(c.Request.Context(), rec, ep.ImageConfigID); err != nil {
		respondGenerationError(c, err)
		return
	}
	response.Success(c, gin.H{"image_generation_id": rec.ID})
}

func (s *Server) characterBatchImages(c *gin.Context) {
	var body struct {
		CharacterIDs []uint `json:"character_ids"`
		EpisodeID    uint   `json:"episode_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.EpisodeID == 0 {
		response.BadRequest(c, "episode_id is required")
		return
	}
	var ep models.Episode
	if err := organizationDB(c).First(&ep, body.EpisodeID).Error; err != nil {
		response.BadRequest(c, "Episode not found")
		return
	}
	ids := make([]uint, 0)
	for _, cid := range body.CharacterIDs {
		var ch models.Character
		if err := organizationDB(c).First(&ch, cid).Error; err != nil {
			continue
		}
		if ch.DramaID != ep.DramaID {
			continue
		}
		var drama models.Drama
		organizationDB(c).First(&drama, ch.DramaID)
		resolution := prompttemplate.CharacterImagePrompt(organizationDB(c), currentOrganizationID(c), drama, ep, ch, "")
		prompt := strings.TrimSpace(resolution.Prompt)
		if prompt == "" {
			continue
		}
		id := ch.ID
		did := ch.DramaID
		rec := &models.ImageGeneration{OrganizationID: currentOrganizationID(c), CharacterID: &id, DramaID: &did, Prompt: prompt, ImageType: "character"}
		if err := s.Images.Generate(c.Request.Context(), rec, ep.ImageConfigID); err == nil {
			ids = append(ids, rec.ID)
		}
	}
	response.Success(c, gin.H{"count": len(ids), "ids": ids})
}

func (s *Server) registerScenes(api *gin.RouterGroup) {
	g := api.Group("/scenes")
	g.POST("", s.createScene)
	g.PUT("/:id", s.updateScene)
	g.POST("/:id/generate-image", s.sceneGenerateImage)
	g.POST("/:id/copy", s.copyScene)
	g.POST("/:id/move", s.moveScene)
	g.DELETE("/:id", s.deleteScene)
}

func (s *Server) createScene(c *gin.Context) {
	var body struct {
		DramaID   uint   `json:"drama_id"`
		EpisodeID *uint  `json:"episode_id"`
		Location  string `json:"location"`
		Time      string `json:"time"`
		Prompt    string `json:"prompt"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.DramaID == 0 || strings.TrimSpace(body.Location) == "" {
		response.BadRequest(c, "drama_id and location required")
		return
	}
	body.Location = strings.TrimSpace(body.Location)
	if len([]rune(body.Location)) > maxNameRunes || len([]rune(body.Time)) > maxNameRunes || len([]rune(body.Prompt)) > maxTextRunes {
		response.BadRequest(c, "scene field is too long")
		return
	}
	var sc models.Scene
	err := organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var drama models.Drama
		if err := tx.First(&drama, body.DramaID).Error; err != nil {
			return err
		}
		if body.EpisodeID != nil {
			var episode models.Episode
			if err := tx.First(&episode, *body.EpisodeID).Error; err != nil {
				return err
			}
			if episode.DramaID != body.DramaID {
				return errOwnershipMismatch
			}
		}
		ts := response.Now()
		prompt := body.Prompt
		if prompt == "" {
			prompt = body.Location
		}
		sc = models.Scene{
			OrganizationID: currentOrganizationID(c), DramaID: body.DramaID, EpisodeID: body.EpisodeID, Location: strings.TrimSpace(body.Location), Time: body.Time,
			Prompt: prompt, Status: "pending", CreatedAt: ts, UpdatedAt: ts,
		}
		return tx.Create(&sc).Error
	})
	if err != nil {
		if errors.Is(err, errOwnershipMismatch) {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "episode does not belong to drama"})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.BadRequest(c, "drama or episode not found")
			return
		}
		response.ServerError(c, err.Error())
		return
	}
	response.Created(c, sc)
}

func (s *Server) updateScene(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid scene id")
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if err := rejectUnknownFields(body, "location", "time", "prompt"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	updates := map[string]any{"updated_at": response.Now()}
	for _, k := range []string{"location", "time", "prompt"} {
		maxRunes := maxTextRunes
		if k == "location" || k == "time" {
			maxRunes = maxNameRunes
		}
		v, ok, err := stringUpdate(body, k, maxRunes)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		if ok {
			if k == "location" {
				v = strings.TrimSpace(v)
				if v == "" {
					response.BadRequest(c, "location must not be empty")
					return
				}
			}
			updates[k] = v
		}
	}
	if len(updates) == 1 {
		response.BadRequest(c, "at least one scene field is required")
		return
	}
	result := organizationDB(c).Model(&models.Scene{}).Where("id = ? AND deleted_at IS NULL", id).Updates(updates)
	if result.RowsAffected == 0 {
		response.NotFound(c, "scene not found")
		return
	}
	response.Success(c, nil)
}

func (s *Server) sceneGenerateImage(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var body struct {
		EpisodeID uint `json:"episode_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	var sc models.Scene
	if err := organizationDB(c).First(&sc, id).Error; err != nil {
		response.BadRequest(c, "Scene not found")
		return
	}
	if body.EpisodeID == 0 {
		response.BadRequest(c, "episode_id is required")
		return
	}
	var ep models.Episode
	if err := organizationDB(c).First(&ep, body.EpisodeID).Error; err != nil {
		response.BadRequest(c, "Episode not found")
		return
	}
	if ep.DramaID != sc.DramaID {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "scene does not belong to episode drama"})
		return
	}
	var drama models.Drama
	organizationDB(c).First(&drama, sc.DramaID)
	resolution := prompttemplate.SceneImagePrompt(organizationDB(c), currentOrganizationID(c), drama, ep, sc, "")
	prompt := strings.TrimSpace(resolution.Prompt)
	if prompt == "" {
		response.BadRequest(c, "prompt empty")
		return
	}
	sid := sc.ID
	did := sc.DramaID
	rec := &models.ImageGeneration{OrganizationID: currentOrganizationID(c), SceneID: &sid, DramaID: &did, Prompt: prompt, ImageType: "scene"}
	organizationDB(c).Model(&sc).Updates(map[string]any{"status": "processing", "updated_at": response.Now()})
	if err := s.Images.Generate(c.Request.Context(), rec, ep.ImageConfigID); err != nil {
		organizationDB(c).Model(&sc).Updates(map[string]any{"status": "failed", "updated_at": response.Now()})
		respondGenerationError(c, err)
		return
	}
	response.Success(c, gin.H{"image_generation_id": rec.ID})
}

func (s *Server) deleteScene(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	result := organizationDB(c).Model(&models.Scene{}).Where("id = ? AND deleted_at IS NULL", id).Update("deleted_at", response.Now())
	if result.RowsAffected == 0 {
		response.NotFound(c, "scene not found")
		return
	}
	response.Success(c, nil)
}

func (s *Server) registerStoryboards(api *gin.RouterGroup) {
	g := api.Group("/storyboards")
	g.POST("", s.createStoryboard)
	g.PUT("/:id", s.updateStoryboard)
	g.DELETE("/:id", s.deleteStoryboard)
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
		if err := tx.First(&episode, epID).Error; err != nil {
			return err
		}
		if err := validateStoryboardResources(tx, episode.DramaID, sceneID, characterIDs); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&models.Storyboard{}).Where("episode_id = ?", epID).Count(&count).Error; err != nil {
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
		if err := tx.First(&storyboard, id).Error; err != nil {
			return err
		}
		var episode models.Episode
		if err := tx.First(&episode, storyboard.EpisodeID).Error; err != nil {
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

func (s *Server) storyboardTTS(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var sb models.Storyboard
	if err := organizationDB(c).First(&sb, id).Error; err != nil {
		response.BadRequest(c, "not found")
		return
	}
	var ep models.Episode
	organizationDB(c).First(&ep, sb.EpisodeID)
	job, err := s.Jobs.CreateQueuedOrganization(currentOrganizationID(c), "tts.generate", "storyboard_tts", uint(id), "", ep.AudioConfigID)
	if err != nil {
		respondGenerationError(c, err)
		return
	}
	response.Success(c, gin.H{"job_id": job.ID, "status": job.Status})
}

func uniquePositiveIDs(value any) []uint {
	items, _ := value.([]any)
	seen := make(map[uint]struct{}, len(items))
	result := make([]uint, 0, len(items))
	for _, item := range items {
		id := asUint(item)
		if id == 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func validateStoryboardResources(tx *gorm.DB, dramaID, sceneID uint, characterIDs []uint) error {
	if sceneID > 0 {
		var scene models.Scene
		if err := tx.Where("id = ? AND deleted_at IS NULL", sceneID).First(&scene).Error; err != nil {
			return err
		}
		if scene.DramaID != dramaID {
			return errOwnershipMismatch
		}
	}
	for _, characterID := range characterIDs {
		var character models.Character
		if err := tx.Where("id = ? AND deleted_at IS NULL", characterID).First(&character).Error; err != nil {
			return err
		}
		if character.DramaID != dramaID {
			return errOwnershipMismatch
		}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}

func asUint(v any) uint { return uint(asInt(v)) }
