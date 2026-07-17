package db

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Open(path string) (*gorm.DB, error) {
	return OpenDatabase("sqlite", path, "")
}

func OpenDatabase(databaseType, path, dsn string) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch databaseType {
	case "", "sqlite":
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		dialector = sqlite.Open(path)
	case "postgres", "postgresql":
		if dsn == "" {
			return nil, fmt.Errorf("postgres DSN is required")
		}
		dialector = postgres.Open(dsn)
	default:
		return nil, fmt.Errorf("unsupported database type %q", databaseType)
	}
	gdb, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", databaseType, err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	if databaseType == "" || databaseType == "sqlite" {
		if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL;"); err != nil {
			log.Printf("warn: enable WAL: %v", err)
		}
	}
	DB = gdb
	return gdb, nil
}

func AutoMigrate(gdb *gorm.DB) error {
	if err := gdb.AutoMigrate(
		&models.Organization{},
		&models.User{},
		&models.Membership{},
		&models.OrganizationInvitation{},
		&models.PasswordResetToken{},
		&models.Session{},
		&models.AuditLog{},
		&models.OrganizationQuota{},
		&models.Drama{},
		&models.Episode{},
		&models.Character{},
		&models.CharacterTemplate{},
		&models.EpisodeCharacter{},
		&models.EpisodeScene{},
		&models.Scene{},
		&models.Storyboard{},
		&models.StoryboardCharacter{},
		&models.AIServiceConfig{},
		&models.AIServiceProvider{},
		&models.AIVoice{},
		&models.AgentConfig{},
		&models.AgentRun{},
		&models.AgentRunEvent{},
		&models.ImageGeneration{},
		&models.VideoGeneration{},
		&models.VideoMerge{},
		&models.Prop{},
		&models.Asset{},
		&models.GridHistory{},
		&models.WebhookReceipt{},
		&models.GenerationJob{},
		&models.JobEvent{},
		&models.MediaMigration{},
		&models.MediaDeletionTask{},
	); err != nil {
		return err
	}
	// Replaced by the organization-scoped composite unique index.
	return gdb.Exec("DROP INDEX IF EXISTS idx_ai_voices_voice_id").Error
}

func SeedDefaults(gdb *gorm.DB) error {
	ts := response.Now()
	providers := []models.AIServiceProvider{
		{Name: "openai-text", DisplayName: "OpenAI / Compatible Text", ServiceType: "text", Provider: "openai", DefaultURL: "https://api.openai.com", PresetModels: `["gpt-4o","gpt-4o-mini"]`, Description: "OpenAI-compatible chat completions", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{Name: "local-openai-text", DisplayName: "Local OpenAI-compatible Text", ServiceType: "text", Provider: "openai_local", DefaultURL: "http://host.docker.internal:11434", PresetModels: `["qwen2.5:latest","llama3.2:latest"]`, Description: "Explicitly allowlisted local text endpoint", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{Name: "openai-image", DisplayName: "OpenAI Image", ServiceType: "image", Provider: "openai", DefaultURL: "https://api.openai.com", PresetModels: `["gpt-image-1","dall-e-3"]`, Description: "OpenAI image generation", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{Name: "chatfire-image", DisplayName: "Chatfire Image Gateway", ServiceType: "image", Provider: "chatfire", DefaultURL: "", PresetModels: `[]`, Description: "OpenAI-compatible gateway URL required", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{Name: "gemini-image", DisplayName: "Gemini Image", ServiceType: "image", Provider: "gemini", DefaultURL: "https://generativelanguage.googleapis.com", PresetModels: `["gemini-2.0-flash-preview-image-generation"]`, Description: "Google Gemini generateContent image API", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{Name: "minimax-image", DisplayName: "MiniMax Image", ServiceType: "image", Provider: "minimax", DefaultURL: "https://api.minimax.chat", PresetModels: `["image-01"]`, Description: "MiniMax image_generation API", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{Name: "volcengine-image", DisplayName: "Volcengine Image", ServiceType: "image", Provider: "volcengine", DefaultURL: "https://ark.cn-beijing.volces.com", PresetModels: `["doubao-seedream-4-0-250828"]`, Description: "Volcengine Ark Seedream API", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{Name: "ali-image", DisplayName: "Aliyun Image", ServiceType: "image", Provider: "ali", DefaultURL: "https://dashscope.aliyuncs.com", PresetModels: `["wan2.1-t2i-turbo","wanx-v1"]`, Description: "Aliyun DashScope image synthesis API", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{Name: "minimax-video", DisplayName: "MiniMax Video", ServiceType: "video", Provider: "minimax", DefaultURL: "https://api.minimax.chat", PresetModels: `["video-01"]`, Description: "MiniMax video", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{Name: "openai-video", DisplayName: "OpenAI Sora Video", ServiceType: "video", Provider: "openai", DefaultURL: "https://api.openai.com", PresetModels: `["sora-2","sora-2-pro"]`, Description: "OpenAI Videos API", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{Name: "volcengine-video", DisplayName: "Volcengine / Seedance", ServiceType: "video", Provider: "volcengine", DefaultURL: "https://ark.cn-beijing.volces.com", PresetModels: `["doubao-seedance-1-0-pro-250528"]`, Description: "Volcengine Ark content generation API", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{Name: "vidu-video", DisplayName: "Vidu Video", ServiceType: "video", Provider: "vidu", DefaultURL: "https://api.vidu.com", PresetModels: `["vidu2.0"]`, Description: "Vidu Enterprise v2 image-to-video API", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{Name: "ali-video", DisplayName: "Aliyun Video", ServiceType: "video", Provider: "ali", DefaultURL: "https://dashscope.aliyuncs.com", PresetModels: `["wan2.1-i2v-turbo"]`, Description: "Aliyun DashScope async video synthesis API", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{Name: "minimax-audio", DisplayName: "MiniMax TTS", ServiceType: "audio", Provider: "minimax", DefaultURL: "https://api.minimax.chat", PresetModels: `["speech-02-hd","speech-02-turbo"]`, Description: "MiniMax TTS", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
	}
	for _, provider := range providers {
		var existing models.AIServiceProvider
		err := gdb.Where("name = ?", provider.Name).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			if err := gdb.Create(&provider).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if err := gdb.Model(&existing).Updates(map[string]any{
			"display_name": provider.DisplayName, "service_type": provider.ServiceType,
			"provider": provider.Provider, "default_url": provider.DefaultURL,
			"preset_models": provider.PresetModels, "description": provider.Description,
			"is_active": true, "updated_at": ts,
		}).Error; err != nil {
			return err
		}
	}

	return SeedOrganizationDefaults(gdb, 0)
}

// SeedOrganizationDefaults ensures every organization can run the local Mock
// workflow independently of the one-time legacy resource migration.
func SeedOrganizationDefaults(gdb *gorm.DB, organizationID uint) error {
	ts := response.Now()
	agentDefaults := []models.AgentConfig{
		{OrganizationID: organizationID, AgentType: "script_rewriter", Name: "剧本改写", Description: "小说/大纲 → 格式化剧本", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{OrganizationID: organizationID, AgentType: "extractor", Name: "角色场景提取", Description: "从剧本提取角色与场景并去重", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{OrganizationID: organizationID, AgentType: "storyboard_breaker", Name: "分镜拆解", Description: "剧本 → 分镜序列", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{OrganizationID: organizationID, AgentType: "voice_assigner", Name: "音色分配", Description: "角色音色自动分配", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
		{OrganizationID: organizationID, AgentType: "grid_prompt_generator", Name: "图片提示词生成", Description: "角色/场景/宫格提示词", IsActive: true, CreatedAt: ts, UpdatedAt: ts},
	}
	for i := range agentDefaults {
		var existing models.AgentConfig
		err := gdb.Where("organization_id = ? AND agent_type = ? AND deleted_at IS NULL", organizationID, agentDefaults[i].AgentType).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := gdb.Create(&agentDefaults[i]).Error; err != nil {
				return fmt.Errorf("seed agent %s: %w", agentDefaults[i].AgentType, err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("find agent %s: %w", agentDefaults[i].AgentType, err)
		}
	}

	mockConfigs := []models.AIServiceConfig{
		{OrganizationID: organizationID, ServiceType: "text", Provider: "mock", Name: "mock-text", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, Priority: -10, CreatedAt: ts, UpdatedAt: ts},
		{OrganizationID: organizationID, ServiceType: "image", Provider: "mock", Name: "mock-image", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, Priority: -10, CreatedAt: ts, UpdatedAt: ts},
		{OrganizationID: organizationID, ServiceType: "video", Provider: "mock", Name: "mock-video", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, Priority: -10, CreatedAt: ts, UpdatedAt: ts},
		{OrganizationID: organizationID, ServiceType: "audio", Provider: "mock", Name: "mock-audio", BaseURL: "http://localhost", APIKey: "mock", Model: "mock", IsActive: true, Priority: -10, CreatedAt: ts, UpdatedAt: ts},
	}
	for i := range mockConfigs {
		var existing models.AIServiceConfig
		err := gdb.Where("organization_id = ? AND name = ? AND service_type = ? AND provider = ?", organizationID, mockConfigs[i].Name, mockConfigs[i].ServiceType, "mock").First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := gdb.Create(&mockConfigs[i]).Error; err != nil {
				return fmt.Errorf("seed mock service %s: %w", mockConfigs[i].ServiceType, err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("find mock service %s: %w", mockConfigs[i].ServiceType, err)
		}
	}
	return nil
}
