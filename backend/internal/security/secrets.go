package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"gorm.io/gorm"
)

const encryptedPrefix = "ENC[v1]:"

// EncryptSecret protects credentials at rest when the deployment provides
// AI_CONFIG_ENCRYPTION_KEY. Without a key, plaintext is retained for local
// mock/demo compatibility; production startup must require the key.
func EncryptSecret(value string) (string, error) {
	if value == "" || strings.HasPrefix(value, encryptedPrefix) {
		return value, nil
	}
	key := configuredKey()
	if key == nil {
		return value, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(value), nil)
	encoded := base64.RawStdEncoding.EncodeToString(append(nonce, ciphertext...))
	return encryptedPrefix + encoded, nil
}

func DecryptSecret(value string) (string, error) {
	if !strings.HasPrefix(value, encryptedPrefix) {
		return value, nil
	}
	key := configuredKey()
	if key == nil {
		return "", fmt.Errorf("encrypted secret requires AI_CONFIG_ENCRYPTION_KEY")
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, encryptedPrefix))
	if err != nil {
		return "", fmt.Errorf("decode encrypted secret: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", fmt.Errorf("encrypted secret is truncated")
	}
	plaintext, err := gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plaintext), nil
}

func configuredKey() []byte {
	value := strings.TrimSpace(os.Getenv("AI_CONFIG_ENCRYPTION_KEY"))
	if value == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

// MigrateAIConfigSecrets upgrades legacy plaintext credentials in one
// transaction. It also refuses to start with encrypted rows when the key is
// missing, avoiding silent provider authentication failures.
func MigrateAIConfigSecrets(database *gorm.DB) error {
	var rows []models.AIServiceConfig
	if err := database.Select("id", "api_key").Find(&rows).Error; err != nil {
		return err
	}
	if configuredKey() == nil {
		for _, row := range rows {
			if strings.HasPrefix(row.APIKey, encryptedPrefix) {
				return fmt.Errorf("AI_CONFIG_ENCRYPTION_KEY is required to read encrypted AI credentials")
			}
		}
		return nil
	}
	return database.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			if row.APIKey == "" || strings.HasPrefix(row.APIKey, encryptedPrefix) {
				continue
			}
			protected, err := EncryptSecret(row.APIKey)
			if err != nil {
				return err
			}
			if err := tx.Model(&models.AIServiceConfig{}).Where("id = ?", row.ID).Update("api_key", protected).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
