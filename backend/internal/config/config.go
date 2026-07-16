package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App      AppConfig      `yaml:"app"`
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Storage  StorageConfig  `yaml:"storage"`
	AI       AIConfig       `yaml:"ai"`
	Auth     AuthConfig     `yaml:"auth"`
	Email    EmailConfig    `yaml:"email"`
}

type AppConfig struct {
	Name     string `yaml:"name"`
	Version  string `yaml:"version"`
	Debug    bool   `yaml:"debug"`
	Language string `yaml:"language"`
}

type ServerConfig struct {
	Port               int      `yaml:"port"`
	Host               string   `yaml:"host"`
	CORSOrigins        []string `yaml:"cors_origins"`
	WebhookSecret      string   `yaml:"webhook_secret"`
	ReadTimeout        int      `yaml:"read_timeout"`
	WriteTimeout       int      `yaml:"write_timeout"`
	RateLimitPerMinute int      `yaml:"rate_limit_per_minute"`
}

type DatabaseConfig struct {
	Type string `yaml:"type"`
	Path string `yaml:"path"`
}

type StorageConfig struct {
	Type      string `yaml:"type"`
	LocalPath string `yaml:"local_path"`
	BaseURL   string `yaml:"base_url"`
}

type AIConfig struct {
	DefaultTextProvider  string `yaml:"default_text_provider"`
	DefaultImageProvider string `yaml:"default_image_provider"`
	DefaultVideoProvider string `yaml:"default_video_provider"`
}

type AuthConfig struct {
	Enabled         bool   `yaml:"enabled"`
	CookieName      string `yaml:"cookie_name"`
	SecureCookies   bool   `yaml:"secure_cookies"`
	SessionTTLHours int    `yaml:"session_ttl_hours"`
}

type EmailConfig struct {
	SMTPHost     string `yaml:"smtp_host"`
	SMTPPort     int    `yaml:"smtp_port"`
	SMTPUsername string `yaml:"smtp_username"`
	SMTPPassword string `yaml:"smtp_password"`
	From         string `yaml:"from"`
	ResetURLBase string `yaml:"reset_url_base"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 5679
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.RateLimitPerMinute <= 0 {
		cfg.Server.RateLimitPerMinute = 600
	}
	if cfg.Auth.CookieName == "" {
		cfg.Auth.CookieName = "flyaimovie_session"
	}
	if cfg.Auth.SessionTTLHours <= 0 {
		cfg.Auth.SessionTTLHours = 24
	}
	if enabled := os.Getenv("AUTH_ENABLED"); enabled != "" {
		cfg.Auth.Enabled = enabled == "1" || strings.EqualFold(enabled, "true")
	}
	if secret := os.Getenv("WEBHOOK_SECRET"); secret != "" {
		cfg.Server.WebhookSecret = secret
	}
	if value := os.Getenv("SMTP_HOST"); value != "" {
		cfg.Email.SMTPHost = value
	}
	if value := os.Getenv("SMTP_PORT"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.Email.SMTPPort = parsed
		}
	}
	if value := os.Getenv("SMTP_USERNAME"); value != "" {
		cfg.Email.SMTPUsername = value
	}
	if value := os.Getenv("SMTP_PASSWORD"); value != "" {
		cfg.Email.SMTPPassword = value
	}
	if value := os.Getenv("EMAIL_FROM"); value != "" {
		cfg.Email.From = value
	}
	if value := os.Getenv("PASSWORD_RESET_URL_BASE"); value != "" {
		cfg.Email.ResetURLBase = value
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./data/flyaimovie.db"
	}
	if cfg.Storage.LocalPath == "" {
		cfg.Storage.LocalPath = "./data/storage"
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.Storage.LocalPath, 0o755); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ValidateProduction rejects development defaults that would expose business
// data or credentials on a public deployment.
func (c *Config) ValidateProduction() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	if c.App.Debug {
		return fmt.Errorf("app.debug must be false in production")
	}
	if !c.Auth.Enabled {
		return fmt.Errorf("auth.enabled must be true in production")
	}
	if !c.Auth.SecureCookies {
		return fmt.Errorf("auth.secure_cookies must be true in production")
	}
	if strings.TrimSpace(c.Server.WebhookSecret) == "" {
		return fmt.Errorf("WEBHOOK_SECRET is required in production")
	}
	if strings.TrimSpace(os.Getenv("AI_CONFIG_ENCRYPTION_KEY")) == "" {
		return fmt.Errorf("AI_CONFIG_ENCRYPTION_KEY is required in production")
	}
	if err := c.Email.ValidateProduction(); err != nil {
		return err
	}
	return nil
}

func (e EmailConfig) ValidateProduction() error {
	if strings.TrimSpace(e.SMTPHost) == "" || e.SMTPPort <= 0 || strings.TrimSpace(e.SMTPUsername) == "" || strings.TrimSpace(e.SMTPPassword) == "" || strings.TrimSpace(e.From) == "" {
		return fmt.Errorf("SMTP_HOST, SMTP_PORT, SMTP_USERNAME, SMTP_PASSWORD and EMAIL_FROM are required in production")
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(e.ResetURLBase)), "https://") {
		return fmt.Errorf("PASSWORD_RESET_URL_BASE must be an https URL in production")
	}
	return nil
}
