package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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

func (s *Server) registerAgentRuns(api *gin.RouterGroup) {
	api.GET("/agent-runs", s.listAgentRuns)
	api.GET("/agent-runs/:id", s.getAgentRun)
	api.POST("/agent-runs/:id/cancel", s.cancelAgentRun)
}

func (s *Server) beginAgentRun(parent context.Context, organizationID uint, agentType string, dramaID, episodeID uint, input string) (*models.AgentRun, context.Context, func(), error) {
	timestamp := response.Now()
	run := &models.AgentRun{OrganizationID: organizationID, AgentType: agentType, DramaID: dramaID, EpisodeID: episodeID, Status: "running", Input: input, StartedAt: timestamp, CreatedAt: timestamp, UpdatedAt: timestamp}
	if err := db.DB.Create(run).Error; err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	s.agentRunMu.Lock()
	s.agentCancels[run.ID] = cancel
	s.agentRunMu.Unlock()
	cleanup := func() {
		cancel()
		s.agentRunMu.Lock()
		delete(s.agentCancels, run.ID)
		s.agentRunMu.Unlock()
	}
	return run, ctx, cleanup, nil
}

func (s *Server) finishAgentRun(run *models.AgentRun, result *agents.ChatResult, runErr error) error {
	timestamp := response.Now()
	status := "completed"
	lastError := ""
	output := ""
	if result != nil {
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		output = string(encoded)
	}
	var current models.AgentRun
	if err := db.DB.First(&current, run.ID).Error; err != nil {
		return err
	}
	if runErr != nil {
		status = "failed"
		lastError = runErr.Error()
		if current.CancelRequestedAt != nil || errors.Is(runErr, context.Canceled) {
			status = "canceled"
		}
	}
	return db.DB.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"status": status, "output_json": output, "last_error": lastError, "completed_at": timestamp, "updated_at": timestamp}
		if err := tx.Model(&models.AgentRun{}).Where("id = ?", run.ID).Updates(updates).Error; err != nil {
			return err
		}
		sequence := 1
		if result != nil {
			for index, call := range result.ToolCalls {
				toolName, _ := call["toolName"].(string)
				payload := map[string]any{"call": call}
				if index < len(result.ToolResults) {
					payload["result"] = result.ToolResults[index]
				}
				encoded, _ := json.Marshal(payload)
				event := models.AgentRunEvent{OrganizationID: run.OrganizationID, AgentRunID: run.ID, Sequence: sequence, EventType: "tool_call", ToolName: toolName, PayloadJSON: string(encoded), CreatedAt: timestamp}
				if err := tx.Create(&event).Error; err != nil {
					return err
				}
				sequence++
			}
		}
		terminal, _ := json.Marshal(map[string]any{"status": status, "error": lastError})
		return tx.Create(&models.AgentRunEvent{OrganizationID: run.OrganizationID, AgentRunID: run.ID, Sequence: sequence, EventType: status, PayloadJSON: string(terminal), CreatedAt: timestamp}).Error
	})
}

func (s *Server) listAgentRuns(c *gin.Context) {
	query := organizationDB(c).Order("id desc").Limit(100)
	if value := strings.TrimSpace(c.Query("status")); value != "" {
		query = query.Where("status = ?", value)
	}
	if value := strings.TrimSpace(c.Query("agent_type")); value != "" {
		query = query.Where("agent_type = ?", value)
	}
	if value := strings.TrimSpace(c.Query("episode_id")); value != "" {
		id, err := strconv.ParseUint(value, 10, 32)
		if err != nil || id == 0 {
			response.BadRequest(c, "invalid episode_id")
			return
		}
		query = query.Where("episode_id = ?", uint(id))
	}
	var rows []models.AgentRun
	if err := query.Find(&rows).Error; err != nil {
		response.ServerError(c, "failed to list agent runs")
		return
	}
	response.Success(c, rows)
}

func (s *Server) getAgentRun(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid agent run id")
		return
	}
	var run models.AgentRun
	if err := organizationDB(c).First(&run, id).Error; err != nil {
		response.NotFound(c, "agent run not found")
		return
	}
	var events []models.AgentRunEvent
	if err := organizationDB(c).Where("agent_run_id = ?", id).Order("sequence asc").Find(&events).Error; err != nil {
		response.ServerError(c, "failed to load agent run events")
		return
	}
	response.Success(c, gin.H{"run": run, "events": events})
}

func (s *Server) cancelAgentRun(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid agent run id")
		return
	}
	var run models.AgentRun
	if err := organizationDB(c).First(&run, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		response.NotFound(c, "agent run not found")
		return
	} else if err != nil {
		response.ServerError(c, "failed to load agent run")
		return
	}
	if run.Status != "running" {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "agent run is already finished"})
		return
	}
	timestamp := response.Now()
	if err := organizationDB(c).Model(&run).Updates(map[string]any{"cancel_requested_at": timestamp, "updated_at": timestamp}).Error; err != nil {
		response.ServerError(c, "failed to cancel agent run")
		return
	}
	s.agentRunMu.Lock()
	cancel := s.agentCancels[id]
	s.agentRunMu.Unlock()
	if cancel != nil {
		cancel()
	}
	response.Success(c, gin.H{"id": id, "cancel_requested": true})
}
