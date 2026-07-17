package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (s *Server) registerCharacterLibrary(api *gin.RouterGroup) {
	g := api.Group("/character-library")
	g.GET("", s.listCharacterTemplates)
	g.POST("", s.createCharacterTemplate)
	g.PUT("/:id", s.updateCharacterTemplate)
	g.DELETE("/:id", s.deleteCharacterTemplate)
	g.POST("/:id/import", s.importCharacterTemplate)
}

func (s *Server) listCharacterTemplates(c *gin.Context) {
	var rows []models.CharacterTemplate
	if err := organizationDB(c).Where("deleted_at IS NULL").Order("updated_at desc").Find(&rows).Error; err != nil {
		response.ServerError(c, "failed to list character templates")
		return
	}
	response.Success(c, rows)
}

func (s *Server) createCharacterTemplate(c *gin.Context) {
	var body models.CharacterTemplate
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		response.BadRequest(c, "name is required")
		return
	}
	if len([]rune(body.Name)) > maxNameRunes || len([]rune(body.Description)) > maxTextRunes || len([]rune(body.Appearance)) > maxTextRunes {
		response.BadRequest(c, "character template field is too long")
		return
	}
	if err := validateLocalMediaOwnership(c, body.ImageURL, body.LocalPath); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := validateReferenceMediaOwnership(c, body.ReferenceImages); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	now := response.Now()
	template := models.CharacterTemplate{
		OrganizationID: currentOrganizationID(c), Name: strings.TrimSpace(body.Name), Role: body.Role,
		Description: body.Description, Appearance: body.Appearance, Personality: body.Personality,
		VoiceStyle: body.VoiceStyle, VoiceProvider: body.VoiceProvider, ImageURL: body.ImageURL,
		ReferenceImages: body.ReferenceImages, LocalPath: body.LocalPath, CreatedAt: now, UpdatedAt: now,
	}
	if err := organizationDB(c).Create(&template).Error; err != nil {
		response.ServerError(c, "failed to create character template")
		return
	}
	response.Created(c, template)
}

func (s *Server) updateCharacterTemplate(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid character template id")
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	updates := map[string]any{"updated_at": response.Now()}
	for _, field := range []string{"name", "role", "description", "appearance", "personality", "voice_style", "voice_provider", "image_url", "reference_images", "local_path"} {
		limit := maxTextRunes
		if field == "name" || field == "role" || field == "voice_style" || field == "voice_provider" {
			limit = maxNameRunes
		}
		value, ok, fieldErr := stringUpdate(body, field, limit)
		if fieldErr != nil {
			response.BadRequest(c, fieldErr.Error())
			return
		}
		if ok {
			if field == "name" && strings.TrimSpace(value) == "" {
				response.BadRequest(c, "name must not be empty")
				return
			}
			if (field == "image_url" || field == "local_path") && value != "" {
				if err := validateLocalMediaOwnership(c, value); err != nil {
					response.BadRequest(c, err.Error())
					return
				}
			}
			if field == "reference_images" {
				if err := validateReferenceMediaOwnership(c, value); err != nil {
					response.BadRequest(c, err.Error())
					return
				}
			}
			updates[field] = strings.TrimSpace(value)
		}
	}
	if len(updates) == 1 {
		response.BadRequest(c, "at least one template field is required")
		return
	}
	result := organizationDB(c).Model(&models.CharacterTemplate{}).Where("id = ? AND deleted_at IS NULL", id).Updates(updates)
	if result.RowsAffected == 0 {
		response.NotFound(c, "character template not found")
		return
	}
	response.Success(c, nil)
}

func (s *Server) deleteCharacterTemplate(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid character template id")
		return
	}
	result := organizationDB(c).Model(&models.CharacterTemplate{}).Where("id = ? AND deleted_at IS NULL", id).Update("deleted_at", response.Now())
	if result.RowsAffected == 0 {
		response.NotFound(c, "character template not found")
		return
	}
	response.Success(c, nil)
}

func (s *Server) importCharacterTemplate(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid character template id")
		return
	}
	var body struct {
		DramaID   uint  `json:"drama_id"`
		EpisodeID *uint `json:"episode_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.DramaID == 0 {
		response.BadRequest(c, "drama_id is required")
		return
	}
	var created models.Character
	err = organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var template models.CharacterTemplate
		if err := tx.Where("id = ? AND deleted_at IS NULL", id).First(&template).Error; err != nil {
			return err
		}
		var drama models.Drama
		if err := tx.Where("id = ? AND deleted_at IS NULL", body.DramaID).First(&drama).Error; err != nil {
			return err
		}
		if body.EpisodeID != nil {
			var episode models.Episode
			if err := tx.Where("id = ? AND drama_id = ? AND deleted_at IS NULL", *body.EpisodeID, body.DramaID).First(&episode).Error; err != nil {
				return errOwnershipMismatch
			}
		}
		now := response.Now()
		created = models.Character{OrganizationID: currentOrganizationID(c), DramaID: body.DramaID, Name: template.Name,
			Role: template.Role, Description: template.Description, Appearance: template.Appearance, Personality: template.Personality,
			VoiceStyle: template.VoiceStyle, VoiceProvider: template.VoiceProvider, ImageURL: template.ImageURL,
			ReferenceImages: template.ReferenceImages, LocalPath: template.LocalPath, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		if body.EpisodeID != nil {
			return tx.Create(&models.EpisodeCharacter{OrganizationID: currentOrganizationID(c), EpisodeID: *body.EpisodeID, CharacterID: created.ID, CreatedAt: now}).Error
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.NotFound(c, "character template or drama not found")
		return
	}
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "episode does not belong to drama"})
		return
	}
	response.Created(c, created)
}

func (s *Server) copyScene(c *gin.Context) {
	s.moveOrCopyScene(c, true)
}

func (s *Server) moveScene(c *gin.Context) {
	s.moveOrCopyScene(c, false)
}

func (s *Server) moveOrCopyScene(c *gin.Context, copyOnly bool) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid scene id")
		return
	}
	var body struct {
		EpisodeID       uint `json:"episode_id"`
		AllowCrossDrama bool `json:"allow_cross_drama"`
		MoveStoryboards bool `json:"move_storyboards"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.EpisodeID == 0 {
		response.BadRequest(c, "episode_id is required")
		return
	}
	var output models.Scene
	err = organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var source models.Scene
		if err := tx.Where("id = ? AND deleted_at IS NULL", id).First(&source).Error; err != nil {
			return err
		}
		var target models.Episode
		if err := tx.Where("id = ? AND deleted_at IS NULL", body.EpisodeID).First(&target).Error; err != nil {
			return err
		}
		if source.DramaID != target.DramaID && !body.AllowCrossDrama {
			return errOwnershipMismatch
		}
		now := response.Now()
		if copyOnly {
			output = source
			output.ID, output.DramaID, output.EpisodeID = 0, target.DramaID, &target.ID
			output.CreatedAt, output.UpdatedAt, output.DeletedAt = now, now, nil
			return tx.Create(&output).Error
		}
		var linked int64
		if err := tx.Model(&models.Storyboard{}).Where("scene_id = ? AND deleted_at IS NULL", source.ID).Count(&linked).Error; err != nil {
			return err
		}
		if linked > 0 && !body.MoveStoryboards && source.DramaID != target.DramaID {
			return errors.New("linked storyboards require move_storyboards")
		}
		if body.MoveStoryboards && linked > 0 {
			var maxNumber int
			tx.Model(&models.Storyboard{}).Where("episode_id = ? AND deleted_at IS NULL", target.ID).Select("COALESCE(MAX(storyboard_number), 0)").Scan(&maxNumber)
			var shots []models.Storyboard
			if err := tx.Where("scene_id = ? AND deleted_at IS NULL", source.ID).Order("storyboard_number").Find(&shots).Error; err != nil {
				return err
			}
			for index := range shots {
				if err := tx.Model(&shots[index]).Updates(map[string]any{"episode_id": target.ID, "storyboard_number": maxNumber + index + 1, "updated_at": now}).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Where("scene_id = ?", source.ID).Delete(&models.EpisodeScene{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.EpisodeScene{OrganizationID: currentOrganizationID(c), EpisodeID: target.ID, SceneID: source.ID, CreatedAt: now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&source).Updates(map[string]any{"drama_id": target.DramaID, "episode_id": target.ID, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.First(&output, source.ID).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.NotFound(c, "scene or episode not found")
		return
	}
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	response.Success(c, output)
}
