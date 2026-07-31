package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/security"
	"github.com/eqzhou/flyaimovie/internal/services/adapters"
	"github.com/eqzhou/flyaimovie/internal/services/servicebundle"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type serviceBundleDraftInput struct {
	BundleID           uint                      `json:"bundle_id"`
	TemplateID         uint                      `json:"template_id"`
	BundleKey          string                    `json:"bundle_key"`
	Services           []servicebundle.DraftItem `json:"services"`
	Credentials        map[string]string         `json:"credentials"`
	ApplyAgentDefaults *bool                     `json:"apply_agent_defaults"`
	PreviewToken       string                    `json:"preview_token"`
}

const maxServiceBundleBodyBytes int64 = 2 << 20

func (s *Server) registerServiceBundles(api *gin.RouterGroup) {
	for _, path := range []string{"/service-bundles", "/ai-service-bundles"} {
		g := api.Group(path)
		g.GET("", s.listServiceBundles)
		g.POST("/preview", s.previewServiceBundle)
		g.POST("/test", s.testServiceBundle)
		g.POST("/apply", s.applyServiceBundle)
	}
}

func (s *Server) testServiceBundle(c *gin.Context) {
	body, err := s.resolveServiceBundleDraft(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	items, err := s.validateServiceBundleDraft(c, body)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	results := make([]gin.H, len(items))
	var group sync.WaitGroup
	for index, item := range items {
		index, item := index, item
		group.Add(1)
		go func() {
			defer group.Done()
			result, probeErr := adapters.ProbeConnection(ctx, adapters.AIConfig{Provider: item.Provider, BaseURL: item.BaseURL, APIKey: item.APIKey, Model: item.Model})
			if probeErr != nil {
				results[index] = gin.H{"service_type": item.ServiceType, "status": "failed", "message": probeErr.Error()}
				return
			}
			results[index] = gin.H{"service_type": item.ServiceType, "status": "ok", "provider": result.Provider, "model": result.Model, "latency_ms": result.LatencyMS, "detail": result.Detail}
		}()
	}
	group.Wait()
	response.Success(c, gin.H{"results": results})
}

func (s *Server) listServiceBundles(c *gin.Context) {
	organizationID := currentOrganizationID(c)
	var rows []models.AIServiceBundle
	if err := db.DB.Where("is_active = ? AND organization_id IN ?", true, []uint{0, organizationID}).Order("is_builtin desc, id").Find(&rows).Error; err != nil {
		response.ServerError(c, "failed to list service bundles")
		return
	}
	out := make([]gin.H, 0, len(rows)+len(servicebundle.Builtins()))
	for _, template := range servicebundle.Builtins() {
		safe := make([]gin.H, 0, len(template.Services))
		for _, item := range template.Services {
			safe = append(safe, serviceBundleItemResponse(item))
		}
		out = append(out, gin.H{"id": 0, "key": template.Key, "name": template.Name, "description": template.Description, "is_builtin": true, "services": safe})
	}
	for _, row := range rows {
		var services []servicebundle.DraftItem
		if err := json.Unmarshal([]byte(row.ServicesJSON), &services); err != nil {
			continue
		}
		safe := make([]gin.H, 0, len(services))
		for _, item := range services {
			safe = append(safe, serviceBundleItemResponse(item))
		}
		out = append(out, gin.H{"id": row.ID, "key": row.Key, "name": row.Name, "description": row.Description, "is_builtin": row.IsBuiltin || row.OrganizationID == 0, "services": safe, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt})
	}
	response.Success(c, out)
}

func (s *Server) previewServiceBundle(c *gin.Context) {
	body, err := s.resolveServiceBundleDraft(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	items, err := s.validateServiceBundleDraft(c, body)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	preview, err := servicebundle.Plan(db.DB, currentOrganizationID(c), items, body.shouldApplyAgentDefaults())
	if err != nil {
		response.ServerError(c, "failed to preview service bundle")
		return
	}
	setAuditResource(c, "service_bundle_preview", "")
	response.Success(c, preview)
}

func (s *Server) applyServiceBundle(c *gin.Context) {
	body, err := s.resolveServiceBundleDraft(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	items, err := s.validateServiceBundleDraft(c, body)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if strings.TrimSpace(body.PreviewToken) == "" {
		response.BadRequest(c, "preview_token is required")
		return
	}
	organizationID := currentOrganizationID(c)
	var result servicebundle.Preview
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAIConfigOrganization(tx, organizationID); err != nil {
			return err
		}
		current, err := servicebundle.Plan(tx, organizationID, items, body.shouldApplyAgentDefaults())
		if err != nil {
			return err
		}
		if current.PreviewToken != body.PreviewToken {
			return errStaleServiceBundlePreview
		}
		for index, item := range items {
			active := true
			if item.IsActive != nil {
				active = *item.IsActive
			}
			if item.IsDefault && !active {
				return fmt.Errorf("default %s service must be active", item.ServiceType)
			}
			if item.IsDefault {
				if err := tx.Model(&models.AIServiceConfig{}).Where("organization_id = ? AND service_type = ?", organizationID, item.ServiceType).Update("is_default", false).Error; err != nil {
					return err
				}
			}
			planned := current.Items[index]
			protectedKey := ""
			if item.APIKey != "" {
				protectedKey, err = security.EncryptSecret(item.APIKey)
				if err != nil {
					return fmt.Errorf("protect %s API key: %w", item.ServiceType, err)
				}
			}
			if planned.Action == "reuse" {
				updates := map[string]any{"priority": item.Priority, "is_default": item.IsDefault, "is_active": active, "endpoint": item.Endpoint, "query_endpoint": item.QueryEndpoint, "settings": item.Settings, "updated_at": response.Now()}
				if protectedKey != "" {
					updates["api_key"] = protectedKey
				}
				if err := tx.Model(&models.AIServiceConfig{}).Where("organization_id = ? AND id = ?", organizationID, planned.ConfigID).Updates(updates).Error; err != nil {
					return err
				}
				continue
			}
			row := models.AIServiceConfig{OrganizationID: organizationID, ServiceType: item.ServiceType, Provider: item.Provider, Name: item.Name, BaseURL: item.BaseURL, APIKey: protectedKey, Model: item.Model, Endpoint: item.Endpoint, QueryEndpoint: item.QueryEndpoint, Priority: item.Priority, IsDefault: item.IsDefault, IsActive: active, Settings: item.Settings, CreatedAt: response.Now(), UpdatedAt: response.Now()}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			current.Items[index].ConfigID = row.ID
		}
		if err := applyServiceBundleAgentDefaults(tx, organizationID, current.Agents, items[0].Model); err != nil {
			return err
		}
		result = current
		return nil
	})
	if errors.Is(err, errStaleServiceBundlePreview) {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "stale preview token; preview the bundle again"})
		return
	}
	if err != nil {
		response.ServerError(c, "failed to apply service bundle")
		return
	}
	setAuditResource(c, "service_bundle", "")
	response.Success(c, gin.H{"items": result.Items, "agents": result.Agents, "conflicts": result.Conflicts})
}

func (input serviceBundleDraftInput) shouldApplyAgentDefaults() bool {
	return input.ApplyAgentDefaults == nil || *input.ApplyAgentDefaults
}

func applyServiceBundleAgentDefaults(tx *gorm.DB, organizationID uint, plans []servicebundle.PlannedAgent, textModel string) error {
	for index := range plans {
		plan := &plans[index]
		if plan.Action == "reuse" {
			continue
		}
		timestamp := response.Now()
		if plan.Action == "update" {
			result := tx.Model(&models.AgentConfig{}).
				Where("organization_id = ? AND id = ?", organizationID, plan.ConfigID).
				Updates(map[string]any{"model": textModel, "is_active": true, "deleted_at": nil, "updated_at": timestamp})
			if result.Error != nil {
				return fmt.Errorf("update agent default %s: %w", plan.AgentType, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("update agent default %s: config changed", plan.AgentType)
			}
			continue
		}
		name, description, ok := servicebundle.AgentDefault(plan.AgentType)
		if !ok {
			return fmt.Errorf("create agent default %s: unsupported agent type", plan.AgentType)
		}
		temperature, maxTokens, maxIterations := 0.7, 4096, 5
		row := models.AgentConfig{
			OrganizationID: organizationID, AgentType: plan.AgentType, Name: name, Description: description,
			Model: textModel, Temperature: &temperature, MaxTokens: &maxTokens, MaxIterations: &maxIterations,
			IsActive: true, CreatedAt: timestamp, UpdatedAt: timestamp,
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("create agent default %s: %w", plan.AgentType, err)
		}
		plan.ConfigID = row.ID
	}
	return nil
}

var errStaleServiceBundlePreview = errors.New("stale service bundle preview")

func (s *Server) resolveServiceBundleDraft(c *gin.Context) (serviceBundleDraftInput, error) {
	var body serviceBundleDraftInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxServiceBundleBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return body, fmt.Errorf("invalid body")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return body, fmt.Errorf("invalid body")
	}
	if len(body.Services) == 0 {
		if template, ok := servicebundle.FindBuiltin(strings.TrimSpace(body.BundleKey)); ok {
			body.Services = template.Services
		}
	}
	if len(body.Services) == 0 {
		id := body.BundleID
		if id == 0 {
			id = body.TemplateID
		}
		if id == 0 {
			return body, fmt.Errorf("services or bundle_id is required")
		}
		var row models.AIServiceBundle
		if err := db.DB.Where("id = ? AND is_active = ? AND organization_id IN ?", id, true, []uint{0, currentOrganizationID(c)}).First(&row).Error; err != nil {
			return body, fmt.Errorf("service bundle not found")
		}
		if err := json.Unmarshal([]byte(row.ServicesJSON), &body.Services); err != nil {
			return body, fmt.Errorf("service bundle is invalid")
		}
	}
	for index := range body.Services {
		if key, ok := body.Credentials[body.Services[index].ServiceType]; ok {
			body.Services[index].APIKey = key
		}
	}
	return body, nil
}

func (s *Server) validateServiceBundleDraft(c *gin.Context, body serviceBundleDraftInput) ([]servicebundle.DraftItem, error) {
	items, err := servicebundle.Normalize(body.Services)
	if err != nil {
		return nil, err
	}
	if body.shouldApplyAgentDefaults() && strings.TrimSpace(items[0].Model) == "" {
		return nil, fmt.Errorf("text model is required when synchronizing agent defaults")
	}
	for index := range items {
		item := &items[index]
		if len([]rune(item.Name)) > maxNameRunes || len([]rune(item.BaseURL)) > maxTextRunes || len([]rune(item.Model)) > maxTextRunes || len([]rune(item.APIKey)) > maxTextRunes || len([]rune(item.Endpoint)) > maxTextRunes || len([]rune(item.QueryEndpoint)) > maxTextRunes || len([]rune(item.Settings)) > maxTextRunes {
			return nil, fmt.Errorf("%s service field is too long", item.ServiceType)
		}
		if strings.TrimSpace(item.Settings) != "" && !json.Valid([]byte(item.Settings)) {
			return nil, fmt.Errorf("%s service settings must be valid JSON", item.ServiceType)
		}
		if item.IsDefault && item.IsActive != nil && !*item.IsActive {
			return nil, fmt.Errorf("default %s service must be active", item.ServiceType)
		}
		validationKey := item.APIKey
		if validationKey == "" {
			var existing models.AIServiceConfig
			err := db.DB.Where("organization_id = ? AND service_type = ? AND provider = ? AND base_url = ? AND model = ? AND name = ?", currentOrganizationID(c), item.ServiceType, item.Provider, item.BaseURL, item.Model, item.Name).First(&existing).Error
			if err == nil {
				validationKey, err = security.DecryptSecret(existing.APIKey)
				if err != nil {
					return nil, fmt.Errorf("stored %s API key cannot be decrypted", item.ServiceType)
				}
			}
		}
		if err := s.validateAIConfigInput(item.ServiceType, item.Provider, item.Name, item.BaseURL, validationKey); err != nil {
			return nil, fmt.Errorf("%s service: %w", item.ServiceType, err)
		}
	}
	return items, nil
}

func serviceBundleItemResponse(item servicebundle.DraftItem) gin.H {
	return gin.H{"service_type": item.ServiceType, "provider": item.Provider, "name": item.Name, "base_url": item.BaseURL, "model": item.Model, "endpoint": item.Endpoint, "query_endpoint": item.QueryEndpoint, "priority": item.Priority, "is_default": item.IsDefault, "is_active": item.IsActive, "settings": item.Settings}
}
