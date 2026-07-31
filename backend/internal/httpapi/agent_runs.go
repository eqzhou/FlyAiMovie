package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/agents"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	errAgentRetrySourceMissing  = errors.New("agent retry source not found")
	errAgentRetryInvalidStatus  = errors.New("agent retry source is not retryable")
	errAgentRetryEpisodeMissing = errors.New("agent retry episode not found")
	errAgentRetryActive         = errors.New("agent retry is already active")
	errAgentRetrySkillCorrupt   = errors.New("agent retry source skill snapshot is corrupt")
	errAgentRunParentMissing    = errors.New("agent run parent not found")
	errAgentRunOrganizationGone = errors.New("agent run organization not found")
)

type agentRunSummary struct {
	ID                 uint    `json:"id"`
	AgentType          string  `json:"agent_type"`
	DramaID            uint    `json:"drama_id"`
	EpisodeID          uint    `json:"episode_id"`
	RetryOfID          *uint   `json:"retry_of_id,omitempty"`
	SkillID            *uint   `json:"skill_id,omitempty"`
	SkillVersionID     *uint   `json:"skill_version_id,omitempty"`
	SkillVersion       int     `json:"skill_version"`
	SkillSource        string  `json:"skill_source"`
	SkillContentSHA256 string  `json:"skill_content_sha256"`
	Status             string  `json:"status"`
	Input              string  `json:"input"`
	OutputJSON         string  `json:"output_json"`
	LastError          string  `json:"last_error"`
	CancelRequestedAt  *string `json:"cancel_requested_at,omitempty"`
	StartedAt          string  `json:"started_at"`
	CompletedAt        *string `json:"completed_at,omitempty"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

func (s *Server) registerAgentRuns(api *gin.RouterGroup) {
	api.GET("/agent-runs", s.listAgentRuns)
	api.GET("/agent-runs/:id", s.getAgentRun)
	api.POST("/agent-runs/:id/cancel", s.cancelAgentRun)
	api.POST("/agent-runs/:id/retry", s.retryAgentRun)
}

func (s *Server) beginAgentRun(parent context.Context, organizationID uint, agentType string, dramaID, episodeID uint, input string) (*models.AgentRun, context.Context, func(), error) {
	return s.beginLinkedAgentRun(parent, organizationID, agentType, dramaID, episodeID, input, nil)
}

func (s *Server) beginLinkedAgentRun(parent context.Context, organizationID uint, agentType string, dramaID, episodeID uint, input string, retryOfID *uint) (*models.AgentRun, context.Context, func(), error) {
	var run *models.AgentRun
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		run, err = createAgentRunRecord(tx, organizationID, agentType, dramaID, episodeID, input, retryOfID)
		return err
	}); err != nil {
		return nil, nil, nil, err
	}
	ctx, cleanup := s.attachAgentRunContext(parent, run)
	return run, ctx, cleanup, nil
}

func createAgentRunRecord(tx *gorm.DB, organizationID uint, agentType string, dramaID, episodeID uint, input string, retryOfID *uint) (*models.AgentRun, error) {
	timestamp := response.Now()
	run := &models.AgentRun{OrganizationID: organizationID, AgentType: agentType, DramaID: dramaID, EpisodeID: episodeID, RetryOfID: retryOfID, Status: "running", Input: input, StartedAt: timestamp, CreatedAt: timestamp, UpdatedAt: timestamp}
	startedPayload, _ := json.Marshal(map[string]any{"status": "running"})
	if err := tx.Create(run).Error; err != nil {
		return nil, err
	}
	if err := tx.Create(&models.AgentRunEvent{OrganizationID: organizationID, AgentRunID: run.ID, Sequence: 1, EventType: "started", PayloadJSON: string(startedPayload), CreatedAt: timestamp}).Error; err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Server) attachAgentRunContext(parent context.Context, run *models.AgentRun) (context.Context, func()) {
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
	return ctx, cleanup
}

func (s *Server) executeAgentRun(run *models.AgentRun, runContext context.Context, cleanup func()) {
	defer cleanup()
	defer func() {
		if recover() != nil {
			if err := s.finishAgentRun(run, nil, errors.New("agent execution interrupted")); err != nil {
				log.Printf("finish interrupted agent run %d: %v", run.ID, err)
			}
		}
	}()
	observer, eventError := s.agentRunObserver(run)
	options := agents.RunOptions{}
	if run.RetryOfID != nil && run.SkillSnapshot != "" {
		override := agents.SkillSnapshotOverride{
			SourceRunID: *run.RetryOfID, SkillSource: run.SkillSource, SkillVersion: run.SkillVersion,
			SkillHash: run.SkillContentSHA256, SkillSnapshot: run.SkillSnapshot,
		}
		if run.SkillID != nil {
			override.SkillID = *run.SkillID
		}
		if run.SkillVersionID != nil {
			override.SkillVersionID = *run.SkillVersionID
		}
		options.SkillSnapshot = &override
	}
	result, runErr := s.Agents.RunObservedWithOptions(runContext, run.OrganizationID, run.AgentType, run.DramaID, run.EpisodeID, run.Input, observer, options)
	if persistErr := eventError(); persistErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("persist agent event: %w", persistErr))
	}
	if err := s.finishAgentRun(run, result, runErr); err != nil {
		timestamp := response.Now()
		fallbackErr := retryAgentRunWrite(func() error {
			return db.DB.Model(&models.AgentRun{}).Where("id = ? AND status = ?", run.ID, "running").Updates(map[string]any{
				"status": "failed", "last_error": "failed to save agent result", "completed_at": timestamp, "updated_at": timestamp,
			}).Error
		})
		if fallbackErr != nil {
			log.Printf("persist terminal state for agent run %d: finish=%v fallback=%v", run.ID, err, fallbackErr)
		}
	}
}

func (s *Server) agentRunObserver(run *models.AgentRun) (agents.EventObserver, func() error) {
	var mu sync.Mutex
	var eventErr error
	observer := func(event agents.RunEvent) {
		mu.Lock()
		defer mu.Unlock()
		if eventErr == nil {
			eventErr = appendAgentRunEvent(run.OrganizationID, run.ID, event.EventType, event.ToolName, event.Payload)
		}
	}
	return observer, func() error {
		mu.Lock()
		defer mu.Unlock()
		return eventErr
	}
}

func appendAgentRunEvent(organizationID, runID uint, eventType, toolName string, payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return retryAgentRunWrite(func() error {
		return db.DB.Transaction(func(tx *gorm.DB) error {
			if organizationID > 0 {
				var organization models.Organization
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", organizationID).First(&organization).Error; errors.Is(err, gorm.ErrRecordNotFound) {
					return errAgentRunOrganizationGone
				} else if err != nil {
					return err
				}
			}
			var parent models.AgentRun
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("organization_id = ? AND id = ?", organizationID, runID).First(&parent).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return errAgentRunParentMissing
			} else if err != nil {
				return err
			}
			if eventType == "prompt_resolved" {
				updates := map[string]any{
					"skill_source": payload["skill_source"], "skill_version": payload["skill_version"],
					"skill_content_sha256": payload["skill_hash"], "skill_snapshot": payload["skill_snapshot"], "updated_at": response.Now(),
				}
				if id, ok := payload["skill_id"].(uint); ok && id > 0 {
					updates["skill_id"] = id
				}
				if id, ok := payload["skill_version_id"].(uint); ok && id > 0 {
					updates["skill_version_id"] = id
				}
				result := tx.Model(&models.AgentRun{}).Where("organization_id = ? AND id = ?", organizationID, runID).Updates(updates)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return errAgentRunParentMissing
				}
			}
			var sequence int
			if err := tx.Model(&models.AgentRunEvent{}).Where("organization_id = ? AND agent_run_id = ?", organizationID, runID).Select("COALESCE(MAX(sequence), 0)").Scan(&sequence).Error; err != nil {
				return err
			}
			return tx.Create(&models.AgentRunEvent{OrganizationID: organizationID, AgentRunID: runID, Sequence: sequence + 1, EventType: eventType, ToolName: toolName, PayloadJSON: string(encoded), CreatedAt: response.Now()}).Error
		})
	})
}

func retryAgentRunWrite(operation func() error) error {
	var operationErr error
	for attempt := 0; attempt < 8; attempt++ {
		operationErr = operation()
		if operationErr == nil || !isSQLiteBusyError(operationErr) {
			return operationErr
		}
		time.Sleep(time.Duration(1<<attempt) * 5 * time.Millisecond)
	}
	return operationErr
}

func isSQLiteBusyError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked")
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
		if result.Type == "failed" {
			status = "failed"
			lastError = strings.TrimSpace(result.Text)
			if lastError == "" {
				lastError = "agent tool execution failed"
			}
		}
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
	return retryAgentRunWrite(func() error {
		return db.DB.Transaction(func(tx *gorm.DB) error {
			updates := map[string]any{"status": status, "output_json": output, "last_error": lastError, "completed_at": timestamp, "updated_at": timestamp}
			if err := tx.Model(&models.AgentRun{}).Where("id = ?", run.ID).Updates(updates).Error; err != nil {
				return err
			}
			var sequence int
			if err := tx.Model(&models.AgentRunEvent{}).Where("organization_id = ? AND agent_run_id = ?", run.OrganizationID, run.ID).Select("COALESCE(MAX(sequence), 0)").Scan(&sequence).Error; err != nil {
				return err
			}
			var streamedToolCalls int64
			if err := tx.Model(&models.AgentRunEvent{}).Where("organization_id = ? AND agent_run_id = ? AND event_type = ?", run.OrganizationID, run.ID, "tool_call").Count(&streamedToolCalls).Error; err != nil {
				return err
			}
			if result != nil && streamedToolCalls == 0 {
				for index, call := range result.ToolCalls {
					toolName, _ := call["toolName"].(string)
					payload := map[string]any{"call": call}
					if index < len(result.ToolResults) {
						payload["result"] = result.ToolResults[index]
					}
					encoded, _ := json.Marshal(payload)
					sequence++
					event := models.AgentRunEvent{OrganizationID: run.OrganizationID, AgentRunID: run.ID, Sequence: sequence, EventType: "tool_call", ToolName: toolName, PayloadJSON: string(encoded), CreatedAt: timestamp}
					if err := tx.Create(&event).Error; err != nil {
						return err
					}
				}
			}
			terminal, _ := json.Marshal(map[string]any{"status": status, "error": lastError})
			sequence++
			return tx.Create(&models.AgentRunEvent{OrganizationID: run.OrganizationID, AgentRunID: run.ID, Sequence: sequence, EventType: status, PayloadJSON: string(terminal), CreatedAt: timestamp}).Error
		})
	})
}

func (s *Server) listAgentRuns(c *gin.Context) {
	query := organizationDB(c).Model(&models.AgentRun{}).Order("id desc").Limit(100)
	if value := strings.TrimSpace(c.Query("status")); value != "" {
		if !isAgentRunStatus(value) {
			response.BadRequest(c, "invalid status")
			return
		}
		if value == "completed" {
			query = query.Where("status IN ?", []string{"completed", "succeeded"})
		} else {
			query = query.Where("status = ?", value)
		}
	}
	if value := strings.TrimSpace(c.Query("agent_type")); value != "" {
		if !isAgentType(value) {
			response.BadRequest(c, "invalid agent_type")
			return
		}
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
	var rows []agentRunSummary
	if err := query.Find(&rows).Error; err != nil {
		response.ServerError(c, "failed to list agent runs")
		return
	}
	response.Success(c, rows)
}

func isAgentRunStatus(value string) bool {
	for _, allowed := range []string{"running", "completed", "succeeded", "failed", "canceled"} {
		if value == allowed {
			return true
		}
	}
	return false
}

func isAgentType(value string) bool {
	for _, allowed := range agents.ValidAgentTypes {
		if value == allowed {
			return true
		}
	}
	return false
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
	safeRun := run
	safeRun.SkillSnapshot = ""
	response.Success(c, gin.H{"run": safeRun, "events": redactAgentRunSkillSnapshots(events)})
}

func redactAgentRunSkillSnapshots(events []models.AgentRunEvent) []models.AgentRunEvent {
	redacted := make([]models.AgentRunEvent, len(events))
	copy(redacted, events)
	for index := range redacted {
		if redacted[index].EventType != "prompt_resolved" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(redacted[index].PayloadJSON), &payload); err != nil {
			redacted[index].PayloadJSON = `{"redacted":true}`
			continue
		}
		delete(payload, "skill_snapshot")
		encoded, err := json.Marshal(payload)
		if err != nil {
			redacted[index].PayloadJSON = `{"redacted":true}`
			continue
		}
		redacted[index].PayloadJSON = string(encoded)
	}
	return redacted
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

func (s *Server) retryAgentRun(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid agent run id")
		return
	}
	s.agentRetryMu.Lock()
	defer s.agentRetryMu.Unlock()
	organizationID := currentOrganizationID(c)
	var source models.AgentRun
	var run *models.AgentRun
	err = retryAgentRunWrite(func() error {
		return db.DB.Transaction(func(tx *gorm.DB) error {
			if lockErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND id = ?", organizationID, id).First(&source).Error; lockErr != nil {
				if errors.Is(lockErr, gorm.ErrRecordNotFound) {
					return errAgentRetrySourceMissing
				}
				return lockErr
			}
			if source.Status != "failed" && source.Status != "canceled" {
				return errAgentRetryInvalidStatus
			}
			var episode models.Episode
			if episodeErr := tx.Where("organization_id = ? AND id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, source.EpisodeID, source.DramaID).First(&episode).Error; episodeErr != nil {
				if errors.Is(episodeErr, gorm.ErrRecordNotFound) {
					return errAgentRetryEpisodeMissing
				}
				return episodeErr
			}
			var activeRetries int64
			if countErr := tx.Model(&models.AgentRun{}).Where("organization_id = ? AND retry_of_id = ? AND status = ?", organizationID, source.ID, "running").Count(&activeRetries).Error; countErr != nil {
				return countErr
			}
			if activeRetries > 0 {
				return errAgentRetryActive
			}
			var createErr error
			run, createErr = createAgentRunRecord(tx, source.OrganizationID, source.AgentType, source.DramaID, source.EpisodeID, source.Input, &source.ID)
			if createErr != nil {
				return createErr
			}
			return copyRetrySkillSnapshot(tx, source, run)
		})
	})
	if err != nil {
		switch {
		case errors.Is(err, errAgentRetrySourceMissing):
			response.NotFound(c, "agent run not found")
		case errors.Is(err, errAgentRetryInvalidStatus):
			response.Conflict(c, "only failed or canceled agent runs can be retried")
		case errors.Is(err, errAgentRetryEpisodeMissing):
			response.NotFound(c, "source episode not found")
		case errors.Is(err, errAgentRetryActive):
			response.Conflict(c, "agent run already has an active retry")
		case errors.Is(err, errAgentRetrySkillCorrupt):
			response.Conflict(c, "source agent run skill snapshot failed integrity validation")
		default:
			response.ServerError(c, "failed to create agent retry")
		}
		return
	}
	parent, timeoutCancel := context.WithTimeout(context.Background(), 15*time.Minute)
	runContext, cleanup := s.attachAgentRunContext(parent, run)
	go s.executeAgentRun(run, runContext, func() {
		cleanup()
		timeoutCancel()
	})
	c.JSON(http.StatusAccepted, gin.H{"code": http.StatusAccepted, "data": agentRunSummaryFromModel(*run), "message": "agent retry started"})
}

func copyRetrySkillSnapshot(tx *gorm.DB, source models.AgentRun, target *models.AgentRun) error {
	if source.SkillSnapshot == "" {
		return nil
	}
	digest := sha256.Sum256([]byte(source.SkillSnapshot))
	computedHash := hex.EncodeToString(digest[:])
	if source.SkillContentSHA256 != "" && !strings.EqualFold(source.SkillContentSHA256, computedHash) {
		return errAgentRetrySkillCorrupt
	}
	target.SkillID = source.SkillID
	target.SkillVersionID = source.SkillVersionID
	target.SkillVersion = source.SkillVersion
	target.SkillSource = source.SkillSource
	target.SkillContentSHA256 = computedHash
	target.SkillSnapshot = source.SkillSnapshot
	return tx.Model(&models.AgentRun{}).Where("organization_id = ? AND id = ?", target.OrganizationID, target.ID).Updates(map[string]any{
		"skill_id": target.SkillID, "skill_version_id": target.SkillVersionID, "skill_version": target.SkillVersion,
		"skill_source": target.SkillSource, "skill_content_sha256": target.SkillContentSHA256,
		"skill_snapshot": target.SkillSnapshot, "updated_at": response.Now(),
	}).Error
}

func agentRunSummaryFromModel(run models.AgentRun) agentRunSummary {
	return agentRunSummary{
		ID: run.ID, AgentType: run.AgentType, DramaID: run.DramaID, EpisodeID: run.EpisodeID, RetryOfID: run.RetryOfID,
		SkillID: run.SkillID, SkillVersionID: run.SkillVersionID, SkillVersion: run.SkillVersion,
		SkillSource: run.SkillSource, SkillContentSHA256: run.SkillContentSHA256,
		Status: run.Status, Input: run.Input, OutputJSON: run.OutputJSON, LastError: run.LastError, CancelRequestedAt: run.CancelRequestedAt,
		StartedAt: run.StartedAt, CompletedAt: run.CompletedAt, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt,
	}
}
