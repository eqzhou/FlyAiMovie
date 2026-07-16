package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
)

const maxWebhookBodyBytes int64 = 1 << 20

var webhookEventIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func (s *Server) verifyWebhook(c *gin.Context) (body []byte, duplicate bool, ok bool) {
	secret := s.Cfg.Server.WebhookSecret
	if secret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "webhooks are disabled"})
		return nil, false, false
	}
	timestampText := c.GetHeader("X-Webhook-Timestamp")
	eventID := c.GetHeader("X-Webhook-Id")
	signatureText := strings.TrimPrefix(c.GetHeader("X-Webhook-Signature"), "sha256=")
	if !webhookEventIDPattern.MatchString(eventID) {
		c.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "invalid webhook authentication"})
		return nil, false, false
	}
	timestamp, err := strconv.ParseInt(timestampText, 10, 64)
	if err != nil || time.Since(time.Unix(timestamp, 0)) > 5*time.Minute || time.Until(time.Unix(timestamp, 0)) > 5*time.Minute {
		c.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "invalid webhook authentication"})
		return nil, false, false
	}
	body, err = io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodyBytes+1))
	if err != nil || int64(len(body)) > maxWebhookBodyBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": http.StatusRequestEntityTooLarge, "message": "webhook body too large"})
		return nil, false, false
	}
	provided, err := hex.DecodeString(signatureText)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "invalid webhook authentication"})
		return nil, false, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestampText + "."))
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)
	if len(provided) != len(expected) || subtle.ConstantTimeCompare(provided, expected) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "invalid webhook authentication"})
		return nil, false, false
	}
	receipt := models.WebhookReceipt{
		EventID: eventID, SignatureHash: hex.EncodeToString(expected), ReceivedAt: response.Now(),
	}
	if err := db.DB.Create(&receipt).Error; err != nil {
		var existing models.WebhookReceipt
		if findErr := db.DB.First(&existing, "event_id = ?", eventID).Error; findErr == nil {
			// An event id is an idempotency key, not an authorization key. Reusing
			// it with a different signed payload must not silently discard data.
			if subtle.ConstantTimeCompare([]byte(existing.SignatureHash), []byte(receipt.SignatureHash)) == 1 {
				return body, true, true
			}
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "webhook event id was already used with a different payload"})
			return nil, false, false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "failed to record webhook"})
		return nil, false, false
	}
	return body, false, true
}
