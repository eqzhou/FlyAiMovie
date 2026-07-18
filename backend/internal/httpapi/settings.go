package httpapi

import (
	"context"
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
	"github.com/gin-gonic/gin"
)

func (s *Server) registerAIConfigs(api *gin.RouterGroup) {
	api.GET("/ai-configs", s.listAIConfigs)
	api.POST("/ai-configs", s.createAIConfig)
	api.PUT("/ai-configs/:id", s.updateAIConfig)
	api.POST("/ai-configs/:id/test", s.testAIConfig)
	api.DELETE("/ai-configs/:id", s.deleteAIConfig)
	api.GET("/ai-providers", s.listAIProviders)
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
	if err := organizationDB(c).Create(&row).Error; err != nil {
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
	candidateAPIKey := existing.APIKey
	if value, ok := body["api_key"].(string); ok && value != "" && !strings.Contains(value, "***") {
		candidateAPIKey = value
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
	result := organizationDB(c).Model(&models.AIServiceConfig{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		response.ServerError(c, "failed to update AI config")
		return
	}
	if result.RowsAffected == 0 {
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
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "metadata.google.internal" || host == "metadata" || host == "169.254.169.254" {
		return fmt.Errorf("base_url host is not allowed")
	}
	privateHost := host == "localhost" || strings.HasSuffix(host, ".localhost")
	if ip := net.ParseIP(host); ip != nil {
		privateHost = ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || !ip.IsGlobalUnicast()
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
	if body.MaxIterations != nil && (*body.MaxIterations < 1 || *body.MaxIterations > 2) {
		response.BadRequest(c, "max_iterations must be between 1 and 2")
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
		if !valid || number > 2 {
			response.BadRequest(c, "max_iterations must be between 1 and 2")
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
	res, err := s.Agents.Run(runContext, currentOrganizationID(c), agentType, body.DramaID, body.EpisodeID, body.Message)
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
	api.GET("/ai-voices", func(c *gin.Context) {
		provider := c.Query("provider")
		var rows []models.AIVoice
		q := organizationDB(c).Model(&models.AIVoice{})
		if provider != "" {
			q = q.Where("provider = ?", provider)
		}
		q.Find(&rows)
		out := make([]gin.H, 0, len(rows))
		for _, r := range rows {
			out = append(out, gin.H{
				"voice_id": r.VoiceID, "voice_name": r.VoiceName, "description": r.Description,
				"language": r.Language, "provider": r.Provider, "capabilities": r.Capabilities, "is_active": r.IsActive,
			})
		}
		response.Success(c, out)
	})
	api.POST("/ai-voices/sync", func(c *gin.Context) {
		ts := response.Now()
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
		seen := make([]string, 0, len(voices))
		for _, voice := range voices {
			seen = append(seen, voice.ID)
			row := models.AIVoice{OrganizationID: organizationID, VoiceID: voice.ID, VoiceName: voice.Name, Description: voice.Description,
				Language: voice.Language, Provider: cfg.Provider, Capabilities: voice.Capabilities, IsActive: true, CreatedAt: ts, UpdatedAt: ts}
			var existing models.AIVoice
			if organizationDB(c).Where("voice_id = ?", voice.ID).First(&existing).Error == nil {
				organizationDB(c).Model(&existing).Updates(map[string]any{"voice_name": row.VoiceName, "description": row.Description, "language": row.Language,
					"provider": row.Provider, "capabilities": row.Capabilities, "is_active": true, "updated_at": ts})
			} else {
				organizationDB(c).Create(&row)
			}
		}
		query := organizationDB(c).Model(&models.AIVoice{}).Where("provider = ?", cfg.Provider)
		if len(seen) > 0 {
			query = query.Where("voice_id NOT IN ?", seen)
		}
		query.Updates(map[string]any{"is_active": false, "updated_at": ts})
		response.Success(c, gin.H{"count": len(voices), "message": message})
	})
}
