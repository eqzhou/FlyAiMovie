package security

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEncryptDecryptSecret(t *testing.T) {
	t.Setenv("AI_CONFIG_ENCRYPTION_KEY", "test-key-material")
	ciphertext, err := EncryptSecret("provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ciphertext, encryptedPrefix) || strings.Contains(ciphertext, "provider-secret") {
		t.Fatalf("secret was not encrypted: %q", ciphertext)
	}
	plaintext, err := DecryptSecret(ciphertext)
	if err != nil || plaintext != "provider-secret" {
		t.Fatalf("decrypt=%q err=%v", plaintext, err)
	}
}

func TestEncryptSecretLegacyWithoutKey(t *testing.T) {
	if err := os.Unsetenv("AI_CONFIG_ENCRYPTION_KEY"); err != nil {
		t.Fatal(err)
	}
	value, err := EncryptSecret("mock")
	if err != nil || value != "mock" {
		t.Fatalf("value=%q err=%v", value, err)
	}
	value, err = DecryptSecret(value)
	if err != nil || value != "mock" {
		t.Fatalf("legacy value=%q err=%v", value, err)
	}
}

func TestDecryptEncryptedSecretRequiresKey(t *testing.T) {
	t.Setenv("AI_CONFIG_ENCRYPTION_KEY", "test-key-material")
	ciphertext, err := EncryptSecret("provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("AI_CONFIG_ENCRYPTION_KEY"); err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptSecret(ciphertext); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestMigrateAIConfigSecrets(t *testing.T) {
	t.Setenv("AI_CONFIG_ENCRYPTION_KEY", "migration-key")
	database, err := gorm.Open(sqlite.Open(t.TempDir()+"/secrets.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.AIServiceConfig{}); err != nil {
		t.Fatal(err)
	}
	var row models.AIServiceConfig
	if err := json.Unmarshal([]byte(`{"service_type":"image","name":"provider","base_url":"https://example.test","api_key":"test-placeholder-key"}`), &row); err != nil {
		t.Fatal(err)
	}
	row.CreatedAt, row.UpdatedAt = "now", "now"
	if err := database.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateAIConfigSecrets(database); err != nil {
		t.Fatal(err)
	}
	var stored models.AIServiceConfig
	if err := database.First(&stored, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored.APIKey, encryptedPrefix) || strings.Contains(stored.APIKey, "test-placeholder-key") {
		t.Fatalf("not migrated: %q", stored.APIKey)
	}
	plaintext, err := DecryptSecret(stored.APIKey)
	if err != nil || plaintext != "test-placeholder-key" {
		t.Fatalf("decrypt=%q err=%v", plaintext, err)
	}
	if err := MigrateAIConfigSecrets(database); err != nil {
		t.Fatalf("migration is not idempotent: %v", err)
	}
}
