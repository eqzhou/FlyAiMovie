package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/security"
	"github.com/eqzhou/flyaimovie/internal/services/adapters"
	"github.com/eqzhou/flyaimovie/internal/services/agents"
	"github.com/eqzhou/flyaimovie/internal/services/ai"
	"github.com/eqzhou/flyaimovie/internal/services/netguard"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Server) registerAIConfigs(api *gin.RouterGroup) {
	api.GET("/ai-configs", s.listAIConfigs)
	api.POST("/ai-configs", s.createAIConfig)
	api.PUT("/ai-configs/:id", s.updateAIConfig)
	api.POST("/ai-configs/test", s.testAIConfigDraft)
	api.POST("/ai-configs/:id/test", s.testAIConfig)
	api.DELETE("/ai-configs/:id", s.deleteAIConfig)
	api.GET("/ai-providers", s.listAIProviders)
}

type aiConfigProbeInput struct {
	ID          uint    `json:"id"`
	ServiceType *string `json:"service_type"`
	Provider    *string `json:"provider"`
	Name        *string `json:"name"`
	BaseURL     *string `json:"base_url"`
	APIKey      *string `json:"api_key"`
	Model       *string `json:"model"`
}

func (s *Server) testAIConfigDraft(c *gin.Context) {
	var body aiConfigProbeInput
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	cfg, status, err := s.resolveAIConfigProbe(c, body)
	if err != nil {
		c.JSON(status, gin.H{"code": status, "message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	result, err := adapters.ProbeConnection(ctx, cfg)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": http.StatusBadGateway, "message": err.Error()})
		return
	}
	response.Success(c, result)
}

func (s *Server) resolveAIConfigProbe(c *gin.Context, body aiConfigProbeInput) (adapters.AIConfig, int, error) {
	var stored models.AIServiceConfig
	if body.ID > 0 {
		if err := organizationDB(c).First(&stored, body.ID).Error; err != nil {
			return adapters.AIConfig{}, http.StatusNotFound, fmt.Errorf("AI config not found")
		}
	}
	serviceType := strings.ToLower(strings.TrimSpace(probeStringValue(body.ServiceType, stored.ServiceType)))
	provider := strings.ToLower(strings.TrimSpace(probeStringValue(body.Provider, stored.Provider)))
	name := strings.TrimSpace(probeStringValue(body.Name, stored.Name))
	baseURL := strings.TrimSpace(probeStringValue(body.BaseURL, stored.BaseURL))
	model := strings.TrimSpace(probeStringValue(body.Model, stored.Model))
	requestedAPIKey := probeStringValue(body.APIKey, "")
	if len([]rune(name)) > maxNameRunes || len([]rune(baseURL)) > maxTextRunes || len([]rune(model)) > maxTextRunes || len([]rune(requestedAPIKey)) > maxTextRunes {
		return adapters.AIConfig{}, http.StatusBadRequest, fmt.Errorf("AI config field is too long")
	}
	apiKey := requestedAPIKey
	canReuseStoredKey := stored.ID > 0 && provider == stored.Provider && serviceType == stored.ServiceType && baseURL == strings.TrimSpace(stored.BaseURL)
	if apiKey == "" && stored.ID > 0 && stored.APIKey != "" && !canReuseStoredKey {
		return adapters.AIConfig{}, http.StatusBadRequest, fmt.Errorf("api_key is required when provider, service type, or base_url changes")
	}
	if apiKey == "" && canReuseStoredKey {
		decrypted, err := security.DecryptSecret(stored.APIKey)
		if err != nil {
			return adapters.AIConfig{}, http.StatusInternalServerError, fmt.Errorf("stored API key cannot be decrypted")
		}
		apiKey = decrypted
	}
	if err := s.validateAIConfigInput(serviceType, provider, name, baseURL, apiKey); err != nil {
		return adapters.AIConfig{}, http.StatusBadRequest, err
	}
	return adapters.AIConfig{Provider: provider, BaseURL: baseURL, APIKey: apiKey, Model: model}, http.StatusOK, nil
}

func probeStringValue(value *string, fallback string) string {
	if value != nil {
		return *value
	}
	return fallback
}

func (s *Server) testAIConfig(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid id")
		return
	}
	var row models.AIServiceConfig
	if err := organizationDB(c).First(&row, id).Error; err != nil {
		response.NotFound(c, "AI config not found")
		return
	}
	key, err := security.DecryptSecret(row.APIKey)
	if err != nil {
		response.ServerError(c, "stored API key cannot be decrypted")
		return
	}
	if err := s.validateAIConfigInput(row.ServiceType, row.Provider, row.Name, row.BaseURL, key); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	result, err := adapters.ProbeConnection(ctx, adapters.AIConfig{Provider: row.Provider, BaseURL: row.BaseURL, APIKey: key, Model: row.Model})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": http.StatusBadGateway, "message": err.Error()})
		return
	}
	response.Success(c, result)
}

func (s *Server) listAIConfigs(c *gin.Context) {
	var rows []models.AIServiceConfig
	q := organizationDB(c).Order("priority desc, id desc")
	if t := c.Query("service_type"); t != "" {
		q = q.Where("service_type = ?", t)
	}
	q.Find(&rows)
	// Only expose whether a credential exists; never return credential material.
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		out = append(out, aiConfigResponse(r))
	}
	response.Success(c, out)
}

func (s *Server) createAIConfig(c *gin.Context) {
	var body struct {
		ServiceType   string `json:"service_type"`
		Provider      string `json:"provider"`
		Name          string `json:"name"`
		BaseURL       string `json:"base_url"`
		APIKey        string `json:"api_key"`
		Model         string `json:"model"`
		Endpoint      string `json:"endpoint"`
		QueryEndpoint string `json:"query_endpoint"`
		Priority      int    `json:"priority"`
		IsDefault     bool   `json:"is_default"`
		IsActive      *bool  `json:"is_active"`
		Settings      string `json:"settings"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	body.ServiceType = strings.ToLower(strings.TrimSpace(body.ServiceType))
	body.Provider = strings.ToLower(strings.TrimSpace(body.Provider))
	body.Name = strings.TrimSpace(body.Name)
	body.BaseURL = strings.TrimSpace(body.BaseURL)
	if err := s.validateAIConfigInput(body.ServiceType, body.Provider, body.Name, body.BaseURL, body.APIKey); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	ts := response.Now()
	active := true
	if body.IsActive != nil {
		active = *body.IsActive
	}
	if body.IsDefault && !active {
		response.BadRequest(c, "default AI config must be active")
		return
	}
	storedAPIKey, err := security.EncryptSecret(body.APIKey)
	if err != nil {
		response.ServerError(c, "failed to protect API key")
		return
	}
	row := models.AIServiceConfig{
		OrganizationID: currentOrganizationID(c),
		ServiceType:    body.ServiceType, Provider: body.Provider, Name: body.Name,
		BaseURL: body.BaseURL, APIKey: storedAPIKey, Model: body.Model,
		Endpoint: body.Endpoint, QueryEndpoint: body.QueryEndpoint, Priority: body.Priority,
		IsDefault: body.IsDefault, IsActive: active, Settings: body.Settings,
		CreatedAt: ts, UpdatedAt: ts,
	}
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		organizationID := currentOrganizationID(c)
		if err := lockAIConfigOrganization(tx, organizationID); err != nil {
			return err
		}
		if row.IsDefault {
			if err := tx.Model(&models.AIServiceConfig{}).Where("organization_id = ? AND service_type = ?", organizationID, row.ServiceType).Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(&row).Error
	}); err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.Created(c, aiConfigResponse(row))
}

func (s *Server) updateAIConfig(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid id")
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if err := rejectUnknownFields(body, "service_type", "provider", "name", "base_url", "model", "endpoint", "query_endpoint", "settings", "api_key", "priority", "is_default", "is_active"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	updates := map[string]any{"updated_at": response.Now()}
	for _, key := range []string{"service_type", "provider", "name", "base_url", "model", "endpoint", "query_endpoint", "settings"} {
		value, exists, fieldErr := stringUpdate(body, key, maxTextRunes)
		if fieldErr != nil {
			response.BadRequest(c, fieldErr.Error())
			return
		}
		if exists {
			updates[key] = value
		}
	}
	if value, exists := body["api_key"]; exists {
		if _, valid := value.(string); !valid {
			response.BadRequest(c, "api_key must be a string")
			return
		}
		updates["api_key"] = value
	}
	if value, exists := body["priority"]; exists {
		number, valid := value.(float64)
		if !valid || number < -1_000_000 || number > 1_000_000 || number != float64(int(number)) {
			response.BadRequest(c, "priority must be an integer between -1000000 and 1000000")
			return
		}
		updates["priority"] = int(number)
	}
	for _, key := range []string{"is_default", "is_active"} {
		if value, exists := body[key]; exists {
			flag, valid := value.(bool)
			if !valid {
				response.BadRequest(c, key+" must be a boolean")
				return
			}
			updates[key] = flag
		}
	}
	if len(updates) == 1 {
		response.BadRequest(c, "at least one AI config field is required")
		return
	}
	var existing models.AIServiceConfig
	if err := organizationDB(c).First(&existing, id).Error; err != nil {
		response.NotFound(c, "AI config not found")
		return
	}
	candidateActive := existing.IsActive
	if value, ok := body["is_active"].(bool); ok {
		candidateActive = value
	}
	candidateDefault := existing.IsDefault
	if value, ok := body["is_default"].(bool); ok {
		candidateDefault = value
	}
	if !candidateActive {
		if requestedDefault, explicitlySet := body["is_default"].(bool); explicitlySet && requestedDefault {
			response.BadRequest(c, "default AI config must be active")
			return
		}
		updates["is_default"] = false
		candidateDefault = false
	}
	candidateType := existing.ServiceType
	if value, ok := body["service_type"].(string); ok {
		candidateType = strings.ToLower(strings.TrimSpace(value))
		updates["service_type"] = candidateType
	}
	candidateProvider := existing.Provider
	if value, ok := body["provider"].(string); ok {
		candidateProvider = strings.ToLower(strings.TrimSpace(value))
		updates["provider"] = candidateProvider
	}
	candidateName := existing.Name
	if value, ok := body["name"].(string); ok {
		candidateName = strings.TrimSpace(value)
		updates["name"] = candidateName
	}
	candidateBaseURL := existing.BaseURL
	if value, ok := body["base_url"].(string); ok {
		candidateBaseURL = strings.TrimSpace(value)
		updates["base_url"] = candidateBaseURL
	}
	identityChanged := candidateType != existing.ServiceType || candidateProvider != existing.Provider || candidateBaseURL != strings.TrimSpace(existing.BaseURL)
	replacementAPIKey, hasReplacementAPIKey := body["api_key"].(string)
	hasReplacementAPIKey = hasReplacementAPIKey && replacementAPIKey != "" && !strings.Contains(replacementAPIKey, "***")
	if identityChanged && existing.APIKey != "" && !hasReplacementAPIKey {
		response.BadRequest(c, "api_key is required when provider, service type, or base_url changes")
		return
	}
	candidateAPIKey := existing.APIKey
	if hasReplacementAPIKey {
		candidateAPIKey = replacementAPIKey
	}
	if err := s.validateAIConfigInput(candidateType, candidateProvider, candidateName, candidateBaseURL, candidateAPIKey); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if v, ok := body["api_key"]; ok {
		if str, ok2 := v.(string); ok2 && str != "" && !strings.Contains(str, "***") {
			stored, encryptErr := security.EncryptSecret(str)
			if encryptErr != nil {
				response.ServerError(c, "failed to protect API key")
				return
			}
			updates["api_key"] = stored
		} else {
			delete(updates, "api_key")
		}
	}
	if len(updates) == 1 {
		response.BadRequest(c, "at least one AI config field is required")
		return
	}
	organizationID := currentOrganizationID(c)
	var affected int64
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAIConfigOrganization(tx, organizationID); err != nil {
			return err
		}
		if candidateDefault {
			if err := tx.Model(&models.AIServiceConfig{}).Where("organization_id = ? AND service_type = ? AND id <> ?", organizationID, candidateType, id).Update("is_default", false).Error; err != nil {
				return err
			}
		}
		result := tx.Model(&models.AIServiceConfig{}).Where("organization_id = ? AND id = ?", organizationID, id).Updates(updates)
		affected = result.RowsAffected
		return result.Error
	})
	if err != nil {
		response.ServerError(c, "failed to update AI config")
		return
	}
	if affected == 0 {
		response.NotFound(c, "AI config not found")
		return
	}
	var row models.AIServiceConfig
	if err := organizationDB(c).First(&row, id).Error; err != nil {
		response.NotFound(c, "AI config not found")
		return
	}
	response.Success(c, aiConfigResponse(row))
}

func lockAIConfigOrganization(tx *gorm.DB, organizationID uint) error {
	if organizationID == 0 {
		return nil
	}
	var organization models.Organization
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&organization, organizationID).Error
}

func validateAIConfigInput(serviceType, provider, name, baseURL, apiKey string) error {
	return validateAIConfigInputWithPrivateHosts(serviceType, provider, name, baseURL, apiKey, nil)
}

func (s *Server) validateAIConfigInput(serviceType, provider, name, baseURL, apiKey string) error {
	allowedHosts := []string(nil)
	if s != nil && s.Cfg != nil {
		allowedHosts = s.Cfg.AI.AllowedPrivateBaseURLHosts
	}
	return validateAIConfigInputWithPrivateHosts(serviceType, provider, name, baseURL, apiKey, allowedHosts)
}

func validateAIConfigInputWithPrivateHosts(serviceType, provider, name, baseURL, apiKey string, allowedPrivateHosts []string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name is required")
	}
	if !adapters.IsSupportedProvider(serviceType, provider) {
		return fmt.Errorf("provider %q does not support service type %q", provider, serviceType)
	}
	if provider == "mock" {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("base_url must be an http or https URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("base_url must not contain credentials, query, or fragment")
	}
	if provider != "openai_local" && parsed.Scheme != "https" {
		return fmt.Errorf("remote provider base_url must use https")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "metadata.google.internal" || host == "metadata" || host == "169.254.169.254" {
		return fmt.Errorf("base_url host is not allowed")
	}
	privateHost := host == "localhost" || strings.HasSuffix(host, ".localhost")
	if ip := net.ParseIP(host); ip != nil {
		privateHost = netguard.IsUnsafeIP(ip)
	}
	if provider == "openai_local" {
		if serviceType != "text" || !containsHost(allowedPrivateHosts, host) {
			return fmt.Errorf("local text base_url host must be explicitly allowed")
		}
	} else if privateHost {
		return fmt.Errorf("base_url must use a public host")
	}
	if strings.TrimSpace(apiKey) == "" && provider != "openai_local" {
		return fmt.Errorf("api_key is required")
	}
	return nil
}

func containsHost(allowed []string, host string) bool {
	for _, candidate := range allowed {
		if strings.TrimSuffix(strings.ToLower(strings.TrimSpace(candidate)), ".") == host {
			return true
		}
	}
	return false
}

func aiConfigResponse(row models.AIServiceConfig) gin.H {
	return gin.H{
		"id": row.ID, "service_type": row.ServiceType, "provider": row.Provider, "name": row.Name,
		"base_url": row.BaseURL, "model": row.Model,
		"endpoint": row.Endpoint, "query_endpoint": row.QueryEndpoint, "priority": row.Priority,
		"is_default": row.IsDefault, "is_active": row.IsActive, "settings": row.Settings,
		"created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"api_key_set": row.APIKey != "",
	}
}

func (s *Server) deleteAIConfig(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	result := organizationDB(c).Delete(&models.AIServiceConfig{}, id)
	if result.RowsAffected == 0 {
		response.NotFound(c, "AI config not found")
		return
	}
	response.Success(c, nil)
}

func (s *Server) listAIProviders(c *gin.Context) {
	var rows []models.AIServiceProvider
	q := db.DB.Where("is_active = ?", true)
	if t := c.Query("service_type"); t != "" {
		q = q.Where("service_type = ?", t)
	}
	q.Find(&rows)
	response.Success(c, rows)
}

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
	if err := c.ShouldBindJSON(&body); err != nil || body.AgentType == "" {
		response.BadRequest(c, "agent_type required")
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
	var existing models.AgentConfig
	err := organizationDB(c).Where("agent_type = ?", body.AgentType).First(&existing).Error
	if err == nil {
		updates := map[string]any{
			"name": body.Name, "model": body.Model, "system_prompt": body.SystemPrompt,
			"temperature": body.Temperature, "max_tokens": body.MaxTokens, "max_iterations": body.MaxIterations,
			"deleted_at": nil, "updated_at": ts,
		}
		if body.IsActive != nil {
			updates["is_active"] = *body.IsActive
		}
		if body.Name == "" {
			delete(updates, "name")
		}
		organizationDB(c).Model(&existing).Updates(updates)
		organizationDB(c).First(&existing, existing.ID)
		response.Success(c, existing)
		return
	}
	active := true
	if body.IsActive != nil {
		active = *body.IsActive
	}
	row := models.AgentConfig{
		OrganizationID: currentOrganizationID(c),
		AgentType:      body.AgentType, Name: body.Name, Description: body.Description, Model: body.Model,
		SystemPrompt: body.SystemPrompt, Temperature: body.Temperature, MaxTokens: body.MaxTokens,
		MaxIterations: body.MaxIterations, IsActive: active, CreatedAt: ts, UpdatedAt: ts,
	}
	organizationDB(c).Create(&row)
	response.Success(c, row)
}

func (s *Server) updateAgentConfig(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid agent config id")
		return
	}
	var body map[string]any
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
	result := organizationDB(c).Model(&models.AgentConfig{}).Where("id = ? AND deleted_at IS NULL", id).Updates(updates)
	if result.RowsAffected == 0 {
		response.NotFound(c, "agent config not found")
		return
	}
	var row models.AgentConfig
	organizationDB(c).First(&row, id)
	response.Success(c, row)
}

func (s *Server) deleteAgentConfig(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	now := response.Now()
	result := organizationDB(c).Model(&models.AgentConfig{}).Where("id = ? AND deleted_at IS NULL", id).Update("deleted_at", now)
	if result.RowsAffected == 0 {
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
	if err := c.ShouldBindJSON(&body); err != nil || body.DramaID == 0 || body.EpisodeID == 0 {
		response.BadRequest(c, "drama_id and episode_id are required")
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
	api.GET("/skills", func(c *gin.Context) {
		entries, _ := os.ReadDir(s.Agents.SkillsDir)
		list := make([]gin.H, 0)
		for _, e := range entries {
			if e.IsDir() && s.Agents.IsValid(e.Name()) {
				list = append(list, gin.H{"id": e.Name()})
			}
		}
		response.Success(c, list)
	})
	api.GET("/skills/:id", func(c *gin.Context) {
		id := c.Param("id")
		if !s.Agents.IsValid(id) {
			response.NotFound(c, "skill not found")
			return
		}
		b, err := os.ReadFile(filepath.Join(s.Agents.SkillsDir, id, "SKILL.md"))
		if err != nil {
			response.NotFound(c, "skill not found")
			return
		}
		response.Success(c, gin.H{"id": id, "content": string(b)})
	})
}

func (s *Server) registerVoices(api *gin.RouterGroup) {
	api.GET("/ai-voices", s.listVoices)
	api.POST("/ai-voices/sync", s.syncVoices)
	api.POST("/ai-voices/:voiceID/preview", s.previewVoice)
}

func (s *Server) listVoices(c *gin.Context) {
	provider := strings.TrimSpace(c.Query("provider"))
	if len(provider) > 80 {
		response.BadRequest(c, "invalid provider")
		return
	}
	var rows []models.AIVoice
	query := organizationDB(c).Model(&models.AIVoice{})
	if provider != "" {
		query = query.Where("provider = ?", provider)
	}
	if c.Query("include_inactive") != "1" {
		query = query.Where("is_active = ?", true)
	}
	if err := query.Order("is_active DESC, provider, voice_name, voice_id").Find(&rows).Error; err != nil {
		response.ServerError(c, "failed to load voices")
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		out = append(out, gin.H{"voice_id": row.VoiceID, "voice_name": row.VoiceName, "description": row.Description,
			"language": row.Language, "provider": row.Provider, "capabilities": row.Capabilities, "is_active": row.IsActive})
	}
	response.Success(c, out)
}

func (s *Server) syncVoices(c *gin.Context) {
	organizationID := currentOrganizationID(c)
	var cfg *ai.ServiceConfig
	var err error
	if organizationID == 0 {
		cfg, err = ai.GetActiveConfig("audio", nil)
	} else {
		cfg, err = ai.GetOrganizationConfig(organizationID, "audio", nil)
	}
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	voices := []adapters.VoiceInfo{}
	message := "MiniMax voices synchronized"
	if cfg.Provider == "mock" {
		voices = []adapters.VoiceInfo{{ID: "male-qn-qingse", Name: "青涩青年", Language: "中文", Capabilities: "mock"},
			{ID: "male-qn-jingying", Name: "精英青年", Language: "中文", Capabilities: "mock"}, {ID: "female-shaonv", Name: "少女", Language: "中文", Capabilities: "mock"},
			{ID: "female-yujie", Name: "御姐", Language: "中文", Capabilities: "mock"}, {ID: "presenter_male", Name: "男性主持人", Language: "中文", Capabilities: "mock"},
			{ID: "presenter_female", Name: "女性主持人", Language: "中文", Capabilities: "mock"}}
		message = "Mock voices seeded"
	} else if cfg.Provider == "minimax" {
		voices, err = adapters.ListMiniMaxVoices(c.Request.Context(), adapters.AIConfig{Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model})
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	} else {
		response.BadRequest(c, "active audio provider does not support voice synchronization")
		return
	}
	ts := response.Now()
	seen := make([]string, 0, len(voices))
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		for _, voice := range voices {
			if strings.TrimSpace(voice.ID) == "" {
				continue
			}
			seen = append(seen, voice.ID)
			row := models.AIVoice{OrganizationID: organizationID, VoiceID: voice.ID, VoiceName: voice.Name, Description: voice.Description,
				Language: voice.Language, Provider: cfg.Provider, Capabilities: voice.Capabilities, IsActive: true, CreatedAt: ts, UpdatedAt: ts}
			var existing models.AIVoice
			findErr := tx.Where("organization_id = ? AND voice_id = ?", organizationID, voice.ID).First(&existing).Error
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
				continue
			}
			if findErr != nil {
				return findErr
			}
			if err := tx.Model(&existing).Updates(map[string]any{"voice_name": row.VoiceName, "description": row.Description, "language": row.Language,
				"provider": row.Provider, "capabilities": row.Capabilities, "is_active": true, "updated_at": ts}).Error; err != nil {
				return err
			}
		}
		query := tx.Model(&models.AIVoice{}).Where("organization_id = ? AND provider = ?", organizationID, cfg.Provider)
		if len(seen) > 0 {
			query = query.Where("voice_id NOT IN ?", seen)
		}
		return query.Updates(map[string]any{"is_active": false, "updated_at": ts}).Error
	})
	if err != nil {
		response.ServerError(c, "failed to save synchronized voices")
		return
	}
	response.Success(c, gin.H{"count": len(seen), "message": message})
}

func (s *Server) previewVoice(c *gin.Context) {
	voiceID := strings.TrimSpace(c.Param("voiceID"))
	if voiceID == "" || len([]rune(voiceID)) > 200 {
		response.BadRequest(c, "invalid voice id")
		return
	}
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if err := rejectUnknownFields(raw, "text", "config_id"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	encoded, _ := json.Marshal(raw)
	var body struct {
		Text     string `json:"text"`
		ConfigID uint   `json:"config_id"`
	}
	if err := json.Unmarshal(encoded, &body); err != nil {
		response.BadRequest(c, "invalid voice preview fields")
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" || len([]rune(body.Text)) > 200 {
		response.BadRequest(c, "preview text must contain 1 to 200 characters")
		return
	}
	organizationID := currentOrganizationID(c)
	var voice models.AIVoice
	if err := organizationDB(c).Where("voice_id = ? AND is_active = ?", voiceID, true).First(&voice).Error; err != nil {
		response.NotFound(c, "active voice not found")
		return
	}
	var configRow models.AIServiceConfig
	query := organizationDB(c).Where("service_type = ? AND provider = ? AND is_active = ?", "audio", voice.Provider, true)
	if body.ConfigID > 0 {
		query = query.Where("id = ?", body.ConfigID)
	} else {
		query = query.Order("is_default DESC, priority DESC, id DESC")
	}
	if err := query.First(&configRow).Error; err != nil {
		response.BadRequest(c, "no matching active audio config for voice provider")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	url, err := s.TTS.GenerateVoicePreviewOrganization(ctx, organizationID, body.Text, voice.VoiceID, &configRow.ID)
	if err != nil {
		response.BadRequest(c, "TTS preview failed: "+err.Error())
		return
	}
	response.Success(c, gin.H{"voice_id": voice.VoiceID, "provider": voice.Provider, "audio_url": url})
}
