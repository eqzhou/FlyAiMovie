package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/production"
	"github.com/gin-gonic/gin"
)

func (s *Server) registerProductions(api *gin.RouterGroup) {
	group := api.Group("/productions")
	group.POST("", s.createProduction)
	group.GET("", s.listProductions)
	group.GET("/:id", s.getProduction)
	group.POST("/:id/cancel", s.cancelProduction)
	group.POST("/:id/retry", s.retryProduction)
}

func (s *Server) createProduction(c *gin.Context) {
	var body struct {
		DramaID   uint `json:"drama_id"`
		EpisodeID uint `json:"episode_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.DramaID == 0 || body.EpisodeID == 0 {
		response.BadRequest(c, "drama_id and episode_id are required")
		return
	}
	run, err := s.Productions.Create(currentOrganizationID(c), body.DramaID, body.EpisodeID)
	if errors.Is(err, production.ErrRunNotFound) {
		response.NotFound(c, "episode not found")
		return
	}
	if errors.Is(err, production.ErrActiveRun) {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	if err != nil {
		response.ServerError(c, "failed to create production")
		return
	}
	response.Success(c, run)
}

func (s *Server) listProductions(c *gin.Context) {
	episodeID, err := parseOptionalUint(c.Query("episode_id"))
	if err != nil {
		response.BadRequest(c, "invalid episode_id")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	rows, err := s.Productions.List(currentOrganizationID(c), episodeID, limit)
	if err != nil {
		response.ServerError(c, "failed to list productions")
		return
	}
	response.Success(c, rows)
}

func (s *Server) getProduction(c *gin.Context) {
	id, ok := productionID(c)
	if !ok {
		return
	}
	run, err := s.Productions.Get(currentOrganizationID(c), id)
	if errors.Is(err, production.ErrRunNotFound) {
		response.NotFound(c, "production not found")
		return
	}
	if err != nil {
		response.ServerError(c, "failed to load production")
		return
	}
	response.Success(c, run)
}

func (s *Server) cancelProduction(c *gin.Context) {
	id, ok := productionID(c)
	if !ok {
		return
	}
	if err := s.Productions.Cancel(currentOrganizationID(c), id); err != nil {
		respondProductionTransition(c, err)
		return
	}
	run, _ := s.Productions.Get(currentOrganizationID(c), id)
	response.Success(c, run)
}

func (s *Server) retryProduction(c *gin.Context) {
	id, ok := productionID(c)
	if !ok {
		return
	}
	if err := s.Productions.Retry(currentOrganizationID(c), id); err != nil {
		respondProductionTransition(c, err)
		return
	}
	run, _ := s.Productions.Get(currentOrganizationID(c), id)
	response.Success(c, run)
}

func productionID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.BadRequest(c, "invalid production id")
		return 0, false
	}
	return uint(id), true
}

func parseOptionalUint(value string) (uint, error) {
	if value == "" {
		return 0, nil
	}
	id, err := strconv.ParseUint(value, 10, 32)
	return uint(id), err
}

func respondProductionTransition(c *gin.Context, err error) {
	if errors.Is(err, production.ErrRunNotFound) {
		response.NotFound(c, "production not found")
		return
	}
	if errors.Is(err, production.ErrActiveRun) {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	if errors.Is(err, production.ErrTerminalRun) {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	response.ServerError(c, "failed to update production")
}
