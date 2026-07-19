package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/security"
	"github.com/eqzhou/flyaimovie/internal/services/adapters"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	"github.com/eqzhou/flyaimovie/internal/services/netguard"
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
		return mapConfig(row)
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
	return mapConfig(rows[0])
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
		return mapConfig(row)
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
	return mapConfig(rows[0])
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
	return mapConfig(row)
}

func mapConfig(row models.AIServiceConfig) (*ServiceConfig, error) {
	if err := validateProviderURL(row.Provider, row.BaseURL); err != nil {
		return nil, err
	}
	apiKey, err := security.DecryptSecret(row.APIKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt AI config %d: %w", row.ID, err)
	}
	return &ServiceConfig{
		OrganizationID: row.OrganizationID,
		ID:             row.ID,
		Provider:       row.Provider,
		BaseURL:        row.BaseURL,
		APIKey:         apiKey,
		Model:          row.Model,
		UpdatedAt:      row.UpdatedAt,
	}, nil
}

func validateProviderURL(provider, baseURL string) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "mock" {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("provider base URL is invalid")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("provider base URL must not contain credentials, query, or fragment")
	}
	if provider != "openai_local" && parsed.Scheme != "https" {
		return fmt.Errorf("remote provider base URL must use https")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if provider != "openai_local" {
		allowTrustedPrivate := parsed.Scheme == "https" && adapters.ProviderCustomCAConfigured() && providerPrivateHostAllowed(host)
		if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata" || host == "metadata.google.internal" {
			if !allowTrustedPrivate {
				return fmt.Errorf("remote provider base URL must use a public host")
			}
		}
		if ip := net.ParseIP(host); ip != nil && netguard.IsUnsafeIP(ip) {
			if !allowTrustedPrivate {
				return fmt.Errorf("remote provider base URL must use a public host")
			}
		}
	}
	return nil
}

func providerPrivateHostAllowed(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	for _, candidate := range strings.Split(os.Getenv("AI_PROVIDER_PRIVATE_HOSTS"), ",") {
		if strings.TrimSuffix(strings.ToLower(strings.TrimSpace(candidate)), ".") == host {
			return true
		}
	}
	return false
}

func NewOpenAIClient(cfg *ServiceConfig) *openai.Client {
	c := openai.DefaultConfig(cfg.APIKey)
	if cfg.Provider == "openai_local" {
		c.HTTPClient = &http.Client{
			Transport: &http.Transport{Proxy: nil},
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	} else if cfg.Provider != "" && cfg.Provider != "mock" {
		c.HTTPClient = adapters.SecureProviderHTTPClient(3 * time.Minute)
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
		return "", sanitizeChatProviderError(ctx, err)
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

func sanitizeChatProviderError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	var apiError *openai.APIError
	if errors.As(err, &apiError) && apiError.HTTPStatusCode > 0 {
		return fmt.Errorf("text provider request failed with HTTP %d", apiError.HTTPStatusCode)
	}
	var requestError *openai.RequestError
	if errors.As(err, &requestError) && requestError.HTTPStatusCode > 0 {
		return fmt.Errorf("text provider request failed with HTTP %d", requestError.HTTPStatusCode)
	}
	return fmt.Errorf("text provider request failed")
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
