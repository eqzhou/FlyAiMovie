package httpapi

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/security"
	"github.com/eqzhou/flyaimovie/internal/services/adapters"
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
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
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
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
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
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
		if tx.Dialector.Name() == "postgres" {
			return tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(-7046029254386353131)).Error
		}
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
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
	id, _ := strconv.Atoi(c.Param("id"))
	organizationID := currentOrganizationID(c)
	var affected int64
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAIConfigOrganization(tx, organizationID); err != nil {
			return err
		}
		result := tx.Where("organization_id = ? AND id = ?", organizationID, id).Delete(&models.AIServiceConfig{})
		affected = result.RowsAffected
		return result.Error
	})
	if err != nil {
		response.ServerError(c, "failed to delete AI config")
		return
	}
	if affected == 0 {
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
