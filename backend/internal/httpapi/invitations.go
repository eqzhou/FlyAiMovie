package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type InvitationSender interface {
	SendInvitation(email, organizationName, role, token string, expiresAt time.Time) error
}

type NoopInvitationSender struct{}

var (
	errInvitationPasswordInvalid = errors.New("new_password must contain 12-72 bytes")
	errInvitationCurrentPassword = errors.New("current_password is invalid")
	errInvitationAlreadyMember   = errors.New("user is already a member")
	errInvitationAlreadyAccepted = errors.New("invitation already accepted")
)

func (NoopInvitationSender) SendInvitation(string, string, string, string, time.Time) error {
	return errors.New("invitation email sender is not configured")
}

func (s *Server) registerPublicInvitations(api *gin.RouterGroup) {
	api.GET("/auth/invitations/:token", s.getInvitation)
	api.POST("/auth/invitations/:token/accept", s.acceptInvitation)
}

func (s *Server) registerOrganizationInvitations(group *gin.RouterGroup) {
	group.GET("/invitations", s.listOrganizationInvitations)
	group.POST("/invitations", s.createOrganizationInvitation)
	group.DELETE("/invitations/:id", s.revokeOrganizationInvitation)
	group.POST("/invitations/:id/resend", s.resendOrganizationInvitation)
}

func (s *Server) createOrganizationInvitation(c *gin.Context) {
	actor, ok := requireOrganizationAdmin(c)
	if !ok {
		return
	}
	var body struct {
		Email    string `json:"email"`
		Role     string `json:"role"`
		TTLHours int    `json:"ttl_hours"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	email, err := normalizeEmail(body.Email)
	body.Role = strings.ToLower(strings.TrimSpace(body.Role))
	if err != nil || !assignableRole(actor.Membership.Role, body.Role) {
		response.BadRequest(c, "valid email and assignable role are required")
		return
	}
	var existing models.User
	if db.DB.Where("email = ?", email).First(&existing).Error == nil {
		var count int64
		if err := db.DB.Model(&models.Membership{}).Where("organization_id = ? AND user_id = ?", actor.Organization.ID, existing.ID).Count(&count).Error; err != nil {
			response.ServerError(c, "failed to check membership")
			return
		}
		if count > 0 {
			response.Conflict(c, "user is already a member")
			return
		}
	}
	if body.TTLHours == 0 {
		body.TTLHours = 72
	}
	if body.TTLHours < 1 || body.TTLHours > 168 {
		response.BadRequest(c, "ttl_hours must be between 1 and 168")
		return
	}
	token, err := randomToken()
	if err != nil {
		response.ServerError(c, "failed to create invitation")
		return
	}
	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(body.TTLHours) * time.Hour)
	invitation := models.OrganizationInvitation{
		OrganizationID: actor.Organization.ID, InvitedBy: actor.User.ID, Email: email, Role: body.Role,
		TokenHash: tokenHash(token), ExpiresAt: expiresAt.Format(time.RFC3339), CreatedAt: now.Format(time.RFC3339),
	}
	if err := db.DB.Create(&invitation).Error; err != nil {
		response.ServerError(c, "failed to create invitation")
		return
	}
	emailSent := s.deliverInvitationEmail(actor.Organization.Name, invitation.Email, invitation.Role, token, expiresAt)
	response.Created(c, gin.H{"id": invitation.ID, "email": invitation.Email, "role": invitation.Role, "expires_at": invitation.ExpiresAt, "token": token, "email_sent": emailSent})
}

func (s *Server) listOrganizationInvitations(c *gin.Context) {
	actor, ok := requireOrganizationAdmin(c)
	if !ok {
		return
	}
	var rows []models.OrganizationInvitation
	if err := db.DB.Where("organization_id = ?", actor.Organization.ID).Order("id desc").Limit(100).Find(&rows).Error; err != nil {
		response.ServerError(c, "failed to load invitations")
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		out = append(out, invitationResponse(row))
	}
	response.Success(c, out)
}

func invitationResponse(row models.OrganizationInvitation) gin.H {
	status := "pending"
	if row.AcceptedAt != nil {
		status = "accepted"
	} else if row.RevokedAt != nil {
		status = "revoked"
	} else if row.ExpiresAt <= time.Now().UTC().Format(time.RFC3339) {
		status = "expired"
	}
	return gin.H{"id": row.ID, "email": row.Email, "role": row.Role, "expires_at": row.ExpiresAt, "accepted_at": row.AcceptedAt, "revoked_at": row.RevokedAt, "status": status, "created_at": row.CreatedAt}
}

func (s *Server) revokeOrganizationInvitation(c *gin.Context) {
	actor, ok := requireOrganizationAdmin(c)
	if !ok {
		return
	}
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid invitation id")
		return
	}
	now := response.Now()
	result := db.DB.Model(&models.OrganizationInvitation{}).Where("id = ? AND organization_id = ? AND accepted_at IS NULL AND revoked_at IS NULL", id, actor.Organization.ID).Updates(map[string]any{"revoked_at": now})
	if result.Error != nil {
		response.ServerError(c, "failed to revoke invitation")
		return
	}
	if result.RowsAffected == 0 {
		response.NotFound(c, "pending invitation not found")
		return
	}
	response.Success(c, gin.H{"id": id, "status": "revoked"})
}

func (s *Server) resendOrganizationInvitation(c *gin.Context) {
	actor, ok := requireOrganizationAdmin(c)
	if !ok {
		return
	}
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid invitation id")
		return
	}
	var previous models.OrganizationInvitation
	if err := db.DB.Where("id = ? AND organization_id = ? AND accepted_at IS NULL AND revoked_at IS NULL", id, actor.Organization.ID).First(&previous).Error; err != nil {
		response.NotFound(c, "pending invitation not found")
		return
	}
	if previous.ExpiresAt <= time.Now().UTC().Format(time.RFC3339) {
		// Expired invitations are still eligible for an explicit resend.
	}
	token, err := randomToken()
	if err != nil {
		response.ServerError(c, "failed to create invitation")
		return
	}
	now := time.Now().UTC()
	expiresAt := now.Add(72 * time.Hour)
	newInvitation := models.OrganizationInvitation{
		OrganizationID: actor.Organization.ID, InvitedBy: actor.User.ID, Email: previous.Email, Role: previous.Role,
		TokenHash: tokenHash(token), ExpiresAt: expiresAt.Format(time.RFC3339), CreatedAt: now.Format(time.RFC3339),
	}
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.OrganizationInvitation{}).Where("id = ? AND organization_id = ? AND accepted_at IS NULL AND revoked_at IS NULL", id, actor.Organization.ID).Update("revoked_at", now.Format(time.RFC3339)).Error; err != nil {
			return err
		}
		return tx.Create(&newInvitation).Error
	})
	if err != nil {
		response.ServerError(c, "failed to resend invitation")
		return
	}
	emailSent := s.deliverInvitationEmail(actor.Organization.Name, newInvitation.Email, newInvitation.Role, token, expiresAt)
	response.Created(c, gin.H{"id": newInvitation.ID, "email": newInvitation.Email, "role": newInvitation.Role, "expires_at": newInvitation.ExpiresAt, "token": token, "status": "pending", "email_sent": emailSent})
}

func (s *Server) deliverInvitationEmail(organizationName, email, role, token string, expiresAt time.Time) bool {
	sender := s.InviteSender
	if sender == nil {
		sender = NoopInvitationSender{}
	}
	if err := sender.SendInvitation(email, organizationName, role, token, expiresAt); err != nil {
		log.Printf("invitation email delivery failed for %s: %v", email, err)
		return false
	}
	return true
}

func findInvitation(token string) (models.OrganizationInvitation, models.Organization, error) {
	if strings.TrimSpace(token) == "" {
		return models.OrganizationInvitation{}, models.Organization{}, errors.New("invitation token is required")
	}
	var invitation models.OrganizationInvitation
	if err := db.DB.Where("token_hash = ? AND accepted_at IS NULL AND revoked_at IS NULL", tokenHash(token)).First(&invitation).Error; err != nil {
		return invitation, models.Organization{}, errors.New("invitation not found")
	}
	if invitation.ExpiresAt <= time.Now().UTC().Format(time.RFC3339) {
		return invitation, models.Organization{}, errors.New("invitation expired")
	}
	var organization models.Organization
	if err := db.DB.Where("id = ? AND status = ?", invitation.OrganizationID, "active").First(&organization).Error; err != nil {
		return invitation, organization, errors.New("organization not found")
	}
	return invitation, organization, nil
}

func (s *Server) getInvitation(c *gin.Context) {
	invitation, organization, err := findInvitation(c.Param("token"))
	if err != nil {
		response.NotFound(c, err.Error())
		return
	}
	response.Success(c, gin.H{"email": invitation.Email, "organization": gin.H{"name": organization.Name, "slug": organization.Slug}, "role": invitation.Role, "expires_at": invitation.ExpiresAt})
}

func (s *Server) acceptInvitation(c *gin.Context) {
	invitation, organization, err := findInvitation(c.Param("token"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	var body struct {
		Email           string `json:"email"`
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		DisplayName     string `json:"display_name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	email, err := normalizeEmail(body.Email)
	if err != nil || email != invitation.Email {
		response.BadRequest(c, "email does not match invitation")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var user models.User
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		userErr := tx.Where("email = ?", email).First(&user).Error
		if errors.Is(userErr, gorm.ErrRecordNotFound) {
			if !validPassword(body.NewPassword) {
				return errInvitationPasswordInvalid
			}
			hash, hashErr := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
			if hashErr != nil {
				return hashErr
			}
			name := strings.TrimSpace(body.DisplayName)
			if name == "" {
				name = email
			}
			user = models.User{Email: email, PasswordHash: string(hash), DisplayName: name, Status: "active", CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}
		} else if userErr != nil {
			return userErr
		} else if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.CurrentPassword)) != nil {
			return errInvitationCurrentPassword
		}
		var count int64
		if err := tx.Model(&models.Membership{}).Where("organization_id = ? AND user_id = ?", organization.ID, user.ID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errInvitationAlreadyMember
		}
		result := tx.Model(&models.OrganizationInvitation{}).Where("id = ? AND accepted_at IS NULL", invitation.ID).Updates(map[string]any{"accepted_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errInvitationAlreadyAccepted
		}
		return tx.Create(&models.Membership{OrganizationID: organization.ID, UserID: user.ID, Role: invitation.Role, CreatedAt: now, UpdatedAt: now}).Error
	})
	if err != nil {
		if errors.Is(err, errInvitationPasswordInvalid) || errors.Is(err, errInvitationCurrentPassword) || errors.Is(err, errInvitationAlreadyMember) || errors.Is(err, errInvitationAlreadyAccepted) {
			response.BadRequest(c, err.Error())
			return
		}
		response.ServerError(c, "failed to accept invitation")
		return
	}
	token, csrf, err := s.createSession(user.ID, organization.ID)
	if err != nil {
		response.ServerError(c, "failed to create session")
		return
	}
	s.setSessionCookie(c, token)
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "accepted", "data": authResponse(user, organization, invitation.Role, csrf)})
}
