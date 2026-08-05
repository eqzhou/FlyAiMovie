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
	if err := findActiveScene(c, uint(id), &sc); err != nil {
		response.BadRequest(c, "Scene not found")
		return
	}
	if body.EpisodeID == 0 {
		response.BadRequest(c, "episode_id is required")
		return
	}
	var ep models.Episode
	if err := findActiveEpisode(c, body.EpisodeID, &ep); err != nil {
		response.BadRequest(c, "Episode not found")
		return
	}
	if ep.DramaID != sc.DramaID {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "scene does not belong to episode drama"})
		return
	}
	var drama models.Drama
	_ = findActiveDrama(c, sc.DramaID, &drama)
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
