package models

import (
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// AIServiceBundle is a credential-free, reusable four-service configuration
// template. OrganizationID 0 denotes a built-in template available to every
// organization; non-zero rows are visible only inside that organization.
type AIServiceBundle struct {
	OrganizationID uint   `gorm:"not null;default:0;uniqueIndex:idx_ai_service_bundle_org_key" json:"-"`
	ID             uint   `gorm:"primaryKey" json:"id"`
	Key            string `gorm:"not null;uniqueIndex:idx_ai_service_bundle_org_key" json:"key"`
	Name           string `gorm:"not null" json:"name"`
	Description    string `json:"description"`
	ServicesJSON   string `gorm:"not null" json:"-"`
	IsBuiltin      bool   `gorm:"not null;default:false" json:"is_builtin"`
	IsActive       bool   `gorm:"not null;default:true" json:"is_active"`
	CreatedAt      string `gorm:"not null" json:"created_at"`
	UpdatedAt      string `gorm:"not null" json:"updated_at"`
}

// AIServiceBundleTemplate is the descriptive name used by migration and API
// integrations. Keep the shorter name as a compatibility alias.
type AIServiceBundleTemplate = AIServiceBundle

// AIServiceBundleTemplateItem documents the credential-free normalized shape
// used inside ServicesJSON. It intentionally has no API key field.
type AIServiceBundleTemplateItem struct {
	ServiceType   string `json:"service_type"`
	Provider      string `json:"provider"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	Model         string `json:"model"`
	Endpoint      string `json:"endpoint,omitempty"`
	QueryEndpoint string `json:"query_endpoint,omitempty"`
	Priority      int    `json:"priority,omitempty"`
	IsDefault     bool   `json:"is_default"`
	IsActive      bool   `json:"is_active"`
	Settings      string `json:"settings,omitempty"`
}

func (bundle *AIServiceBundle) BeforeSave(_ *gorm.DB) error {
	var payload any
	if err := json.Unmarshal([]byte(bundle.ServicesJSON), &payload); err != nil {
		return fmt.Errorf("invalid service bundle JSON: %w", err)
	}
	if containsCredentialField(payload) {
		return fmt.Errorf("service bundle templates must not contain credentials")
	}
	return nil
}

func containsCredentialField(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.TrimSpace(key))
			if normalized == "api_key" || normalized == "credentials" || normalized == "secret" || normalized == "token" {
				return true
			}
			if normalized == "settings" {
				if encoded, ok := child.(string); ok && strings.TrimSpace(encoded) != "" {
					var nested any
					if json.Unmarshal([]byte(encoded), &nested) == nil && containsCredentialField(nested) {
						return true
					}
				}
			}
			if containsCredentialField(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsCredentialField(child) {
				return true
			}
		}
	}
	return false
}
