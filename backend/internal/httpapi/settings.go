package httpapi

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/security"
	"github.com/eqzhou/flyaimovie/internal/services/adapters"
	"github.com/eqzhou/flyaimovie/internal/services/agents"
	"github.com/gin-gonic/gin"
)

func (s *Server) registerAIConfigs(api *gin.RouterGroup) {
	api.GET("/ai-configs", s.listAIConfigs)
	api.POST("/ai-configs", s.createAIConfig)
	api.PUT("/ai-configs/:id", s.updateAIConfig)
	api.DELETE("/ai-configs/:id", s.deleteAIConfig)
	api.GET("/ai-providers", s.listAIProviders)
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
	if err := validateAIConfigInput(body.ServiceType, body.Provider, body.Name, body.BaseURL, body.APIKey); err != nil {
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
	if err := validateAIConfigInput(candidateType, candidateProvider, candidateName, candidateBaseURL, candidateAPIKey); err != nil {
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
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata.google.internal" || host == "metadata" {
		return fmt.Errorf("base_url host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || !ip.IsGlobalUnicast()) {
		return fmt.Errorf("base_url must use a public host")
	}
	if strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("api_key is required")
	}
	return nil
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
	res, err := s.Agents.Run(c.Request.Context(), currentOrganizationID(c), agentType, body.DramaID, body.EpisodeID, body.Message)
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
				"language": r.Language, "provider": r.Provider,
			})
		}
		response.Success(c, out)
	})
	api.POST("/ai-voices/sync", func(c *gin.Context) {
		// Seed a few default Chinese voices when remote sync is unavailable.
		ts := response.Now()
		defaults := []models.AIVoice{
			{OrganizationID: currentOrganizationID(c), VoiceID: "male-qn-qingse", VoiceName: "青涩青年", Language: "中文", Provider: "minimax", CreatedAt: ts},
			{OrganizationID: currentOrganizationID(c), VoiceID: "male-qn-jingying", VoiceName: "精英青年", Language: "中文", Provider: "minimax", CreatedAt: ts},
			{OrganizationID: currentOrganizationID(c), VoiceID: "female-shaonv", VoiceName: "少女", Language: "中文", Provider: "minimax", CreatedAt: ts},
			{OrganizationID: currentOrganizationID(c), VoiceID: "female-yujie", VoiceName: "御姐", Language: "中文", Provider: "minimax", CreatedAt: ts},
			{OrganizationID: currentOrganizationID(c), VoiceID: "presenter_male", VoiceName: "男性主持人", Language: "中文", Provider: "minimax", CreatedAt: ts},
			{OrganizationID: currentOrganizationID(c), VoiceID: "presenter_female", VoiceName: "女性主持人", Language: "中文", Provider: "minimax", CreatedAt: ts},
		}
		organizationDB(c).Where("provider = ?", "minimax").Delete(&models.AIVoice{})
		organizationDB(c).Create(&defaults)
		response.Success(c, gin.H{"count": len(defaults), "message": "Seeded default voices (configure MiniMax for live sync)"})
	})
}
