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
		{name: "private AI hosts", mutate: func(c *Config) { c.AI.AllowedPrivateBaseURLHosts = []string{"localhost"} }},
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

func TestLoadAppliesPrivateAIHostAllowlistEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("database:\n  path: test.db\nstorage:\n  local_path: storage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_ALLOWED_PRIVATE_BASE_URL_HOSTS", "localhost, host.docker.internal,127.0.0.1")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AI.AllowedPrivateBaseURLHosts) != 3 || cfg.AI.AllowedPrivateBaseURLHosts[1] != "host.docker.internal" {
		t.Fatalf("allowlist=%v", cfg.AI.AllowedPrivateBaseURLHosts)
	}
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

func TestLoadAppliesAllRuntimeOverridesAndDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("app:\n  name: test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(dir, "db", "runtime.db")
	storagePath := filepath.Join(dir, "runtime-storage")
	for key, value := range map[string]string{
		"AUTH_ENABLED": "true", "DATABASE_PATH": databasePath, "STORAGE_LOCAL_PATH": storagePath,
		"WEBHOOK_SECRET": "runtime-webhook", "SMTP_HOST": "smtp.runtime.test", "SMTP_PORT": "465",
		"SMTP_USERNAME": "mailer", "SMTP_PASSWORD": "password", "EMAIL_FROM": "mail@example.test",
		"PASSWORD_RESET_URL_BASE": "https://app.example.test/reset",
	} {
		t.Setenv(key, value)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Auth.Enabled || cfg.Auth.CookieName != "flyaimovie_session" || cfg.Auth.SessionTTLHours != 24 || cfg.Server.Port != 5679 || cfg.Server.Host != "0.0.0.0" {
		t.Fatalf("defaults=%+v", cfg)
	}
	if cfg.Database.Path != databasePath || cfg.Storage.LocalPath != storagePath || cfg.Server.WebhookSecret != "runtime-webhook" {
		t.Fatalf("paths/server=%+v", cfg)
	}
	if cfg.Email.SMTPPort != 465 || cfg.Email.SMTPHost != "smtp.runtime.test" || cfg.Email.ResetURLBase != "https://app.example.test/reset" {
		t.Fatalf("email=%+v", cfg.Email)
	}
}

func TestLoadRejectsMissingAndMalformedConfig(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("missing config accepted")
	}
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("server: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("malformed config accepted")
	}
}

func TestProductionValidationRejectsNilAndInsecureResetURL(t *testing.T) {
	var cfg *Config
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatal("nil config accepted")
	}
	if err := (EmailConfig{SMTPHost: "smtp", SMTPPort: 587, SMTPUsername: "user", SMTPPassword: "pass", From: "from@example.test", ResetURLBase: "http://insecure.test"}).ValidateProduction(); err == nil {
		t.Fatal("insecure reset URL accepted")
	}
}
