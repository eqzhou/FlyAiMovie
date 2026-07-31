package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const authContextKey = "flyaimovie.auth"

const (
	minPasswordBytes = 8
	maxPasswordBytes = 72 // bcrypt rejects passwords longer than 72 bytes.
)

var (
	slugPattern      = regexp.MustCompile(`[^a-z0-9]+`)
	errSetupComplete = errors.New("setup already completed")
	errEmailTaken    = errors.New("email already registered")
)

type authContext struct {
	User         models.User
	Organization models.Organization
	Membership   models.Membership
	Session      models.Session
}

func (s *Server) registerAuth(api *gin.RouterGroup) {
	auth := api.Group("/auth")
	auth.GET("/status", s.authStatus)
	auth.POST("/setup", s.authSetup)
	auth.POST("/register", s.authRegister)
	auth.POST("/login", s.authLogin)
	auth.GET("/me", s.requireSession(), s.authMe)
	auth.POST("/logout", s.requireSession(), s.authLogout)
	auth.GET("/organizations", s.requireSession(), s.authOrganizations)
	auth.POST("/switch-organization", s.requireSession(), s.authSwitchOrganization)
	auth.POST("/change-password", s.requireSession(), s.authChangePassword)
	auth.GET("/platform-settings", s.requireSession(), s.getPlatformSettings)
	auth.PUT("/platform-settings", s.requireSession(), s.putPlatformSettings)
}

func (s *Server) authStatus(c *gin.Context) {
	var count int64
	_ = db.DB.Model(&models.User{}).Count(&count).Error
	settings := loadPlatformSettings()
	response.Success(c, gin.H{
		"enabled":                    s.Cfg.Auth.Enabled,
		"setup_required":             count == 0,
		"registration_enabled":       settings.RegistrationEnabled,
		"require_email_verification": settings.RequireEmailVerification,
	})
}

type setupInput struct {
	OrganizationName string `json:"organization_name"`
	Email            string `json:"email"`
	DisplayName      string `json:"display_name"`
	Password         string `json:"password"`
}

func (s *Server) authSetup(c *gin.Context) {
	if !s.Cfg.Auth.Enabled {
		response.BadRequest(c, "authentication is disabled")
		return
	}
	var body setupInput
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	email, err := normalizeEmail(body.Email)
	if err != nil || strings.TrimSpace(body.OrganizationName) == "" || !validPassword(body.Password) {
		response.BadRequest(c, "valid organization, email and password of 8-72 bytes are required")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		response.ServerError(c, "failed to secure password")
		return
	}
	user, organization, err := createInitialAccount(body, email, string(hash))
	if errors.Is(err, errSetupComplete) {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	if err != nil {
		response.ServerError(c, "failed to initialize account")
		return
	}
	sessionToken, csrfToken, err := s.createSession(user.ID, organization.ID)
	if err != nil {
		response.ServerError(c, "failed to create session")
		return
	}
	s.setSessionCookie(c, sessionToken)
	c.JSON(http.StatusCreated, gin.H{"code": http.StatusCreated, "message": "created", "data": authResponse(user, organization, "owner", csrfToken)})
}

func (s *Server) authRegister(c *gin.Context) {
	if !s.Cfg.Auth.Enabled {
		response.BadRequest(c, "authentication is disabled")
		return
	}
	var userCount int64
	if err := db.DB.Model(&models.User{}).Count(&userCount).Error; err != nil {
		response.ServerError(c, "failed to check setup status")
		return
	}
	if userCount == 0 {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "setup required"})
		return
	}
	settings := loadPlatformSettings()
	if !settings.RegistrationEnabled {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "registration disabled"})
		return
	}
	var body setupInput
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	email, err := normalizeEmail(body.Email)
	if err != nil || strings.TrimSpace(body.OrganizationName) == "" || !validPassword(body.Password) {
		response.BadRequest(c, "valid organization, email and password of 8-72 bytes are required")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		response.ServerError(c, "failed to secure password")
		return
	}
	user, organization, err := createRegisteredAccount(body, email, string(hash), settings.RequireEmailVerification)
	if errors.Is(err, errEmailTaken) {
		response.Conflict(c, errEmailTaken.Error())
		return
	}
	if err != nil {
		response.ServerError(c, "failed to register account")
		return
	}
	if settings.RequireEmailVerification {
		c.JSON(http.StatusCreated, gin.H{
			"code":    http.StatusCreated,
			"message": "created",
			"data": gin.H{
				"verification_required": true,
				"email":                 email,
			},
		})
		return
	}
	sessionToken, csrfToken, err := s.createSession(user.ID, organization.ID)
	if err != nil {
		response.ServerError(c, "failed to create session")
		return
	}
	s.setSessionCookie(c, sessionToken)
	c.JSON(http.StatusCreated, gin.H{"code": http.StatusCreated, "message": "created", "data": authResponse(user, organization, "owner", csrfToken)})
}

func createInitialAccount(body setupInput, email, passwordHash string) (models.User, models.Organization, error) {
	now := response.Now()
	var user models.User
	var organization models.Organization
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.User{}).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return errSetupComplete
		}
		organization = models.Organization{Name: strings.TrimSpace(body.OrganizationName), Slug: uniqueSlug(tx, body.OrganizationName), Status: "active", CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&organization).Error; err != nil {
			return err
		}
		displayName := strings.TrimSpace(body.DisplayName)
		if displayName == "" {
			displayName = email
		}
		verifiedAt := now
		user = models.User{
			Email: email, PasswordHash: passwordHash, DisplayName: displayName, Status: "active",
			IsPlatformAdmin: true, EmailVerifiedAt: &verifiedAt,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		membership := models.Membership{OrganizationID: organization.ID, UserID: user.ID, Role: "owner", CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&membership).Error; err != nil {
			return err
		}
		if err := claimLegacyResources(tx, organization.ID); err != nil {
			return err
		}
		return db.SeedOrganizationDefaults(tx, organization.ID)
	})
	return user, organization, err
}

func createRegisteredAccount(body setupInput, email, passwordHash string, requireEmailVerification bool) (models.User, models.Organization, error) {
	now := response.Now()
	var user models.User
	var organization models.Organization
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&models.User{}).Where("email = ?", email).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return errEmailTaken
		}
		organization = models.Organization{Name: strings.TrimSpace(body.OrganizationName), Slug: uniqueSlug(tx, body.OrganizationName), Status: "active", CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&organization).Error; err != nil {
			return err
		}
		displayName := strings.TrimSpace(body.DisplayName)
		if displayName == "" {
			displayName = email
		}
		user = models.User{
			Email: email, PasswordHash: passwordHash, DisplayName: displayName, Status: "active",
			IsPlatformAdmin: false, CreatedAt: now, UpdatedAt: now,
		}
		if !requireEmailVerification {
			verifiedAt := now
			user.EmailVerifiedAt = &verifiedAt
		}
		if err := tx.Create(&user).Error; err != nil {
			if isUniqueConstraintError(err) {
				return errEmailTaken
			}
			return err
		}
		membership := models.Membership{OrganizationID: organization.ID, UserID: user.ID, Role: "owner", CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&membership).Error; err != nil {
			return err
		}
		return db.SeedOrganizationDefaults(tx, organization.ID)
	})
	return user, organization, err
}

func loadPlatformSettings() models.PlatformSettings {
	var settings models.PlatformSettings
	if err := db.DB.First(&settings, 1).Error; err != nil {
		return models.PlatformSettings{ID: 1, RegistrationEnabled: true, RequireEmailVerification: false}
	}
	return settings
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "duplicate")
}

func claimLegacyResources(tx *gorm.DB, organizationID uint) error {
	resources := []any{
		&models.Drama{}, &models.Episode{}, &models.Character{}, &models.EpisodeCharacter{},
		&models.EpisodeScene{}, &models.Scene{}, &models.Storyboard{}, &models.StoryboardCharacter{},
		&models.AIServiceConfig{}, &models.AIVoice{}, &models.AgentConfig{}, &models.ImageGeneration{},
		&models.VideoGeneration{}, &models.VideoMerge{}, &models.Prop{}, &models.Asset{},
		&models.GridHistory{}, &models.GenerationJob{}, &models.ProductionRun{},
		&models.AuditLog{}, &models.OrganizationQuota{},
	}
	for _, resource := range resources {
		if err := tx.Model(resource).Where("organization_id = ?", 0).Update("organization_id", organizationID).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) authLogin(c *gin.Context) {
	if !s.Cfg.Auth.Enabled {
		response.BadRequest(c, "authentication is disabled")
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	user, organization, membership, err := verifyLogin(body.Email, body.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "invalid credentials"})
		return
	}
	settings := loadPlatformSettings()
	if settings.RequireEmailVerification && user.EmailVerifiedAt == nil {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "email verification required"})
		return
	}
	sessionToken, csrfToken, err := s.createSession(user.ID, organization.ID)
	if err != nil {
		response.ServerError(c, "failed to create session")
		return
	}
	now := response.Now()
	_ = db.DB.Model(&user).Updates(map[string]any{"last_login_at": now, "updated_at": now}).Error
	s.setSessionCookie(c, sessionToken)
	response.Success(c, authResponse(user, organization, membership.Role, csrfToken))
}

func verifyLogin(rawEmail, password string) (models.User, models.Organization, models.Membership, error) {
	email, _ := normalizeEmail(rawEmail)
	var user models.User
	if db.DB.Where("email = ? AND status = ?", email, "active").First(&user).Error != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return user, models.Organization{}, models.Membership{}, errors.New("invalid credentials")
	}
	var membership models.Membership
	if err := db.DB.Where("user_id = ?", user.ID).Order("organization_id").First(&membership).Error; err != nil {
		return user, models.Organization{}, membership, err
	}
	var organization models.Organization
	if err := db.DB.Where("id = ? AND status = ?", membership.OrganizationID, "active").First(&organization).Error; err != nil {
		return user, organization, membership, err
	}
	return user, organization, membership, nil
}

func (s *Server) authMe(c *gin.Context) {
	actor, _ := currentAuth(c)
	response.Success(c, authResponse(actor.User, actor.Organization, actor.Membership.Role, actor.Session.CSRFToken))
}

func (s *Server) authLogout(c *gin.Context) {
	actor, _ := currentAuth(c)
	now := response.Now()
	result := db.DB.Model(&models.Session{}).Where("token_hash = ? AND revoked_at IS NULL", actor.Session.TokenHash).Update("revoked_at", now)
	if result.Error != nil || result.RowsAffected != 1 {
		response.ServerError(c, "failed to revoke session")
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: s.Cfg.Auth.CookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Cfg.Auth.SecureCookies, SameSite: http.SameSiteLaxMode})
	response.Success(c, nil)
}

func (s *Server) createSession(userID, organizationID uint) (string, string, error) {
	return createSessionRecord(db.DB, userID, organizationID, s.Cfg.Auth.SessionTTLHours)
}

func createSessionRecord(database *gorm.DB, userID, organizationID uint, ttlHours int) (string, string, error) {
	token, err := randomToken()
	if err != nil {
		return "", "", err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC()
	session := models.Session{
		TokenHash: tokenHash(token), CSRFToken: csrf, UserID: userID, OrganizationID: organizationID,
		ExpiresAt:  now.Add(time.Duration(ttlHours) * time.Hour).Format(time.RFC3339),
		LastSeenAt: now.Format(time.RFC3339), CreatedAt: now.Format(time.RFC3339),
	}
	if err := database.Create(&session).Error; err != nil {
		return "", "", err
	}
	return token, csrf, nil
}

func (s *Server) setSessionCookie(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: s.Cfg.Auth.CookieName, Value: token, Path: "/", MaxAge: s.Cfg.Auth.SessionTTLHours * 3600,
		HttpOnly: true, Secure: s.Cfg.Auth.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) requireSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.authenticate(c) {
			return
		}
		if isUnsafeMethod(c.Request.Method) && !s.validCSRF(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "invalid csrf token"})
			return
		}
		c.Next()
	}
}

func (s *Server) protectBusinessAPI() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.Cfg.Auth.Enabled {
			c.Next()
			return
		}
		if !s.authenticate(c) {
			return
		}
		actor, _ := currentAuth(c)
		if actor.Membership.Role == "viewer" && isUnsafeMethod(c.Request.Method) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "insufficient role"})
			return
		}
		if isAdminMutation(c.Request.Method, c.Request.URL.Path) && actor.Membership.Role != "owner" && actor.Membership.Role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "admin role required"})
			return
		}
		if isUnsafeMethod(c.Request.Method) && !s.validCSRF(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "invalid csrf token"})
			return
		}
		c.Next()
	}
}

func (s *Server) authenticate(c *gin.Context) bool {
	cookie, err := c.Request.Cookie(s.Cfg.Auth.CookieName)
	if err != nil || cookie.Value == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "authentication required"})
		return false
	}
	var session models.Session
	now := time.Now().UTC().Format(time.RFC3339)
	if err := db.DB.Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", tokenHash(cookie.Value), now).First(&session).Error; err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "authentication required"})
		return false
	}
	actor, err := loadActor(session)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "organization access denied"})
		return false
	}
	c.Set(authContextKey, actor)
	return true
}

func loadActor(session models.Session) (authContext, error) {
	var user models.User
	if err := db.DB.Where("id = ? AND status = ?", session.UserID, "active").First(&user).Error; err != nil {
		return authContext{}, err
	}
	var organization models.Organization
	if err := db.DB.Where("id = ? AND status = ?", session.OrganizationID, "active").First(&organization).Error; err != nil {
		return authContext{}, err
	}
	var membership models.Membership
	if err := db.DB.Where("organization_id = ? AND user_id = ?", organization.ID, user.ID).First(&membership).Error; err != nil || !validRole(membership.Role) {
		return authContext{}, errors.New("invalid membership")
	}
	return authContext{User: user, Organization: organization, Membership: membership, Session: session}, nil
}

func (s *Server) validCSRF(c *gin.Context) bool {
	actor, ok := currentAuth(c)
	if !ok {
		return false
	}
	provided := strings.TrimSpace(c.GetHeader("X-CSRF-Token"))
	if provided == "" || actor.Session.CSRFToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(actor.Session.CSRFToken)) == 1
}

func currentAuth(c *gin.Context) (authContext, bool) {
	value, exists := c.Get(authContextKey)
	actor, ok := value.(authContext)
	return actor, exists && ok
}

func organizationDB(c *gin.Context) *gorm.DB {
	actor, ok := currentAuth(c)
	if !ok {
		return db.DB
	}
	return db.DB.Where("organization_id = ?", actor.Organization.ID)
}

func currentOrganizationID(c *gin.Context) uint {
	actor, ok := currentAuth(c)
	if !ok {
		return 0
	}
	return actor.Organization.ID
}

func authResponse(user models.User, organization models.Organization, role, csrf string) gin.H {
	data := gin.H{
		"user":         gin.H{"id": user.ID, "email": user.Email, "display_name": user.DisplayName, "is_platform_admin": user.IsPlatformAdmin},
		"organization": gin.H{"id": organization.ID, "name": organization.Name, "slug": organization.Slug},
		"role":         role,
	}
	if csrf != "" {
		data["csrf_token"] = csrf
	}
	return data
}

func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 254 {
		return "", errors.New("invalid email")
	}
	return email, nil
}

func validPassword(password string) bool {
	return len(password) >= minPasswordBytes && len(password) <= maxPasswordBytes
}

func validRole(role string) bool {
	switch role {
	case "owner", "admin", "editor", "viewer":
		return true
	default:
		return false
	}
}

func isUnsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func isAdminMutation(method, path string) bool {
	if !isUnsafeMethod(method) {
		return false
	}
	if strings.HasPrefix(path, "/api/v1/prompt-templates/") && strings.HasSuffix(path, "/preview") {
		return false
	}
	return strings.HasPrefix(path, "/api/v1/ai-configs") || strings.HasPrefix(path, "/api/v1/service-bundles") || strings.HasPrefix(path, "/api/v1/ai-service-bundles") || strings.HasPrefix(path, "/api/v1/agent-configs") || strings.HasPrefix(path, "/api/v1/prompt-templates") || path == "/api/v1/ai-voices/sync" || strings.HasPrefix(path, "/api/v1/organization/")
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func uniqueSlug(tx *gorm.DB, name string) string {
	base := strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if base == "" {
		base = "studio"
	}
	slug := base
	for suffix := 2; ; suffix++ {
		var count int64
		tx.Model(&models.Organization{}).Where("slug = ?", slug).Count(&count)
		if count == 0 {
			return slug
		}
		slug = base + "-" + strconv.Itoa(suffix)
	}
}
