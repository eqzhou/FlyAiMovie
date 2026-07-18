package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/security"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	openai "github.com/sashabaranov/go-openai"
	"gorm.io/gorm"
)

type ServiceConfig struct {
	OrganizationID uint
	ID             uint
	Provider       string
	BaseURL        string
	APIKey         string
	Model          string
	UpdatedAt      string
}

func GetActiveConfig(serviceType string, preferredID *uint) (*ServiceConfig, error) {
	var row models.AIServiceConfig
	q := db.DB.Where("service_type = ? AND is_active = ?", serviceType, true)
	if preferredID != nil && *preferredID > 0 {
		if err := db.DB.Where("id = ? AND service_type = ? AND is_active = ?", *preferredID, serviceType, true).First(&row).Error; err != nil {
			return nil, fmt.Errorf("active %s AI config %d not found", serviceType, *preferredID)
		}
		return mapConfig(row), nil
	}
	var rows []models.AIServiceConfig
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no active %s AI config; please configure in settings", serviceType)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].IsDefault != rows[j].IsDefault {
			return rows[i].IsDefault
		}
		return rows[i].Priority > rows[j].Priority
	})
	return mapConfig(rows[0]), nil
}

// GetOrganizationConfig is the tenant-scoped variant used by request paths.
// Keeping the organization predicate here prevents a caller from selecting a
// credential row belonging to another tenant by guessing its numeric ID.
func GetOrganizationConfig(organizationID uint, serviceType string, preferredID *uint) (*ServiceConfig, error) {
	if organizationID == 0 {
		return nil, fmt.Errorf("organization is required")
	}
	var row models.AIServiceConfig
	q := db.DB.Where("organization_id = ? AND service_type = ? AND is_active = ?", organizationID, serviceType, true)
	if preferredID != nil && *preferredID > 0 {
		if err := q.Where("id = ?", *preferredID).First(&row).Error; err != nil {
			return nil, fmt.Errorf("active %s AI config %d not found", serviceType, *preferredID)
		}
		return mapConfig(row), nil
	}
	var rows []models.AIServiceConfig
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no active %s AI config; please configure in settings", serviceType)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].IsDefault != rows[j].IsDefault {
			return rows[i].IsDefault
		}
		return rows[i].Priority > rows[j].Priority
	})
	return mapConfig(rows[0]), nil
}

// GetTaskConfig loads the exact configuration used when an async task was submitted.
// It intentionally permits a later-disabled config so an accepted provider task can finish.
func GetTaskConfig(serviceType string, configID *uint) (*ServiceConfig, error) {
	return GetTaskConfigOrganization(0, serviceType, configID)
}

// GetTaskConfigOrganization loads a task's configuration within its tenant.
// A zero organization preserves the legacy single-tenant/demo mode.
func GetTaskConfigOrganization(organizationID uint, serviceType string, configID *uint) (*ServiceConfig, error) {
	if configID == nil || *configID == 0 {
		if organizationID == 0 {
			return GetActiveConfig(serviceType, nil)
		}
		return GetOrganizationConfig(organizationID, serviceType, nil)
	}
	var row models.AIServiceConfig
	query := db.DB.Where("id = ? AND service_type = ?", *configID, serviceType)
	if organizationID != 0 {
		query = query.Where("organization_id = ?", organizationID)
	}
	if err := query.First(&row).Error; err != nil {
		return nil, fmt.Errorf("%s AI config %d not found", serviceType, *configID)
	}
	return mapConfig(row), nil
}

func mapConfig(row models.AIServiceConfig) *ServiceConfig {
	apiKey, err := security.DecryptSecret(row.APIKey)
	if err != nil {
		// Preserve the existing function signature while preventing use of an
		// unreadable credential; callers surface the provider failure later.
		apiKey = ""
	}
	return &ServiceConfig{
		OrganizationID: row.OrganizationID,
		ID:             row.ID,
		Provider:       row.Provider,
		BaseURL:        row.BaseURL,
		APIKey:         apiKey,
		Model:          row.Model,
		UpdatedAt:      row.UpdatedAt,
	}
}

func NewOpenAIClient(cfg *ServiceConfig) *openai.Client {
	c := openai.DefaultConfig(cfg.APIKey)
	if cfg.Provider == "openai_local" {
		c.HTTPClient = &http.Client{Transport: &http.Transport{Proxy: nil}}
	}
	if cfg.BaseURL != "" {
		// support both .../v1 and bare base
		base := cfg.BaseURL
		c.BaseURL = base
		if len(base) > 0 && base[len(base)-1] == '/' {
			base = base[:len(base)-1]
		}
		// go-openai appends /chat/completions relative to BaseURL which should end with /v1
		if !hasSuffix(base, "/v1") {
			c.BaseURL = base + "/v1"
		} else {
			c.BaseURL = base
		}
	}
	return openai.NewClientWithConfig(c)
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

func Chat(ctx context.Context, cfg *ServiceConfig, system, user string, temperature float32) (string, error) {
	return ChatWithMaxTokens(ctx, cfg, system, user, temperature, 0)
}

func ChatWithMaxTokens(ctx context.Context, cfg *ServiceConfig, system, user string, temperature float32, maxTokens int) (string, error) {
	cacheKey := chatCacheKey(cfg, system, user, temperature, maxTokens)
	if db.DB != nil {
		if cached, err := mediacache.New(db.DB, nil).ResolveValue(cfg.OrganizationID, "ai_request", cacheKey); err == nil {
			return cached, nil
		}
	}
	client := NewOpenAIClient(cfg)
	model := cfg.Model
	if model == "" {
		model = "gpt-4o-mini"
	}
	request := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: system},
			{Role: openai.ChatMessageRoleUser, Content: user},
		},
		Temperature: temperature,
	}
	if maxTokens > 0 {
		request.MaxTokens = maxTokens
	}
	resp, err := client.CreateChatCompletion(ctx, request)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty chat response")
	}
	result := resp.Choices[0].Message.Content
	if db.DB != nil && result != "" {
		_, _, _ = mediacache.New(db.DB, nil).PutValue(cfg.OrganizationID, "ai_request", cacheKey, "text", result, time.Hour)
	}
	return result, nil
}

func chatCacheKey(cfg *ServiceConfig, system, user string, temperature float32, maxTokens int) string {
	input := struct {
		ConfigID    uint    `json:"config_id"`
		Provider    string  `json:"provider"`
		BaseURL     string  `json:"base_url"`
		Model       string  `json:"model"`
		UpdatedAt   string  `json:"updated_at"`
		System      string  `json:"system"`
		User        string  `json:"user"`
		Temperature float32 `json:"temperature"`
		MaxTokens   int     `json:"max_tokens"`
	}{cfg.ID, cfg.Provider, cfg.BaseURL, cfg.Model, cfg.UpdatedAt, system, user, temperature, maxTokens}
	payload, _ := json.Marshal(input)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func GetByID(id uint) (*models.AIServiceConfig, error) {
	var row models.AIServiceConfig
	if err := db.DB.First(&row, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("ai config %d not found", id)
		}
		return nil, err
	}
	if row.APIKey != "" {
		if value, err := security.DecryptSecret(row.APIKey); err == nil {
			row.APIKey = value
		}
	}
	return &row, nil
}
