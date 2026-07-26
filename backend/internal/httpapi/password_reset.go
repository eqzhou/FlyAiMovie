package httpapi

import (
	"errors"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type PasswordResetSender interface {
	SendPasswordReset(email, token string, expiresAt time.Time) error
}

type NoopPasswordResetSender struct{}

var errInvalidResetToken = errors.New("reset token is invalid or expired")

func (NoopPasswordResetSender) SendPasswordReset(string, string, time.Time) error {
	return errors.New("password reset email sender is not configured")
}

func (s *Server) registerPasswordReset(api *gin.RouterGroup) {
	api.POST("/auth/password-reset/request", s.requestPasswordReset)
	api.POST("/auth/password-reset/consume", s.consumePasswordReset)
}

func (s *Server) requestPasswordReset(c *gin.Context) {
	var body struct {
		Email string `json:"email"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	email, err := normalizeEmail(body.Email)
	if err != nil {
		// Keep the same response as every other request to avoid email enumeration.
		response.Success(c, gin.H{"message": "if the account exists, reset instructions will be sent"})
		return
	}
	var user models.User
	if err := db.DB.Where("email = ? AND status = ?", email, "active").First(&user).Error; err != nil {
		response.Success(c, gin.H{"message": "if the account exists, reset instructions will be sent"})
		return
	}
	// Revoke outstanding tokens for this user before issuing a fresh one.
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339)
	db.DB.Model(&models.PasswordResetToken{}).Where("user_id = ? AND consumed_at IS NULL", user.ID).Update("consumed_at", nowText)
	token, err := randomToken()
	if err != nil {
		response.ServerError(c, "failed to create reset request")
		return
	}
	expires := now.Add(30 * time.Minute)
	row := models.PasswordResetToken{UserID: user.ID, TokenHash: tokenHash(token), ExpiresAt: expires.Format(time.RFC3339), CreatedAt: nowText}
	if err := db.DB.Create(&row).Error; err != nil {
		response.ServerError(c, "failed to create reset request")
		return
	}
	sender := s.ResetSender
	if sender == nil {
		sender = NoopPasswordResetSender{}
	}
	if err := sender.SendPasswordReset(user.Email, token, expires); err != nil {
		// Do not expose delivery configuration details; the token remains unusable
		// until a sender is configured, and the response stays enumeration-safe.
		_ = db.DB.Model(&row).Update("consumed_at", nowText).Error
	}
	response.Success(c, gin.H{"message": "if the account exists, reset instructions will be sent"})
}

func (s *Server) consumePasswordReset(c *gin.Context) {
	var body struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Token) == "" || !validPassword(body.NewPassword) {
		response.BadRequest(c, "token and new_password of 12-72 bytes are required")
		return
	}
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339)
	var row models.PasswordResetToken
	if err := db.DB.Where("token_hash = ? AND consumed_at IS NULL AND expires_at > ?", tokenHash(body.Token), nowText).First(&row).Error; err != nil {
		response.BadRequest(c, errInvalidResetToken.Error())
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		response.ServerError(c, "failed to secure password")
		return
	}
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.PasswordResetToken{}).Where("id = ? AND consumed_at IS NULL AND expires_at > ?", row.ID, nowText).Update("consumed_at", nowText)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errInvalidResetToken
		}
		if err := tx.Model(&models.User{}).Where("id = ? AND status = ?", row.UserID, "active").Updates(map[string]any{"password_hash": string(hash), "updated_at": nowText}).Error; err != nil {
			return err
		}
		return tx.Model(&models.Session{}).Where("user_id = ? AND revoked_at IS NULL", row.UserID).Update("revoked_at", nowText).Error
	})
	if err != nil {
		if errors.Is(err, errInvalidResetToken) {
			response.BadRequest(c, errInvalidResetToken.Error())
			return
		}
		response.ServerError(c, "failed to reset password")
		return
	}
	response.Success(c, gin.H{"message": "password updated; please log in again"})
}
