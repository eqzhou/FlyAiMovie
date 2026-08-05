package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/agents"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (s *Server) registerAgentConfigs(api *gin.RouterGroup) {
	g := api.Group("/agent-configs")
	g.GET("", s.listAgentConfigs)
	g.GET("/:id", s.getAgentConfig)
	g.POST("", s.upsertAgentConfig)
	g.PUT("/:id", s.updateAgentConfig)
	g.DELETE("/:id", s.deleteAgentConfig)
}

func (s *Server) listAgentConfigs(c *gin.Context) {
	var rows []models.AgentConfig
	organizationDB(c).Where("deleted_at IS NULL").Find(&rows)
	response.Success(c, rows)
}

func (s *Server) getAgentConfig(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var row models.AgentConfig
	if err := organizationDB(c).First(&row, id).Error; err != nil {
		response.BadRequest(c, "Not found")
		return
	}
	response.Success(c, row)
}

func (s *Server) upsertAgentConfig(c *gin.Context) {
	var body struct {
		AgentType     string   `json:"agent_type"`
		Name          string   `json:"name"`
		Description   string   `json:"description"`
		Model         string   `json:"model"`
		SystemPrompt  string   `json:"system_prompt"`
		Temperature   *float64 `json:"temperature"`
		MaxTokens     *int     `json:"max_tokens"`
		MaxIterations *int     `json:"max_iterations"`
		IsActive      *bool    `json:"is_active"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.AgentType == "" {
		response.BadRequest(c, "agent_type required")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		response.BadRequest(c, "invalid agent config body")
		return
	}
	body.AgentType = strings.TrimSpace(body.AgentType)
	if len([]rune(body.AgentType)) > maxNameRunes || len([]rune(body.Name)) > maxNameRunes || len([]rune(body.Description)) > maxTextRunes || len([]rune(body.Model)) > maxTextRunes || len([]rune(body.SystemPrompt)) > maxTextRunes {
		response.BadRequest(c, "agent config field is too long")
		return
	}
	if !s.Agents.IsValid(body.AgentType) {
		response.BadRequest(c, "unsupported agent_type")
		return
	}
	if body.Temperature != nil && (*body.Temperature < 0 || *body.Temperature > 2) {
		response.BadRequest(c, "temperature must be between 0 and 2")
		return
	}
	if body.MaxTokens != nil && (*body.MaxTokens < 1 || *body.MaxTokens > 128000) {
		response.BadRequest(c, "max_tokens must be between 1 and 128000")
		return
	}
	if body.MaxIterations != nil && (*body.MaxIterations < 1 || *body.MaxIterations > 5) {
		response.BadRequest(c, "max_iterations must be between 1 and 5")
		return
	}
	ts := response.Now()
	active := true
	if body.IsActive != nil {
		active = *body.IsActive
	}
	organizationID := currentOrganizationID(c)
	var row models.AgentConfig
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAIConfigOrganization(tx, organizationID); err != nil {
			return err
		}
		queryErr := tx.Where("organization_id = ? AND agent_type = ?", organizationID, body.AgentType).Order("id DESC").First(&row).Error
		if queryErr == nil {
			updates := map[string]any{
				"description": body.Description, "model": body.Model, "system_prompt": body.SystemPrompt,
				"temperature": body.Temperature, "max_tokens": body.MaxTokens, "max_iterations": body.MaxIterations,
				"deleted_at": nil, "updated_at": ts,
			}
			if body.IsActive != nil {
				updates["is_active"] = *body.IsActive
			}
			if body.Name != "" {
				updates["name"] = body.Name
			}
			if err := tx.Model(&row).Updates(updates).Error; err != nil {
				return err
			}
			return tx.First(&row, row.ID).Error
		}
		if !errors.Is(queryErr, gorm.ErrRecordNotFound) {
			return queryErr
		}
		row = models.AgentConfig{
			OrganizationID: organizationID, AgentType: body.AgentType, Name: body.Name, Description: body.Description, Model: body.Model,
			SystemPrompt: body.SystemPrompt, Temperature: body.Temperature, MaxTokens: body.MaxTokens,
			MaxIterations: body.MaxIterations, IsActive: active, CreatedAt: ts, UpdatedAt: ts,
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		response.ServerError(c, "failed to save agent config")
		return
	}
	response.Success(c, row)
}

func (s *Server) updateAgentConfig(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid agent config id")
		return
	}
	var body map[string]any
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if err := rejectUnknownFields(body, "model", "name", "description", "system_prompt", "temperature", "max_tokens", "max_iterations", "is_active"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	updates := map[string]any{"updated_at": response.Now()}
	for _, k := range []string{"model", "name", "description", "system_prompt"} {
		v, ok, fieldErr := stringUpdate(body, k, maxTextRunes)
		if fieldErr != nil {
			response.BadRequest(c, fieldErr.Error())
			return
		}
		if ok {
			updates[k] = v
		}
	}
	if v, ok := body["temperature"]; ok {
		number, valid := v.(float64)
		if !valid || number < 0 || number > 2 {
			response.BadRequest(c, "temperature must be between 0 and 2")
			return
		}
		updates["temperature"] = number
	}
	if v, ok := body["max_tokens"]; ok {
		number, valid := positiveJSONInt(v)
		if !valid || number > 128000 {
			response.BadRequest(c, "max_tokens must be between 1 and 128000")
			return
		}
		updates["max_tokens"] = number
	}
	if v, ok := body["max_iterations"]; ok {
		number, valid := positiveJSONInt(v)
		if !valid || number > 5 {
			response.BadRequest(c, "max_iterations must be between 1 and 5")
			return
		}
		updates["max_iterations"] = number
	}
	if v, ok := body["is_active"]; ok {
		active, valid := v.(bool)
		if !valid {
			response.BadRequest(c, "is_active must be a boolean")
			return
		}
		updates["is_active"] = active
	}
	if len(updates) == 1 {
		response.BadRequest(c, "at least one agent config field is required")
		return
	}
	organizationID := currentOrganizationID(c)
	var affected int64
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAIConfigOrganization(tx, organizationID); err != nil {
			return err
		}
		result := tx.Model(&models.AgentConfig{}).Where("organization_id = ? AND id = ? AND deleted_at IS NULL", organizationID, id).Updates(updates)
		affected = result.RowsAffected
		return result.Error
	})
	if err != nil {
		response.ServerError(c, "failed to update agent config")
		return
	}
	if affected == 0 {
		response.NotFound(c, "agent config not found")
		return
	}
	var row models.AgentConfig
	if err := organizationDB(c).First(&row, id).Error; err != nil {
		response.ServerError(c, "failed to reload agent config")
		return
	}
	response.Success(c, row)
}

func (s *Server) deleteAgentConfig(c *gin.Context) {
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
	id, _ := strconv.Atoi(c.Param("id"))
	now := response.Now()
	organizationID := currentOrganizationID(c)
	var affected int64
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAIConfigOrganization(tx, organizationID); err != nil {
			return err
		}
		result := tx.Model(&models.AgentConfig{}).Where("organization_id = ? AND id = ? AND deleted_at IS NULL", organizationID, id).Update("deleted_at", now)
		affected = result.RowsAffected
		return result.Error
	})
	if err != nil {
		response.ServerError(c, "failed to delete agent config")
		return
	}
	if affected == 0 {
		response.NotFound(c, "agent config not found")
		return
	}
	response.Success(c, nil)
}

func (s *Server) registerAgent(api *gin.RouterGroup) {
	api.POST("/agent/:type/chat", s.agentChat)
	api.GET("/agent/:type/debug", s.agentDebug)
	s.registerAgentRuns(api)
}

func (s *Server) agentChat(c *gin.Context) {
	agentType := c.Param("type")
	if !s.Agents.IsValid(agentType) {
		response.BadRequest(c, "Invalid agent type: "+agentType)
		return
	}
	var body struct {
		Message   string `json:"message"`
		DramaID   uint   `json:"drama_id"`
		EpisodeID uint   `json:"episode_id"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.DramaID == 0 || body.EpisodeID == 0 {
		response.BadRequest(c, "drama_id and episode_id are required")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		response.BadRequest(c, "invalid agent request body")
		return
	}
	if len([]rune(body.Message)) > maxTextRunes {
		response.BadRequest(c, "message is too long")
		return
	}
	var episode models.Episode
	if err := organizationDB(c).Where("id = ? AND drama_id = ? AND deleted_at IS NULL", body.EpisodeID, body.DramaID).First(&episode).Error; err != nil {
		response.NotFound(c, "episode not found")
		return
	}
	run, runContext, cleanup, err := s.beginAgentRun(c.Request.Context(), currentOrganizationID(c), agentType, body.DramaID, body.EpisodeID, body.Message)
	if err != nil {
		response.ServerError(c, "failed to create agent run")
		return
	}
	defer cleanup()
	observer, eventError := s.agentRunObserver(run)
	res, err := s.Agents.RunObserved(runContext, currentOrganizationID(c), agentType, body.DramaID, body.EpisodeID, body.Message, observer)
	if persistErr := eventError(); persistErr != nil {
		err = errors.Join(err, fmt.Errorf("persist agent event: %w", persistErr))
	}
	if finishErr := s.finishAgentRun(run, res, err); finishErr != nil {
		response.ServerError(c, "failed to save agent run")
		return
	}
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, res)
}

func (s *Server) agentDebug(c *gin.Context) {
	agentType := c.Param("type")
	if !s.Agents.IsValid(agentType) {
		response.BadRequest(c, "Invalid agent type")
		return
	}
	response.Success(c, gin.H{"agent_type": agentType, "valid": true, "all": agents.ValidAgentTypes})
}

func (s *Server) registerSkills(api *gin.RouterGroup) {
	s.registerSkillRegistryRoutes(api)
}
