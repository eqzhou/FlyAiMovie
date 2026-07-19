package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/prompttemplate"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var promptKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

const (
	maxPromptPreviewVariables = 16
	maxPromptPreviewBodyBytes = 2 << 20
)

var promptCategories = map[string]bool{
	"agent_system": true, "grid": true, "image": true, "video": true, "audio": true,
}

func (s *Server) registerPromptTemplates(api *gin.RouterGroup) {
	g := api.Group("/prompt-templates")
	g.GET("", s.listPromptTemplates)
	g.POST("", s.createPromptTemplate)
	g.POST("/preview", s.previewPromptTemplateDraft)
	g.PUT("/:id", s.updatePromptTemplate)
	g.DELETE("/:id", s.deletePromptTemplate)
	g.POST("/:id/preview", s.previewPromptTemplate)
	g.POST("/:id/restore-default", s.restorePromptTemplate)
	g.GET("/:id/revisions", s.listPromptTemplateRevisions)
	g.POST("/:id/revisions/:version/restore", s.restorePromptTemplateRevision)
}

func bindPromptPreviewVariables(c *gin.Context, body map[string]any, expectedNames []string) (map[string]string, bool) {
	rawVariables, ok := body["variables"].(map[string]any)
	if !ok {
		response.BadRequest(c, "variables required")
		return nil, false
	}
	if len(rawVariables) > maxPromptPreviewVariables {
		response.BadRequest(c, "too many prompt variables")
		return nil, false
	}
	expected := make(map[string]struct{}, len(expectedNames))
	for _, name := range expectedNames {
		expected[name] = struct{}{}
	}
	variables := make(map[string]string, len(rawVariables))
	for name, rawValue := range rawVariables {
		if _, ok := expected[name]; !ok {
			response.BadRequest(c, fmt.Sprintf("prompt variable %q is not used by template", name))
			return nil, false
		}
		value, ok := rawValue.(string)
		if !ok {
			response.BadRequest(c, "prompt variable values must be strings")
			return nil, false
		}
		if len([]rune(value)) > maxTextRunes {
			response.BadRequest(c, "prompt variable value is too long")
			return nil, false
		}
		variables[name] = value
	}
	return variables, true
}

func (s *Server) previewPromptTemplateDraft(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPromptPreviewBodyBytes)
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	if err := rejectUnknownFields(body, "content", "variables"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	content, ok := body["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		response.BadRequest(c, "prompt content is required")
		return
	}
	variableNames, err := prompttemplate.Variables(content)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	variables, ok := bindPromptPreviewVariables(c, body, variableNames)
	if !ok {
		return
	}
	rendered, err := prompttemplate.Render(content, variables)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"rendered": rendered, "variables": variableNames})
}

func newPromptTemplateRevision(row models.PromptTemplate) models.PromptTemplateRevision {
	return models.PromptTemplateRevision{
		OrganizationID: row.OrganizationID, PromptTemplateID: row.ID, Version: row.Version,
		Key: row.Key, Name: row.Name, Category: row.Category, Description: row.Description,
		Content: row.Content, VariablesJSON: row.VariablesJSON, IsActive: row.IsActive, CreatedAt: row.UpdatedAt,
	}
}

func createPromptTemplateRevision(tx *gorm.DB, row models.PromptTemplate) error {
	revision := newPromptTemplateRevision(row)
	return tx.Create(&revision).Error
}

func (s *Server) listPromptTemplates(c *gin.Context) {
	var rows []models.PromptTemplate
	query := organizationDB(c).Where("organization_id = ? AND deleted_at IS NULL", currentOrganizationID(c)).Order("category, name, id")
	if category := strings.TrimSpace(c.Query("category")); category != "" {
		query = query.Where("category = ?", category)
	}
	if err := query.Find(&rows).Error; err != nil {
		response.ServerError(c, "failed to load prompt templates")
		return
	}
	response.Success(c, rows)
}

type promptTemplateInput struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Content     string `json:"content"`
	IsActive    *bool  `json:"is_active"`
}

func bindPromptTemplateInput(c *gin.Context, creating bool) (promptTemplateInput, []string, bool) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return promptTemplateInput{}, nil, false
	}
	if err := rejectUnknownFields(body, "key", "name", "category", "description", "content", "is_active"); err != nil {
		response.BadRequest(c, err.Error())
		return promptTemplateInput{}, nil, false
	}
	encoded, _ := json.Marshal(body)
	var input promptTemplateInput
	if err := json.Unmarshal(encoded, &input); err != nil {
		response.BadRequest(c, "invalid prompt template fields")
		return input, nil, false
	}
	if creating && (!promptKeyPattern.MatchString(input.Key) || strings.TrimSpace(input.Name) == "" || !promptCategories[input.Category] || strings.TrimSpace(input.Content) == "") {
		response.BadRequest(c, "key, name, category and content are required")
		return input, nil, false
	}
	if input.Key != "" && !promptKeyPattern.MatchString(input.Key) {
		response.BadRequest(c, "invalid prompt template key")
		return input, nil, false
	}
	if input.Category != "" && !promptCategories[input.Category] {
		response.BadRequest(c, "invalid prompt template category")
		return input, nil, false
	}
	if len([]rune(input.Name)) > maxNameRunes || len([]rune(input.Description)) > 2_000 {
		response.BadRequest(c, "prompt template text is too long")
		return input, nil, false
	}
	variables, err := prompttemplate.Variables(input.Content)
	if err != nil {
		response.BadRequest(c, err.Error())
		return input, nil, false
	}
	return input, variables, true
}

func (s *Server) createPromptTemplate(c *gin.Context) {
	input, variables, ok := bindPromptTemplateInput(c, true)
	if !ok {
		return
	}
	active := true
	if input.IsActive != nil {
		active = *input.IsActive
	}
	encodedVariables, _ := json.Marshal(variables)
	now := response.Now()
	organizationID := currentOrganizationID(c)
	var row models.PromptTemplate
	revived := false
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		var existing models.PromptTemplate
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("organization_id = ? AND key = ?", organizationID, input.Key).
			First(&existing).Error
		if err == nil && existing.DeletedAt == nil {
			return gorm.ErrDuplicatedKey
		}
		if err == nil && existing.DeletedAt != nil {
			// Soft-deleted keys still occupy the unique index; revive the row instead of failing create.
			updates := map[string]any{
				"name": strings.TrimSpace(input.Name), "category": input.Category, "description": input.Description,
				"content": input.Content, "variables_json": string(encodedVariables), "version": existing.Version + 1,
				"is_active": active, "deleted_at": nil, "updated_at": now,
			}
			if err := tx.Model(&existing).Updates(updates).Error; err != nil {
				return err
			}
			if err := tx.Where("organization_id = ? AND id = ?", organizationID, existing.ID).First(&row).Error; err != nil {
				return err
			}
			revived = true
			return createPromptTemplateRevision(tx, row)
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row = models.PromptTemplate{
			OrganizationID: organizationID, Key: input.Key, Name: strings.TrimSpace(input.Name), Category: input.Category,
			Description: input.Description, Content: input.Content, VariablesJSON: string(encodedVariables),
			Version: 1, IsActive: active, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return createPromptTemplateRevision(tx, row)
	}); err != nil {
		response.Conflict(c, "prompt template key already exists")
		return
	}
	if revived {
		response.Success(c, row)
		return
	}
	response.Created(c, row)
}

func promptTemplateID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.BadRequest(c, "invalid prompt template id")
		return 0, false
	}
	return uint(id), true
}

func loadPromptTemplate(c *gin.Context, id uint) (models.PromptTemplate, bool) {
	var row models.PromptTemplate
	if err := organizationDB(c).Where("organization_id = ? AND deleted_at IS NULL", currentOrganizationID(c)).First(&row, id).Error; err != nil {
		response.NotFound(c, "prompt template not found")
		return row, false
	}
	return row, true
}

func (s *Server) updatePromptTemplate(c *gin.Context) {
	id, ok := promptTemplateID(c)
	if !ok {
		return
	}
	_, ok = loadPromptTemplate(c, id)
	if !ok {
		return
	}
	input, variables, ok := bindPromptTemplateInput(c, false)
	if !ok {
		return
	}
	organizationID := currentOrganizationID(c)
	var row models.PromptTemplate
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND id = ? AND deleted_at IS NULL", organizationID, id).First(&row).Error; err != nil {
			return err
		}
		updates := map[string]any{"updated_at": response.Now(), "version": row.Version + 1}
		if input.Key != "" {
			updates["key"] = input.Key
		}
		if input.Name != "" {
			updates["name"] = strings.TrimSpace(input.Name)
		}
		if input.Category != "" {
			updates["category"] = input.Category
		}
		if input.Description != "" {
			updates["description"] = input.Description
		}
		if input.Content != "" {
			encoded, _ := json.Marshal(variables)
			updates["content"], updates["variables_json"] = input.Content, string(encoded)
		}
		if input.IsActive != nil {
			updates["is_active"] = *input.IsActive
		}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Where("organization_id = ? AND id = ?", organizationID, id).First(&row).Error; err != nil {
			return err
		}
		return createPromptTemplateRevision(tx, row)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.NotFound(c, "prompt template not found")
		return
	}
	if err != nil {
		response.Conflict(c, "prompt template key already exists")
		return
	}
	response.Success(c, row)
}

func (s *Server) previewPromptTemplate(c *gin.Context) {
	id, ok := promptTemplateID(c)
	if !ok {
		return
	}
	row, ok := loadPromptTemplate(c, id)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPromptPreviewBodyBytes)
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "variables required")
		return
	}
	if err := rejectUnknownFields(body, "variables"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	variableNames, err := prompttemplate.Variables(row.Content)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	variables, ok := bindPromptPreviewVariables(c, body, variableNames)
	if !ok {
		return
	}
	rendered, err := prompttemplate.Render(row.Content, variables)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"rendered": rendered, "version": row.Version})
}

func (s *Server) restorePromptTemplate(c *gin.Context) {
	id, ok := promptTemplateID(c)
	if !ok {
		return
	}
	row, ok := loadPromptTemplate(c, id)
	if !ok {
		return
	}
	item, exists := prompttemplate.DefaultFor(row.Key)
	if !exists {
		response.BadRequest(c, "no built-in default for this template")
		return
	}
	variables, _ := prompttemplate.Variables(item.Content)
	encoded, _ := json.Marshal(variables)
	organizationID := currentOrganizationID(c)
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND id = ? AND deleted_at IS NULL", organizationID, id).First(&row).Error; err != nil {
			return err
		}
		updates := map[string]any{"name": item.Name, "category": item.Category, "description": item.Description, "content": item.Content, "variables_json": string(encoded), "version": row.Version + 1, "is_active": true, "updated_at": response.Now()}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Where("organization_id = ? AND id = ?", organizationID, id).First(&row).Error; err != nil {
			return err
		}
		return createPromptTemplateRevision(tx, row)
	})
	if err != nil {
		response.ServerError(c, "failed to restore prompt template")
		return
	}
	response.Success(c, row)
}

func (s *Server) listPromptTemplateRevisions(c *gin.Context) {
	id, ok := promptTemplateID(c)
	if !ok {
		return
	}
	if _, ok := loadPromptTemplate(c, id); !ok {
		return
	}
	var revisions []models.PromptTemplateRevision
	if err := organizationDB(c).Where("organization_id = ? AND prompt_template_id = ?", currentOrganizationID(c), id).Order("version DESC").Find(&revisions).Error; err != nil {
		response.ServerError(c, "failed to load prompt template revisions")
		return
	}
	response.Success(c, revisions)
}

func promptTemplateVersion(c *gin.Context) (int, bool) {
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil || version < 1 {
		response.BadRequest(c, "invalid prompt template version")
		return 0, false
	}
	return version, true
}

func (s *Server) restorePromptTemplateRevision(c *gin.Context) {
	id, ok := promptTemplateID(c)
	if !ok {
		return
	}
	version, ok := promptTemplateVersion(c)
	if !ok {
		return
	}
	organizationID := currentOrganizationID(c)
	var row models.PromptTemplate
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND id = ? AND deleted_at IS NULL", organizationID, id).First(&row).Error; err != nil {
			return err
		}
		var revision models.PromptTemplateRevision
		if err := tx.Where("organization_id = ? AND prompt_template_id = ? AND version = ?", organizationID, id, version).First(&revision).Error; err != nil {
			return err
		}
		updates := map[string]any{
			"name": revision.Name, "category": revision.Category, "description": revision.Description,
			"content": revision.Content, "variables_json": revision.VariablesJSON, "is_active": revision.IsActive,
			"version": row.Version + 1, "updated_at": response.Now(),
		}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Where("organization_id = ? AND id = ?", organizationID, id).First(&row).Error; err != nil {
			return err
		}
		return createPromptTemplateRevision(tx, row)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.NotFound(c, "prompt template revision not found")
		return
	}
	if err != nil {
		response.ServerError(c, "failed to restore prompt template revision")
		return
	}
	response.Success(c, row)
}

func (s *Server) deletePromptTemplate(c *gin.Context) {
	id, ok := promptTemplateID(c)
	if !ok {
		return
	}
	row, ok := loadPromptTemplate(c, id)
	if !ok {
		return
	}
	now := response.Now()
	organizationDB(c).Model(&row).Updates(map[string]any{"deleted_at": now, "updated_at": now})
	response.Success(c, gin.H{"deleted": true})
}
