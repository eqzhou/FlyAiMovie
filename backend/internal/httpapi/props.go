package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
)

func (s *Server) registerProps(api *gin.RouterGroup) {
	g := api.Group("/props")
	g.GET("", s.listProps)
	g.POST("", s.createProp)
	g.PUT("/:id", s.updateProp)
	g.DELETE("/:id", s.deleteProp)
	g.POST("/:id/generate-image", s.propGenerateImage)
}

func (s *Server) listProps(c *gin.Context) {
	q := organizationDB(c).Model(&models.Prop{}).Where("deleted_at IS NULL").Order("id desc")
	if v := c.Query("drama_id"); v != "" {
		q = q.Where("drama_id = ?", v)
	}
	var rows []models.Prop
	q.Limit(200).Find(&rows)
	response.Success(c, rows)
}

func (s *Server) createProp(c *gin.Context) {
	var body struct {
		DramaID     uint   `json:"drama_id"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.DramaID == 0 || strings.TrimSpace(body.Name) == "" {
		response.BadRequest(c, "drama_id and name required")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if len([]rune(body.Name)) > maxNameRunes || len([]rune(body.Type)) > maxNameRunes || len([]rune(body.Description)) > maxTextRunes || len([]rune(body.Prompt)) > maxTextRunes {
		response.BadRequest(c, "prop field is too long")
		return
	}
	var drama models.Drama
	if err := organizationDB(c).First(&drama, body.DramaID).Error; err != nil {
		response.BadRequest(c, "drama not found")
		return
	}
	ts := response.Now()
	prompt := body.Prompt
	if prompt == "" {
		prompt = body.Name + ", prop product shot, white background"
	}
	row := models.Prop{
		OrganizationID: currentOrganizationID(c), DramaID: body.DramaID, Name: strings.TrimSpace(body.Name), Type: body.Type,
		Description: body.Description, Prompt: prompt, CreatedAt: ts, UpdatedAt: ts,
	}
	if err := organizationDB(c).Create(&row).Error; err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.Created(c, row)
}

func (s *Server) updateProp(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid prop id")
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if err := rejectUnknownFields(body, "name", "type", "description", "prompt", "image_url", "local_path"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	updates := map[string]any{"updated_at": response.Now()}
	for _, k := range []string{"name", "type", "description", "prompt", "image_url", "local_path"} {
		maxRunes := maxTextRunes
		if k == "name" || k == "type" {
			maxRunes = maxNameRunes
		}
		v, ok, err := stringUpdate(body, k, maxRunes)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		if ok {
			if k == "name" {
				v = strings.TrimSpace(v)
				if v == "" {
					response.BadRequest(c, "name must not be empty")
					return
				}
			}
			if (k == "image_url" || k == "local_path") && v != "" {
				if err := validateLocalMediaOwnership(c, v); err != nil {
					response.BadRequest(c, err.Error())
					return
				}
			}
			updates[k] = v
		}
	}
	if len(updates) == 1 {
		response.BadRequest(c, "at least one prop field is required")
		return
	}
	result := organizationDB(c).Model(&models.Prop{}).Where("id = ? AND deleted_at IS NULL", id).Updates(updates)
	if result.RowsAffected == 0 {
		response.NotFound(c, "prop not found")
		return
	}
	response.Success(c, nil)
}

func (s *Server) deleteProp(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	now := response.Now()
	result := organizationDB(c).Model(&models.Prop{}).Where("id = ? AND deleted_at IS NULL", id).Update("deleted_at", now)
	if result.RowsAffected == 0 {
		response.NotFound(c, "prop not found")
		return
	}
	response.Success(c, nil)
}

func (s *Server) propGenerateImage(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var body struct {
		EpisodeID uint `json:"episode_id"`
	}
	if err := bindOptionalJSON(c, &body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	var prop models.Prop
	if err := organizationDB(c).First(&prop, id).Error; err != nil {
		response.NotFound(c, "prop not found")
		return
	}
	var configID *uint
	if body.EpisodeID > 0 {
		var ep models.Episode
		if err := organizationDB(c).First(&ep, body.EpisodeID).Error; err != nil {
			response.BadRequest(c, "episode not found")
			return
		}
		if ep.DramaID != prop.DramaID {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "prop does not belong to episode drama"})
			return
		}
		configID = ep.ImageConfigID
	}
	prompt := firstNonEmpty(prop.Prompt, prop.Name+", prop, product photography")
	pid := prop.ID
	did := prop.DramaID
	rec := &models.ImageGeneration{
		OrganizationID: currentOrganizationID(c), PropID: &pid, DramaID: &did, Prompt: prompt, ImageType: "prop",
	}
	if err := s.Images.Generate(c.Request.Context(), rec, configID); err != nil {
		respondGenerationError(c, err)
		return
	}
	if rec.ImageURL != "" {
		organizationDB(c).Model(&prop).Updates(map[string]any{
			"image_url": rec.ImageURL, "local_path": rec.LocalPath, "updated_at": response.Now(),
		})
	}
	response.Success(c, gin.H{"image_generation_id": rec.ID, "image_url": rec.ImageURL})
}
