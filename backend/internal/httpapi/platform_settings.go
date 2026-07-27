package httpapi

import (
	"net/http"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
)

type platformSettingsInput struct {
	RegistrationEnabled      *bool `json:"registration_enabled"`
	RequireEmailVerification *bool `json:"require_email_verification"`
}

func requirePlatformAdmin(c *gin.Context) bool {
	actor, ok := currentAuth(c)
	if !ok || !actor.User.IsPlatformAdmin {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "platform admin required"})
		return false
	}
	return true
}

func platformSettingsResponse(settings models.PlatformSettings) gin.H {
	return gin.H{
		"registration_enabled":       settings.RegistrationEnabled,
		"require_email_verification": settings.RequireEmailVerification,
		"updated_at":                 settings.UpdatedAt,
		"updated_by":                 settings.UpdatedBy,
	}
}

func (s *Server) getPlatformSettings(c *gin.Context) {
	if !requirePlatformAdmin(c) {
		return
	}
	settings := loadPlatformSettings()
	response.Success(c, platformSettingsResponse(settings))
}

func (s *Server) putPlatformSettings(c *gin.Context) {
	if !requirePlatformAdmin(c) {
		return
	}
	var body platformSettingsInput
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	if body.RegistrationEnabled == nil || body.RequireEmailVerification == nil {
		response.BadRequest(c, "registration_enabled and require_email_verification are required")
		return
	}

	actor, _ := currentAuth(c)
	now := response.Now()
	var settings models.PlatformSettings
	if err := db.DB.First(&settings, 1).Error; err != nil {
		response.ServerError(c, "failed to load platform settings")
		return
	}
	settings.RegistrationEnabled = *body.RegistrationEnabled
	settings.RequireEmailVerification = *body.RequireEmailVerification
	settings.UpdatedAt = now
	settings.UpdatedBy = &actor.User.ID
	if err := db.DB.Save(&settings).Error; err != nil {
		response.ServerError(c, "failed to update platform settings")
		return
	}
	response.Success(c, platformSettingsResponse(settings))
}
