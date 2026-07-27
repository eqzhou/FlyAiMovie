package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	errExistingAccountRequiresInvitation = errors.New("existing accounts require a verified invitation")
	errNewUserPasswordInvalid            = errors.New("new users require a password of 8-72 bytes")
	errUserAlreadyMember                 = errors.New("user is already a member")
)

func (s *Server) registerOrganizationMembers(api *gin.RouterGroup) {
	group := api.Group("/organization/members")
	group.GET("", s.listOrganizationMembers)
	group.POST("", s.createOrganizationMember)
	group.PUT("/:userID", s.updateOrganizationMember)
	group.DELETE("/:userID", s.deleteOrganizationMember)
	group.PUT("/:userID/password", s.resetOrganizationMemberPassword)
	s.registerOrganizationInvitations(group)
}

func requireOrganizationAdmin(c *gin.Context) (authContext, bool) {
	actor, ok := currentAuth(c)
	if !ok || (actor.Membership.Role != "owner" && actor.Membership.Role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "admin role required"})
		return actor, false
	}
	return actor, true
}

func (s *Server) listOrganizationMembers(c *gin.Context) {
	actor, ok := requireOrganizationAdmin(c)
	if !ok {
		return
	}
	var memberships []models.Membership
	if err := db.DB.Where("organization_id = ?", actor.Organization.ID).Order("created_at").Find(&memberships).Error; err != nil {
		response.ServerError(c, "failed to load members")
		return
	}
	ids := make([]uint, 0, len(memberships))
	roles := make(map[uint]string, len(memberships))
	for _, membership := range memberships {
		ids = append(ids, membership.UserID)
		roles[membership.UserID] = membership.Role
	}
	var users []models.User
	if len(ids) > 0 {
		db.DB.Where("id IN ?", ids).Order("id").Find(&users)
	}
	out := make([]gin.H, 0, len(users))
	for _, user := range users {
		out = append(out, gin.H{"user_id": user.ID, "email": user.Email, "display_name": user.DisplayName, "status": user.Status, "role": roles[user.ID]})
	}
	response.Success(c, out)
}

func (s *Server) createOrganizationMember(c *gin.Context) {
	actor, ok := requireOrganizationAdmin(c)
	if !ok {
		return
	}
	var body struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
		Role        string `json:"role"`
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
	now := response.Now()
	var user models.User
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Where("email = ?", email).First(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if !validPassword(body.Password) {
				return errNewUserPasswordInvalid
			}
			hash, hashErr := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
			if hashErr != nil {
				return hashErr
			}
			displayName := strings.TrimSpace(body.DisplayName)
			if displayName == "" {
				displayName = email
			}
			user = models.User{Email: email, DisplayName: displayName, PasswordHash: string(hash), Status: "active", CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			return errExistingAccountRequiresInvitation
		}
		var count int64
		if err := tx.Model(&models.Membership{}).Where("organization_id = ? AND user_id = ?", actor.Organization.ID, user.ID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errUserAlreadyMember
		}
		return tx.Create(&models.Membership{OrganizationID: actor.Organization.ID, UserID: user.ID, Role: body.Role, CreatedAt: now, UpdatedAt: now}).Error
	})
	if err != nil {
		if errors.Is(err, errExistingAccountRequiresInvitation) {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
			return
		}
		if errors.Is(err, errNewUserPasswordInvalid) || errors.Is(err, errUserAlreadyMember) {
			response.BadRequest(c, err.Error())
			return
		}
		response.ServerError(c, "failed to create member")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": http.StatusCreated, "message": "created", "data": gin.H{"user_id": user.ID, "email": user.Email, "display_name": user.DisplayName, "role": body.Role}})
}

func (s *Server) updateOrganizationMember(c *gin.Context) {
	actor, ok := requireOrganizationAdmin(c)
	if !ok {
		return
	}
	userID, err := strconv.ParseUint(c.Param("userID"), 10, 64)
	if err != nil || userID == 0 {
		response.BadRequest(c, "invalid user id")
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	body.Role = strings.ToLower(strings.TrimSpace(body.Role))
	if !assignableRole(actor.Membership.Role, body.Role) {
		response.BadRequest(c, "role cannot be assigned")
		return
	}
	var membership models.Membership
	if err := db.DB.Where("organization_id = ? AND user_id = ?", actor.Organization.ID, uint(userID)).First(&membership).Error; err != nil {
		response.NotFound(c, "member not found")
		return
	}
	if membership.Role == "owner" || (membership.Role == "admin" && actor.Membership.Role != "owner") {
		response.BadRequest(c, "member role is protected")
		return
	}
	if err := db.DB.Model(&membership).Updates(map[string]any{"role": body.Role, "updated_at": response.Now()}).Error; err != nil {
		response.ServerError(c, "failed to update member")
		return
	}
	response.Success(c, gin.H{"user_id": userID, "role": body.Role})
}

func (s *Server) deleteOrganizationMember(c *gin.Context) {
	actor, ok := requireOrganizationAdmin(c)
	if !ok {
		return
	}
	userID, err := strconv.ParseUint(c.Param("userID"), 10, 64)
	if err != nil || userID == 0 {
		response.BadRequest(c, "invalid user id")
		return
	}
	var membership models.Membership
	if err := db.DB.Where("organization_id = ? AND user_id = ?", actor.Organization.ID, uint(userID)).First(&membership).Error; err != nil {
		response.NotFound(c, "member not found")
		return
	}
	if membership.Role == "owner" || (membership.Role == "admin" && actor.Membership.Role != "owner") || actor.User.ID == uint(userID) {
		response.BadRequest(c, "member cannot be removed")
		return
	}
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("organization_id = ? AND user_id = ?", actor.Organization.ID, uint(userID)).Delete(&models.Membership{}).Error; err != nil {
			return err
		}
		return tx.Model(&models.Session{}).Where("organization_id = ? AND user_id = ? AND revoked_at IS NULL", actor.Organization.ID, uint(userID)).Update("revoked_at", response.Now()).Error
	})
	if err != nil {
		response.ServerError(c, "failed to remove member")
		return
	}
	response.Success(c, nil)
}

func assignableRole(actorRole, targetRole string) bool {
	if targetRole != "admin" && targetRole != "editor" && targetRole != "viewer" {
		return false
	}
	return actorRole == "owner" || (actorRole == "admin" && targetRole != "admin")
}

func (s *Server) resetOrganizationMemberPassword(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{
		"code":    http.StatusForbidden,
		"message": "organization administrators cannot change global account credentials",
	})
}

func (s *Server) authOrganizations(c *gin.Context) {
	actor, _ := currentAuth(c)
	var memberships []models.Membership
	db.DB.Where("user_id = ?", actor.User.ID).Find(&memberships)
	out := make([]gin.H, 0, len(memberships))
	for _, membership := range memberships {
		var organization models.Organization
		if db.DB.Where("id = ? AND status = ?", membership.OrganizationID, "active").First(&organization).Error == nil {
			out = append(out, gin.H{"id": organization.ID, "name": organization.Name, "slug": organization.Slug, "role": membership.Role, "current": organization.ID == actor.Organization.ID})
		}
	}
	response.Success(c, out)
}

func (s *Server) authSwitchOrganization(c *gin.Context) {
	actor, _ := currentAuth(c)
	var body struct {
		OrganizationID uint `json:"organization_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.OrganizationID == 0 {
		response.BadRequest(c, "organization_id is required")
		return
	}
	var membership models.Membership
	if err := db.DB.Where("organization_id = ? AND user_id = ?", body.OrganizationID, actor.User.ID).First(&membership).Error; err != nil || !validRole(membership.Role) {
		response.NotFound(c, "organization not found")
		return
	}
	var organization models.Organization
	if err := db.DB.Where("id = ? AND status = ?", body.OrganizationID, "active").First(&organization).Error; err != nil {
		response.NotFound(c, "organization not found")
		return
	}
	var token, csrf string
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		var createErr error
		token, csrf, createErr = createSessionRecord(tx, actor.User.ID, organization.ID, s.Cfg.Auth.SessionTTLHours)
		if createErr != nil {
			return createErr
		}
		result := tx.Model(&models.Session{}).Where("token_hash = ? AND revoked_at IS NULL", actor.Session.TokenHash).Update("revoked_at", response.Now())
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("active session not found")
		}
		return nil
	})
	if err != nil {
		response.ServerError(c, "failed to switch organization")
		return
	}
	s.setSessionCookie(c, token)
	response.Success(c, authResponse(actor.User, organization, membership.Role, csrf))
}

func (s *Server) authChangePassword(c *gin.Context) {
	actor, _ := currentAuth(c)
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || !validPassword(body.NewPassword) {
		response.BadRequest(c, "new password must contain 8-72 bytes")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(actor.User.PasswordHash), []byte(body.CurrentPassword)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "current password is incorrect"})
		return
	}
	if body.CurrentPassword == body.NewPassword {
		response.BadRequest(c, "new password must be different")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		response.ServerError(c, "failed to secure password")
		return
	}
	now := response.Now()
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.User{}).Where("id = ?", actor.User.ID).Updates(map[string]any{"password_hash": string(hash), "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.PasswordResetToken{}).Where("user_id = ? AND consumed_at IS NULL", actor.User.ID).Update("consumed_at", now).Error; err != nil {
			return err
		}
		return tx.Model(&models.Session{}).Where("user_id = ? AND revoked_at IS NULL", actor.User.ID).Update("revoked_at", now).Error
	}); err != nil {
		response.ServerError(c, "failed to change password")
		return
	}
	token, csrf, err := s.createSession(actor.User.ID, actor.Organization.ID)
	if err != nil {
		response.ServerError(c, "password changed; please sign in again")
		return
	}
	s.setSessionCookie(c, token)
	actor.User.PasswordHash = string(hash)
	response.Success(c, authResponse(actor.User, actor.Organization, actor.Membership.Role, csrf))
}
