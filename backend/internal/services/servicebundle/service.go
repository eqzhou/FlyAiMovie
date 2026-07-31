package servicebundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"gorm.io/gorm"
)

var serviceTypes = []string{"text", "image", "video", "audio"}

var agentDefaults = []struct {
	AgentType   string
	Name        string
	Description string
}{
	{AgentType: "script_rewriter", Name: "剧本改写", Description: "小说或大纲改写为格式化剧本"},
	{AgentType: "extractor", Name: "角色场景提取", Description: "从剧本提取并去重角色与场景"},
	{AgentType: "storyboard_breaker", Name: "分镜拆解", Description: "将剧本拆解为有序分镜"},
	{AgentType: "voice_assigner", Name: "音色分配", Description: "为角色分配合适音色"},
	{AgentType: "grid_prompt_generator", Name: "宫格提示词", Description: "生成角色、场景与宫格提示词"},
}

type DraftItem struct {
	ServiceType   string `json:"service_type"`
	Provider      string `json:"provider"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	APIKey        string `json:"api_key,omitempty"`
	Model         string `json:"model"`
	Endpoint      string `json:"endpoint,omitempty"`
	QueryEndpoint string `json:"query_endpoint,omitempty"`
	Priority      int    `json:"priority,omitempty"`
	IsDefault     bool   `json:"is_default"`
	IsActive      *bool  `json:"is_active,omitempty"`
	Settings      string `json:"settings,omitempty"`
}

type PlannedItem struct {
	ServiceType string `json:"service_type"`
	Action      string `json:"action"`
	ConfigID    uint   `json:"config_id,omitempty"`
	Provider    string `json:"provider"`
	Name        string `json:"name"`
	IsDefault   bool   `json:"is_default"`
}

type Conflict struct {
	ServiceType string `json:"service_type"`
	Kind        string `json:"kind"`
	ConfigID    uint   `json:"config_id"`
	Message     string `json:"message"`
}

type PlannedAgent struct {
	AgentType string `json:"agent_type"`
	Action    string `json:"action"`
	ConfigID  uint   `json:"config_id,omitempty"`
	Model     string `json:"model"`
}

type Preview struct {
	Items        []PlannedItem  `json:"items"`
	Agents       []PlannedAgent `json:"agents"`
	Conflicts    []Conflict     `json:"conflicts"`
	PreviewToken string         `json:"preview_token"`
}

type BuiltinTemplate struct {
	Key         string
	Name        string
	Description string
	Services    []DraftItem
}

// Builtins returns templates compiled into the application. They contain
// endpoint metadata only; callers must supply credentials in a draft request.
func Builtins() []BuiltinTemplate {
	active := true
	return []BuiltinTemplate{{
		Key: "standard-cloud-studio", Name: "Standard cloud studio", Description: "A balanced four-service starting point",
		Services: []DraftItem{
			{ServiceType: "text", Provider: "openai", Name: "bundle-text", BaseURL: "https://api.openai.com", Model: "gpt-4o-mini", IsDefault: true, IsActive: &active},
			{ServiceType: "image", Provider: "openai", Name: "bundle-image", BaseURL: "https://api.openai.com", Model: "gpt-image-1", IsDefault: true, IsActive: &active},
			{ServiceType: "video", Provider: "openai", Name: "bundle-video", BaseURL: "https://api.openai.com", Model: "sora-2", IsDefault: true, IsActive: &active},
			{ServiceType: "audio", Provider: "minimax", Name: "bundle-audio", BaseURL: "https://api.minimax.chat", Model: "speech-02-turbo", IsDefault: true, IsActive: &active},
		},
	}}
}

func FindBuiltin(key string) (BuiltinTemplate, bool) {
	for _, template := range Builtins() {
		if template.Key == key {
			return template, true
		}
	}
	return BuiltinTemplate{}, false
}

func Normalize(items []DraftItem) ([]DraftItem, error) {
	if len(items) != len(serviceTypes) {
		return nil, fmt.Errorf("bundle must contain exactly text, image, video, and audio services")
	}
	byType := make(map[string]DraftItem, len(items))
	for _, item := range items {
		item.ServiceType = strings.ToLower(strings.TrimSpace(item.ServiceType))
		item.Provider = strings.ToLower(strings.TrimSpace(item.Provider))
		item.Name = strings.TrimSpace(item.Name)
		item.BaseURL = strings.TrimSpace(item.BaseURL)
		item.Model = strings.TrimSpace(item.Model)
		if _, exists := byType[item.ServiceType]; exists {
			return nil, fmt.Errorf("service type %q appears more than once", item.ServiceType)
		}
		byType[item.ServiceType] = item
	}
	normalized := make([]DraftItem, 0, len(serviceTypes))
	for _, serviceType := range serviceTypes {
		item, ok := byType[serviceType]
		if !ok {
			return nil, fmt.Errorf("bundle is missing %s service", serviceType)
		}
		normalized = append(normalized, item)
	}
	return normalized, nil
}

func Plan(tx *gorm.DB, organizationID uint, items []DraftItem, applyAgentDefaults bool) (Preview, error) {
	normalized, err := Normalize(items)
	if err != nil {
		return Preview{}, err
	}
	var rows []models.AIServiceConfig
	if err := tx.Where("organization_id = ?", organizationID).Order("id").Find(&rows).Error; err != nil {
		return Preview{}, err
	}
	preview := Preview{Items: make([]PlannedItem, 0, 4), Agents: make([]PlannedAgent, 0), Conflicts: make([]Conflict, 0)}
	for _, draft := range normalized {
		planned := PlannedItem{ServiceType: draft.ServiceType, Action: "create", Provider: draft.Provider, Name: draft.Name, IsDefault: draft.IsDefault}
		for _, row := range rows {
			if row.ServiceType != draft.ServiceType {
				continue
			}
			if row.Provider == draft.Provider && strings.TrimSpace(row.BaseURL) == draft.BaseURL && row.Model == draft.Model && row.Name == draft.Name {
				planned.Action, planned.ConfigID = "reuse", row.ID
				preview.Conflicts = append(preview.Conflicts, Conflict{ServiceType: draft.ServiceType, Kind: "reused", ConfigID: row.ID, Message: "an equivalent organization config will be reused"})
			}
			if draft.IsDefault && row.IsDefault && row.ID != planned.ConfigID {
				preview.Conflicts = append(preview.Conflicts, Conflict{ServiceType: draft.ServiceType, Kind: "default_replaced", ConfigID: row.ID, Message: "the current default will be replaced"})
			}
		}
		preview.Items = append(preview.Items, planned)
	}
	var agentRows []models.AgentConfig
	if applyAgentDefaults {
		if err := tx.Where("organization_id = ?", organizationID).Order("id").Find(&agentRows).Error; err != nil {
			return Preview{}, err
		}
		preview.Agents = planAgents(agentRows, normalized[0].Model)
	}
	preview.PreviewToken, err = tokenWithAgents(organizationID, normalized, rows, applyAgentDefaults, agentRows)
	return preview, err
}

func planAgents(rows []models.AgentConfig, textModel string) []PlannedAgent {
	byType := make(map[string]models.AgentConfig, len(rows))
	for _, row := range rows {
		byType[row.AgentType] = row
	}
	planned := make([]PlannedAgent, 0, len(agentDefaults))
	for _, item := range agentDefaults {
		plan := PlannedAgent{AgentType: item.AgentType, Action: "create", Model: textModel}
		if row, ok := byType[item.AgentType]; ok {
			plan.ConfigID = row.ID
			plan.Action = "update"
			if row.Model == textModel && row.IsActive && row.DeletedAt == nil {
				plan.Action = "reuse"
			}
		}
		planned = append(planned, plan)
	}
	return planned
}

func AgentDefault(agentType string) (name, description string, ok bool) {
	for _, item := range agentDefaults {
		if item.AgentType == agentType {
			return item.Name, item.Description, true
		}
	}
	return "", "", false
}

func Token(organizationID uint, items []DraftItem, rows []models.AIServiceConfig) (string, error) {
	return tokenWithAgents(organizationID, items, rows, false, nil)
}

func tokenWithAgents(organizationID uint, items []DraftItem, rows []models.AIServiceConfig, applyAgentDefaults bool, agentRows []models.AgentConfig) (string, error) {
	items = append([]DraftItem(nil), items...)
	rows = append([]models.AIServiceConfig(nil), rows...)
	agentRows = append([]models.AgentConfig(nil), agentRows...)
	sort.Slice(items, func(i, j int) bool { return items[i].ServiceType < items[j].ServiceType })
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	sort.Slice(agentRows, func(i, j int) bool { return agentRows[i].ID < agentRows[j].ID })
	payload, err := json.Marshal(struct {
		OrganizationID     uint                     `json:"organization_id"`
		Items              []DraftItem              `json:"items"`
		Rows               []models.AIServiceConfig `json:"rows"`
		ApplyAgentDefaults bool                     `json:"apply_agent_defaults"`
		AgentRows          []models.AgentConfig     `json:"agent_rows"`
	}{organizationID, items, rows, applyAgentDefaults, agentRows})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
