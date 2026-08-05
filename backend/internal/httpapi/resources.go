package httpapi

import (
	"errors"
	"fmt"
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
	g.POST("/:id/save-to-library", s.characterSaveToLibrary)
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

		ReferenceImages string `json:"reference_images"`
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
	if len([]rune(body.ReferenceImages)) > maxTextRunes {
		response.BadRequest(c, "character field is too long")
		return
	}
	if strings.TrimSpace(body.ReferenceImages) != "" {
		if err := validateReferenceMediaOwnership(c, body.ReferenceImages); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}

	var created models.Character
	err := organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var drama models.Drama
		if err := activeTx(tx).First(&drama, body.DramaID).Error; err != nil {
			return err
		}
		if body.EpisodeID != nil {
			var episode models.Episode
			if err := activeTx(tx).First(&episode, *body.EpisodeID).Error; err != nil {
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
			VoiceStyle: body.VoiceStyle, ReferenceImages: body.ReferenceImages, CreatedAt: now, UpdatedAt: now,
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
		if errors.Is(err, errOwnershipMismatch) {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "episode does not belong to drama"})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
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

func (s *Server) characterSaveToLibrary(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid character id")
		return
	}
	var ch models.Character
	if err := findActiveCharacter(c, uint(id), &ch); err != nil {
		response.NotFound(c, "character not found")
		return
	}
	now := response.Now()
	template := models.CharacterTemplate{
		OrganizationID:  currentOrganizationID(c),
		Name:            ch.Name,
		Role:            ch.Role,
		Description:     ch.Description,
		Appearance:      ch.Appearance,
		Personality:     ch.Personality,
		VoiceStyle:      ch.VoiceStyle,
		VoiceProvider:   ch.VoiceProvider,
		ImageURL:        ch.ImageURL,
		ReferenceImages: ch.ReferenceImages,
		LocalPath:       ch.LocalPath,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := organizationDB(c).Create(&template).Error; err != nil {
		response.ServerError(c, "failed to save character to library")
		return
	}
	response.Created(c, template)
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
	if err := findActiveCharacter(c, uint(id), &ch); err != nil {
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
	if err := findActiveCharacter(c, uint(id), &ch); err != nil {
		response.BadRequest(c, "Character not found")
		return
	}
	var ep models.Episode
	if body.EpisodeID == 0 {
		response.BadRequest(c, "episode_id is required")
		return
	}
	if err := findActiveEpisode(c, body.EpisodeID, &ep); err != nil {
		response.BadRequest(c, "Episode not found")
		return
	}
	if ep.DramaID != ch.DramaID {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "character does not belong to episode drama"})
		return
	}
	var drama models.Drama
	_ = findActiveDrama(c, ch.DramaID, &drama)
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
		ReferenceImages: ch.ReferenceImages,
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
	if err := findActiveEpisode(c, body.EpisodeID, &ep); err != nil {
		response.BadRequest(c, "Episode not found")
		return
	}
	ids := make([]uint, 0)
	errs := make([]string, 0)
	for _, cid := range body.CharacterIDs {
		var ch models.Character
		if err := findActiveCharacter(c, cid, &ch); err != nil {
			errs = append(errs, fmt.Sprintf("character %d: not found", cid))
			continue
		}
		if ch.DramaID != ep.DramaID {
			errs = append(errs, fmt.Sprintf("character %d: does not belong to episode drama", cid))
			continue
		}
		var drama models.Drama
		_ = findActiveDrama(c, ch.DramaID, &drama)
		resolution := prompttemplate.CharacterImagePrompt(organizationDB(c), currentOrganizationID(c), drama, ep, ch, "")
		prompt := strings.TrimSpace(resolution.Prompt)
		if prompt == "" {
			errs = append(errs, fmt.Sprintf("character %d: empty prompt", cid))
			continue
		}
		id := ch.ID
		did := ch.DramaID
		rec := &models.ImageGeneration{OrganizationID: currentOrganizationID(c), CharacterID: &id, DramaID: &did, Prompt: prompt, ImageType: "character", ReferenceImages: ch.ReferenceImages}
		if err := s.Images.Generate(c.Request.Context(), rec, ep.ImageConfigID); err != nil {
			errs = append(errs, fmt.Sprintf("character %d: %s", cid, err.Error()))
			continue
		}
		ids = append(ids, rec.ID)
	}
	response.Success(c, gin.H{"count": len(ids), "ids": ids, "errors": errs})
}
