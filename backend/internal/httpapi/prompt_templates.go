package httpapi

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/prompttemplate"
	"github.com/gin-gonic/gin"
)

var promptKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

var promptCategories = map[string]bool{
	"agent_system": true, "grid": true, "image": true, "video": true, "audio": true,
}

func (s *Server) registerPromptTemplates(api *gin.RouterGroup) {
	g := api.Group("/prompt-templates")
	g.GET("", s.listPromptTemplates)
	g.POST("", s.createPromptTemplate)
	g.PUT("/:id", s.updatePromptTemplate)
	g.DELETE("/:id", s.deletePromptTemplate)
	g.POST("/:id/preview", s.previewPromptTemplate)
	g.POST("/:id/restore-default", s.restorePromptTemplate)
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
	row := models.PromptTemplate{OrganizationID: currentOrganizationID(c), Key: input.Key, Name: strings.TrimSpace(input.Name), Category: input.Category, Description: input.Description, Content: input.Content, VariablesJSON: string(encodedVariables), Version: 1, IsActive: active, CreatedAt: now, UpdatedAt: now}
	if err := organizationDB(c).Create(&row).Error; err != nil {
		response.Conflict(c, "prompt template key already exists")
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
	row, ok := loadPromptTemplate(c, id)
	if !ok {
		return
	}
	input, variables, ok := bindPromptTemplateInput(c, false)
	if !ok {
		return
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
		updates["content"] = input.Content
		updates["variables_json"] = string(encoded)
	}
	if input.IsActive != nil {
		updates["is_active"] = *input.IsActive
	}
	if err := organizationDB(c).Model(&row).Updates(updates).Error; err != nil {
		response.Conflict(c, "prompt template key already exists")
		return
	}
	organizationDB(c).First(&row, id)
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
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "variables required")
		return
	}
	if err := rejectUnknownFields(body, "variables"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	rawVariables, ok := body["variables"].(map[string]any)
	if !ok {
		response.BadRequest(c, "variables required")
		return
	}
	variables := make(map[string]string, len(rawVariables))
	for name, rawValue := range rawVariables {
		value, ok := rawValue.(string)
		if !ok {
			response.BadRequest(c, "prompt variable values must be strings")
			return
		}
		variables[name] = value
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
	organizationDB(c).Model(&row).Updates(map[string]any{"name": item.Name, "category": item.Category, "description": item.Description, "content": item.Content, "variables_json": string(encoded), "version": row.Version + 1, "is_active": true, "updated_at": response.Now()})
	organizationDB(c).First(&row, id)
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
