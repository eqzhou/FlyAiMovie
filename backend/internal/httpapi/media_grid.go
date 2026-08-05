package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/ffmpeg"
	"github.com/eqzhou/flyaimovie/internal/services/generation"
	"github.com/eqzhou/flyaimovie/internal/textutil"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Server) registerGrid(api *gin.RouterGroup) {
	g := api.Group("/grid")
	g.POST("/prompt", s.gridPrompt)
	g.POST("/generate", s.gridGenerate)
	g.GET("/status/:id", s.gridStatus)
	g.POST("/split", s.gridSplit)
	g.POST("/history/:id/assign", s.gridAssignCell)
}

const maxGridRequestBodyBytes = 128 << 10
const maxGridCellPromptRunes = 4_000

func normalizeGridMode(mode string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" {
		return "first_frame", nil
	}
	switch normalized {
	case "first_frame", "first_last", "multi_ref":
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported grid mode %q", normalized)
	}
}

func validateGridStoryboardCount(mode string, rows, cols int, storyboardIDs []uint) error {
	capacity := rows * cols
	if len(storyboardIDs) > capacity {
		return fmt.Errorf("grid accepts at most %d storyboard ids", capacity)
	}
	uniqueCount := len(storyboardIDs)
	if mode == "first_last" {
		if capacity%2 != 0 || len(storyboardIDs) != capacity {
			return errors.New("first_last grid requires an even number of mirrored storyboard ids matching grid cells")
		}
		uniqueCount = capacity / 2
		for index := 0; index < uniqueCount; index++ {
			if storyboardIDs[index] != storyboardIDs[index+uniqueCount] {
				return errors.New("first_last grid requires mirrored storyboard ids for first and last frames")
			}
		}
	}
	seen := make(map[uint]struct{}, uniqueCount)
	for _, storyboardID := range storyboardIDs[:uniqueCount] {
		if _, exists := seen[storyboardID]; exists {
			return errors.New("grid storyboard ids must be unique target slots")
		}
		seen[storyboardID] = struct{}{}
	}
	return nil
}

func validateGridCellPrompts(rows, cols int, cellPrompts []string) error {
	if len(cellPrompts) > rows*cols {
		return fmt.Errorf("cell_prompts accepts at most %d items", rows*cols)
	}
	for _, prompt := range cellPrompts {
		if len([]rune(prompt)) > maxGridCellPromptRunes {
			return fmt.Errorf("each cell_prompts item must be at most %d characters", maxGridCellPromptRunes)
		}
	}
	return nil
}

type gridCellAssignment struct {
	CellIndex    int    `json:"cell_index"`
	StoryboardID uint   `json:"storyboard_id"`
	FrameType    string `json:"frame_type"`
}

var (
	errUnregisteredGridCell  = errors.New("grid cell is not registered to the current organization")
	errGridCellMissing       = errors.New("grid cell does not exist")
	errGridStoryboardMissing = errors.New("storyboard not found")
)

func decodeGridStrings(raw string) []string {
	var values []string
	if json.Unmarshal([]byte(raw), &values) != nil {
		return nil
	}
	return values
}

func decodeGridIDs(raw string) []uint {
	var values []uint
	if json.Unmarshal([]byte(raw), &values) != nil {
		return nil
	}
	return values
}

func deriveGridAssignments(mode string, storyboardIDs []uint, cellCount int, requestedFrameType string) []gridCellAssignment {
	assignments := make([]gridCellAssignment, 0, min(cellCount, len(storyboardIDs)))
	if mode == "first_last" || requestedFrameType == "first_last" {
		half := len(storyboardIDs) / 2
		if half == 0 {
			return assignments
		}
		for index := 0; index < cellCount && index < len(storyboardIDs); index++ {
			frameType := "first_frame"
			if index >= cellCount/2 {
				frameType = "last_frame"
			}
			assignments = append(assignments, gridCellAssignment{CellIndex: index, StoryboardID: storyboardIDs[index%half], FrameType: frameType})
		}
		return assignments
	}
	frameType, err := normalizeStoryboardFrameType(requestedFrameType)
	if err != nil {
		frameType = "first_frame"
	}
	for index := 0; index < cellCount && index < len(storyboardIDs); index++ {
		assignments = append(assignments, gridCellAssignment{CellIndex: index, StoryboardID: storyboardIDs[index], FrameType: frameType})
	}
	return assignments
}

func historyGridAssignments(history models.GridHistory, cellCount int) []gridCellAssignment {
	var assignments []gridCellAssignment
	if json.Unmarshal([]byte(history.AssignmentsJSON), &assignments) == nil {
		valid := make([]gridCellAssignment, 0, len(assignments))
		for _, assignment := range assignments {
			if assignment.CellIndex >= 0 && assignment.CellIndex < cellCount && assignment.StoryboardID > 0 {
				if frameType, err := normalizeStoryboardFrameType(assignment.FrameType); err == nil {
					assignment.FrameType = frameType
					valid = append(valid, assignment)
				}
			}
		}
		if len(valid) > 0 {
			return valid
		}
	}
	return deriveGridAssignments(history.Mode, decodeGridIDs(history.StoryboardIDs), cellCount, history.SplitFrameType)
}

func replaceGridAssignment(assignments []gridCellAssignment, replacement gridCellAssignment) []gridCellAssignment {
	result := make([]gridCellAssignment, 0, len(assignments)+1)
	replaced := false
	for _, assignment := range assignments {
		if assignment.CellIndex == replacement.CellIndex || (assignment.StoryboardID == replacement.StoryboardID && assignment.FrameType == replacement.FrameType) {
			if assignment.CellIndex == replacement.CellIndex && !replaced {
				result = append(result, replacement)
				replaced = true
			}
			continue
		}
		result = append(result, assignment)
	}
	if !replaced {
		result = append(result, replacement)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CellIndex < result[j].CellIndex })
	return result
}

func gridFrameColumn(frameType string) string {
	switch frameType {
	case "last_frame":
		return "last_frame_image"
	case "composed":
		return "composed_image"
	default:
		return "first_frame_image"
	}
}

func gridFrameUpdate(frameType, url string) map[string]any {
	updates := map[string]any{"updated_at": response.Now()}
	switch frameType {
	case "last_frame":
		updates["last_frame_image"] = url
	case "composed":
		updates["composed_image"] = url
	default:
		updates["first_frame_image"] = url
	}
	return updates
}

func (s *Server) gridAssignCell(c *gin.Context) {
	historyID, err := strconv.Atoi(c.Param("id"))
	if err != nil || historyID < 1 {
		response.BadRequest(c, "invalid grid history id")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var body map[string]any
	if err := bindSingleJSON(c, &body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if err := rejectUnknownFields(body, "cell_index", "storyboard_id", "frame_type"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	cellIndex, validIndex := nonNegativeJSONInt(body["cell_index"])
	storyboardID, validStoryboard := positiveJSONInt(body["storyboard_id"])
	frameValue, validFrame := body["frame_type"].(string)
	frameType, frameErr := normalizeStoryboardFrameType(frameValue)
	if !validIndex || !validStoryboard || !validFrame || frameErr != nil {
		response.BadRequest(c, "cell_index, storyboard_id and a valid frame_type are required")
		return
	}
	setAuditResource(c, "grid_cell_assignment", fmt.Sprintf("history:%d;storyboard:%d;cell:%d", historyID, storyboardID, cellIndex))

	var history models.GridHistory
	if err := organizationDB(c).First(&history, historyID).Error; err != nil {
		response.NotFound(c, "grid history not found")
		return
	}
	cells := decodeGridStrings(history.CellsJSON)
	if cellIndex >= len(cells) || strings.TrimSpace(cells[cellIndex]) == "" {
		response.BadRequest(c, "grid cell does not exist")
		return
	}
	storyboardIDValue := uint(storyboardID)
	if err := validateGridOwnership(c, history.DramaID, history.EpisodeID, []uint{storyboardIDValue}); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}

	replacement := gridCellAssignment{CellIndex: cellIndex, StoryboardID: storyboardIDValue, FrameType: frameType}
	organizationID := currentOrganizationID(c)
	committedCellURL := ""
	committedAssignments := []gridCellAssignment(nil)
	err = organizationDB(c).Transaction(func(tx *gorm.DB) error {
		var locked models.GridHistory
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND id = ?", organizationID, historyID).First(&locked).Error; err != nil {
			return err
		}
		lockedCells := decodeGridStrings(locked.CellsJSON)
		if cellIndex >= len(lockedCells) || strings.TrimSpace(lockedCells[cellIndex]) == "" {
			return errGridCellMissing
		}
		committedCellURL = lockedCells[cellIndex]
		var ownedCellCount int64
		if !locked.CellsVerified {
			return errUnregisteredGridCell
		}
		if err := tx.Model(&models.Asset{}).
			Where("organization_id = ? AND url = ? AND category = ? AND grid_history_id = ? AND deleted_at IS NULL", organizationID, committedCellURL, "grid_cell", locked.ID).
			Count(&ownedCellCount).Error; err != nil {
			return err
		}
		if ownedCellCount < 1 {
			return errUnregisteredGridCell
		}
		assignments := historyGridAssignments(locked, len(lockedCells))
		for _, assignment := range assignments {
			if assignment.CellIndex != cellIndex || (assignment.StoryboardID == storyboardIDValue && assignment.FrameType == frameType) {
				continue
			}
			column := gridFrameColumn(assignment.FrameType)
			clear := tx.Model(&models.Storyboard{}).
				Where("organization_id = ? AND id = ? AND deleted_at IS NULL AND "+column+" = ?", organizationID, assignment.StoryboardID, committedCellURL)
			if locked.EpisodeID != nil {
				clear = clear.Where("episode_id = ?", *locked.EpisodeID)
			}
			if err := clear.Update(column, "").Error; err != nil {
				return err
			}
		}
		query := tx.Model(&models.Storyboard{}).Where("organization_id = ? AND id = ? AND deleted_at IS NULL", organizationID, storyboardIDValue)
		if locked.EpisodeID != nil {
			query = query.Where("episode_id = ?", *locked.EpisodeID)
		}
		updated := query.Updates(gridFrameUpdate(frameType, committedCellURL))
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errGridStoryboardMissing
		}
		nextAssignments := replaceGridAssignment(assignments, replacement)
		committedAssignments = nextAssignments
		return tx.Model(&models.GridHistory{}).Where("organization_id = ? AND id = ?", organizationID, locked.ID).Updates(map[string]any{
			"assignments_json": mustJSON(nextAssignments),
			"updated_at":       response.Now(),
		}).Error
	})
	if err != nil {
		switch {
		case errors.Is(err, errUnregisteredGridCell):
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "grid cells from this history must be regenerated before reassignment"})
		case errors.Is(err, errGridStoryboardMissing), errors.Is(err, gorm.ErrRecordNotFound):
			response.NotFound(c, "storyboard not found")
		case errors.Is(err, errGridCellMissing):
			response.BadRequest(c, "grid cell does not exist")
		default:
			response.ServerError(c, "failed to assign grid cell")
		}
		return
	}
	response.Success(c, gin.H{"cell_index": cellIndex, "cell_url": committedCellURL, "storyboard_id": storyboardIDValue, "frame_type": frameType, "assignments": committedAssignments})
}

func (s *Server) gridPrompt(c *gin.Context) {
	var body struct {
		Rows      int    `json:"rows"`
		Cols      int    `json:"cols"`
		Mode      string `json:"mode"`
		EpisodeID *uint  `json:"episode_id"`
		DramaID   *uint  `json:"drama_id"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxGridRequestBodyBytes)
	if err := bindOptionalJSON(c, &body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if body.Rows == 0 {
		body.Rows = 2
	}
	if body.Cols == 0 {
		body.Cols = 2
	}
	if body.Rows < 1 || body.Rows > 5 || body.Cols < 1 || body.Cols > 5 || body.Rows*body.Cols > 25 {
		response.BadRequest(c, "grid must be between 1x1 and 5x5")
		return
	}
	mode, err := normalizeGridMode(body.Mode)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	body.Mode = mode
	if err := validateGridOwnership(c, body.DramaID, body.EpisodeID, nil); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	var shots []models.Storyboard
	var episode models.Episode
	var drama models.Drama
	if body.EpisodeID != nil {
		organizationDB(c).Where("episode_id = ? AND deleted_at IS NULL", *body.EpisodeID).Order("storyboard_number").Find(&shots)
		if err := findActiveEpisode(c, *body.EpisodeID, &episode); err != nil {
			response.NotFound(c, "episode not found")
			return
		}
		_ = findActiveDrama(c, episode.DramaID, &drama)
	} else if body.DramaID != nil {
		if err := findActiveDrama(c, *body.DramaID, &drama); err != nil {
			response.NotFound(c, "drama not found")
			return
		}
	}
	prompt, cells := s.buildGridPrompt(c, currentOrganizationID(c), drama, episode, body.Mode, body.Rows, body.Cols, shots)
	response.Success(c, gin.H{"grid_prompt": prompt, "mode": body.Mode, "rows": body.Rows, "cols": body.Cols, "cell_prompts": cells})
}

func (s *Server) gridGenerate(c *gin.Context) {
	var body struct {
		Prompt        string   `json:"prompt"`
		DramaID       *uint    `json:"drama_id"`
		EpisodeID     *uint    `json:"episode_id"`
		ConfigID      *uint    `json:"config_id"`
		Size          string   `json:"size"`
		Mode          string   `json:"mode"`
		Rows          int      `json:"rows"`
		Cols          int      `json:"cols"`
		CellPrompts   []string `json:"cell_prompts"`
		StoryboardIDs []uint   `json:"storyboard_ids"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxGridRequestBodyBytes)
	if err := bindSingleJSON(c, &body); err != nil || body.Prompt == "" {
		response.BadRequest(c, "prompt required")
		return
	}
	if body.Rows == 0 {
		body.Rows = 2
	}
	if body.Cols == 0 {
		body.Cols = 2
	}
	mode, err := normalizeGridMode(body.Mode)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	body.Mode = mode
	if body.Rows < 1 || body.Rows > 5 || body.Cols < 1 || body.Cols > 5 || body.Rows*body.Cols > 25 {
		response.BadRequest(c, "grid must be between 1x1 and 5x5")
		return
	}
	if err := validateGridStoryboardCount(body.Mode, body.Rows, body.Cols, body.StoryboardIDs); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := validateGridCellPrompts(body.Rows, body.Cols, body.CellPrompts); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := validateGridOwnership(c, body.DramaID, body.EpisodeID, body.StoryboardIDs); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	configID := body.ConfigID
	if body.ConfigID != nil {
		if err := validateAIConfigReferenceFor(c, *body.ConfigID, "image"); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	if configID == nil && body.EpisodeID != nil {
		var ep models.Episode
		if err := findActiveEpisode(c, *body.EpisodeID, &ep); err == nil {
			configID = ep.ImageConfigID
		}
	}
	rec := &models.ImageGeneration{OrganizationID: currentOrganizationID(c), DramaID: body.DramaID, Prompt: body.Prompt, ImageType: "grid", Size: body.Size}
	ts := response.Now()
	hist := models.GridHistory{
		OrganizationID: currentOrganizationID(c),
		DramaID:        body.DramaID, EpisodeID: body.EpisodeID, Mode: body.Mode,
		Rows: body.Rows, Cols: body.Cols, Prompt: body.Prompt,
		CellPrompts: mustJSON(body.CellPrompts), StoryboardIDs: mustJSON(body.StoryboardIDs),
		Status: "processing", CreatedAt: ts, UpdatedAt: ts,
	}
	if err := organizationDB(c).Create(&hist).Error; err != nil {
		response.ServerError(c, "failed to create grid history")
		return
	}
	if err := s.Images.Generate(c.Request.Context(), rec, configID); err != nil {
		hist.Status = "failed"
		hist.ErrorMsg = err.Error()
		hist.UpdatedAt = response.Now()
		organizationDB(c).Save(&hist)
		response.BadRequest(c, err.Error())
		return
	}
	hist.ImageGenID = &rec.ID
	hist.ImageURL = rec.ImageURL
	if rec.Status == "completed" {
		hist.Status = "completed"
		now := response.Now()
		hist.CompletedAt = &now
	}
	if rec.Status == "processing" {
		hist.Status = "processing"
	}
	hist.UpdatedAt = response.Now()
	organizationDB(c).Save(&hist)
	response.Success(c, gin.H{"image": rec, "history": hist})
}

func (s *Server) gridStatus(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var rec models.ImageGeneration
	if err := organizationDB(c).First(&rec, id).Error; err != nil {
		response.NotFound(c, "not found")
		return
	}
	response.Success(c, rec)
}

func (s *Server) gridSplit(c *gin.Context) {
	var body struct {
		ImagePath     string `json:"image_path"`
		ImageURL      string `json:"image_url"`
		Rows          int    `json:"rows"`
		Cols          int    `json:"cols"`
		StoryboardIDs []uint `json:"storyboard_ids"`
		FrameType     string `json:"frame_type"`
		HistoryID     *uint  `json:"history_id"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxGridRequestBodyBytes)
	if err := bindSingleJSON(c, &body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	if body.Rows == 0 {
		body.Rows = 2
	}
	if body.Cols == 0 {
		body.Cols = 2
	}
	if body.FrameType == "" {
		body.FrameType = "first_frame"
	}
	if body.FrameType != "first_last" {
		normalizedFrameType, err := normalizeStoryboardFrameType(body.FrameType)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		body.FrameType = normalizedFrameType
	}
	if body.Rows < 1 || body.Rows > 5 || body.Cols < 1 || body.Cols > 5 || body.Rows*body.Cols > 25 {
		response.BadRequest(c, "grid must be between 1x1 and 5x5")
		return
	}
	if err := validateGridStoryboardCount(body.FrameType, body.Rows, body.Cols, body.StoryboardIDs); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	var history models.GridHistory
	var historyDramaID, historyEpisodeID *uint
	if body.HistoryID != nil {
		if err := organizationDB(c).First(&history, *body.HistoryID).Error; err != nil {
			response.NotFound(c, "grid history not found")
			return
		}
		historyDramaID, historyEpisodeID = history.DramaID, history.EpisodeID
		if history.Status == "split" || strings.TrimSpace(history.CellsJSON) != "" {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "grid history is already split; reassign individual cells instead"})
			return
		}
		if history.Rows != body.Rows || history.Cols != body.Cols {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "grid layout does not match the generated history"})
			return
		}
		if (history.Mode == "first_last") != (body.FrameType == "first_last") {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "grid frame mode does not match the generated history"})
			return
		}
	}
	if err := validateGridOwnership(c, historyDramaID, historyEpisodeID, body.StoryboardIDs); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	src := textutil.FirstNonEmpty(body.ImagePath, body.ImageURL)
	if err := validateLocalMediaOwnership(c, src); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	abs, err := generation.EnsureLocalFile(s.Store, src)
	if err != nil {
		abs, err = generation.EnsureLocalFile(s.Store, strings.TrimPrefix(src, "/static/"))
		if err != nil {
			response.BadRequest(c, "image not found")
			return
		}
	}
	outDir := filepath.Join(s.Store.Root, "grid_cells", uuid.NewString())
	paths, err := ffmpeg.SplitGrid(abs, body.Rows, body.Cols, outDir)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	urls := make([]string, 0, len(paths))
	for _, pth := range paths {
		rel, _ := filepath.Rel(s.Store.Root, pth)
		rel = filepath.ToSlash(rel)
		url := s.Store.PublicURL(rel)
		urls = append(urls, url)
	}
	assignments := deriveGridAssignments(history.Mode, body.StoryboardIDs, len(urls), body.FrameType)
	var historyUpdates map[string]any
	if body.HistoryID != nil {
		historyUpdates = map[string]any{
			"cells_json":       mustJSON(urls),
			"assignments_json": mustJSON(assignments),
			"status":           "split",
			"updated_at":       response.Now(),
			"storyboard_ids":   mustJSON(body.StoryboardIDs),
			"image_url":        src,
			"split_frame_type": body.FrameType,
			"cells_verified":   true,
		}
	}
	if err := s.assignGridCellsWithHistory(c, body.StoryboardIDs, urls, body.FrameType, body.HistoryID, historyUpdates); err != nil {
		_ = os.RemoveAll(outDir)
		if errors.Is(err, errGridHistoryAlreadySplit) {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "grid history is already split; reassign individual cells instead"})
			return
		}
		response.ServerError(c, "failed to assign grid cells")
		return
	}
	response.Success(c, gin.H{"cells": urls, "count": len(urls), "assignments": assignments, "cells_verified": body.HistoryID != nil})
}
