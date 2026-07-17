package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsRateLimitPerMinute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	contents := fmt.Sprintf("app:\n  name: test\ndatabase:\n  path: %q\nstorage:\n  local_path: %q\n", filepath.Join(dir, "test.db"), filepath.Join(dir, "storage"))
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.RateLimitPerMinute != 600 {
		t.Fatalf("rate_limit_per_minute=%d want 600", cfg.Server.RateLimitPerMinute)
	}
}

func TestValidateProduction(t *testing.T) {
	t.Setenv("AI_CONFIG_ENCRYPTION_KEY", "test-encryption-key")
	cfg := &Config{
		App:    AppConfig{Debug: false},
		Server: ServerConfig{WebhookSecret: "webhook-secret"},
		Auth:   AuthConfig{Enabled: true, SecureCookies: true},
		Email:  EmailConfig{SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPUsername: "mailer", SMTPPassword: "secret", From: "noreply@example.com", ResetURLBase: "https://app.example.com"},
	}
	if err := cfg.ValidateProduction(); err != nil {
		t.Fatalf("valid production config: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "debug", mutate: func(c *Config) { c.App.Debug = true }},
		{name: "auth disabled", mutate: func(c *Config) { c.Auth.Enabled = false }},
		{name: "insecure cookie", mutate: func(c *Config) { c.Auth.SecureCookies = false }},
		{name: "missing webhook secret", mutate: func(c *Config) { c.Server.WebhookSecret = "" }},
		{name: "missing email", mutate: func(c *Config) { c.Email.SMTPHost = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			copy := *cfg
			tc.mutate(&copy)
			if err := copy.ValidateProduction(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	t.Run("missing encryption key", func(t *testing.T) {
		t.Setenv("AI_CONFIG_ENCRYPTION_KEY", "")
		if err := cfg.ValidateProduction(); err == nil {
			t.Fatal("expected validation error")
		}
	})
}

func TestLoadAppliesDatabaseAndStorageEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("database:\n  type: sqlite\n  path: old.db\nstorage:\n  local_path: old-storage\nauth:\n  secure_cookies: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_TYPE", "postgres")
	t.Setenv("DATABASE_DSN", "host=db dbname=flyaimovie")
	t.Setenv("STORAGE_LOCAL_PATH", filepath.Join(dir, "media"))
	t.Setenv("AUTH_SECURE_COOKIES", "false")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Type != "postgres" || cfg.Database.DSN != "host=db dbname=flyaimovie" || cfg.Auth.SecureCookies || cfg.Storage.LocalPath != filepath.Join(dir, "media") {
		t.Fatalf("config=%+v", cfg)
	}
}
