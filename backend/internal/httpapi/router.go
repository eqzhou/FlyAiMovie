package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/agents"
	"github.com/eqzhou/flyaimovie/internal/services/generation"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	"github.com/eqzhou/flyaimovie/internal/services/mediacleanup"
	"github.com/eqzhou/flyaimovie/internal/services/mediaref"
	"github.com/eqzhou/flyaimovie/internal/services/production"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"github.com/gin-gonic/gin"
)

const authRequestBodyLimit int64 = 64 << 10

type Server struct {
	Cfg           *config.Config
	Store         *storage.LocalStorage
	Agents        *agents.Runner
	Images        *generation.ImageService
	Videos        *generation.VideoService
	TTS           *generation.TTSService
	Jobs          *jobs.Service
	Productions   *production.Service
	Cache         *mediacache.Service
	Frontend      string // dist path optional
	ResetSender   PasswordResetSender
	InviteSender  InvitationSender
	agentRunMu    sync.Mutex
	agentRetryMu  sync.Mutex
	configWriteMu sync.Mutex
	agentCancels  map[uint]context.CancelFunc
}

func NewServer(cfg *config.Config, store *storage.LocalStorage, skillsDir, frontendDist string) *Server {
	jobService := jobs.New(db.DB)
	cacheService := mediacache.New(db.DB, store)
	references := &mediaref.Resolver{Store: store}
	server := &Server{
		Cfg:          cfg,
		Store:        store,
		Agents:       agents.NewRunner(skillsDir),
		Images:       &generation.ImageService{Store: store, Jobs: jobService, References: references, Cache: cacheService},
		Videos:       &generation.VideoService{Store: store, Jobs: jobService, References: references, Cache: cacheService},
		TTS:          &generation.TTSService{Store: store, Cache: cacheService},
		Jobs:         jobService,
		Cache:        cacheService,
		Frontend:     frontendDist,
		ResetSender:  NoopPasswordResetSender{},
		InviteSender: NoopInvitationSender{},
		agentCancels: make(map[uint]context.CancelFunc),
	}
	server.Productions = production.New(db.DB, server.Agents, server.Images, server.Videos, jobService)
	timestamp := response.Now()
	if err := db.DB.Model(&models.AgentRun{}).Where("status = ?", "running").Updates(map[string]any{
		"status": "failed", "last_error": "server restarted during agent execution", "completed_at": timestamp, "updated_at": timestamp,
	}).Error; err != nil {
		log.Printf("recover interrupted agent runs: %v", err)
	}
	if _, err := cacheService.PurgeAllExpired(100); err != nil {
		log.Printf("purge expired cache: %v", err)
	}
	if cleanup, err := mediacleanup.New(db.DB, store).ProcessOrganization(0, 100); err != nil {
		log.Printf("retry media cleanup tasks: %v", err)
	} else if cleanup.Failed > 0 {
		log.Printf("media cleanup retry left %d failed tasks", cleanup.Failed)
	}
	if cfg != nil && cfg.Email.SMTPHost != "" && cfg.Email.SMTPPort > 0 && cfg.Email.SMTPUsername != "" && cfg.Email.SMTPPassword != "" && cfg.Email.From != "" && cfg.Email.ResetURLBase != "" {
		mailer := NewSMTPPasswordResetSender(cfg.Email)
		server.ResetSender = mailer
		server.InviteSender = mailer
	}
	return server
}

func (s *Server) Router() *gin.Engine {
	if !s.Cfg.App.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery(), routeTemplateLogger(), s.securityHeaders(), s.authSecurity())
	r.Use(s.rateLimit(newIPRateLimiter(s.Cfg.Server.RateLimitPerMinute, time.Minute, time.Now)), s.cors())

	r.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "timestamp": time.Now().UTC().Format(time.RFC3339)})
	})

	publicAPI := r.Group("/api/v1")
	s.registerAuth(publicAPI)
	s.registerPasswordReset(publicAPI)
	s.registerPublicInvitations(publicAPI)
	s.registerWebhooks(publicAPI)

	api := r.Group("/api/v1", s.protectBusinessAPI(), s.auditMutations())
	{
		s.registerDramas(api)
		s.registerEpisodes(api)
		s.registerCharacters(api)
		s.registerCharacterLibrary(api)
		s.registerScenes(api)
		s.registerStoryboards(api)
		s.registerImages(api)
		s.registerVideos(api)
		s.registerUpload(api)
		s.registerAIConfigs(api)
		s.registerServiceBundles(api)
		s.registerAgentConfigs(api)
		s.registerPromptTemplates(api)
		s.registerAgent(api)
		s.registerCompose(api)
		s.registerMerge(api)
		s.registerGrid(api)
		s.registerSkills(api)
		s.registerVoices(api)
		s.registerProps(api)
		s.registerAssets(api)
		s.registerPipelineExtras(api)
		s.registerJobs(api)
		s.registerProductions(api)
		s.registerAuditLogs(api)
		s.registerOrganizationQuota(api)
		s.registerMediaCache(api)
		s.registerOrganizationMembers(api)
		s.registerOrganizationData(api)
	}

	if s.Cfg.Auth.Enabled {
		r.GET("/static/*filepath", s.requireSession(), s.servePrivateStatic)
	} else {
		r.GET("/static/*filepath", s.servePublicStatic)
	}

	// frontend SPA
	if s.Frontend != "" {
		r.Static("/assets", filepath.Join(s.Frontend, "assets"))
		r.NoRoute(func(c *gin.Context) {
			// API 404
			if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/api" {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "not found"})
				return
			}
			index := filepath.Join(s.Frontend, "index.html")
			c.File(index)
		})
	}
	return r
}

func (s *Server) servePublicStatic(c *gin.Context) {
	relativePath := strings.TrimPrefix(c.Param("filepath"), "/")
	s.serveStaticFile(c, relativePath)
}

func (s *Server) serveStaticFile(c *gin.Context, relativePath string) {
	root, err := os.OpenRoot(s.Store.Root)
	if err != nil {
		response.NotFound(c, "media not found")
		return
	}
	defer root.Close()
	file, err := root.Open(relativePath)
	if err != nil {
		response.NotFound(c, "media not found")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		response.NotFound(c, "media not found")
		return
	}
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
}

func routeTemplateLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "<unmatched>"
		}
		fmt.Fprintf(gin.DefaultWriter, "[GIN] %s | %3d | %13v | %15s | %-7s %q\n",
			time.Now().Format("2006/01/02 - 15:04:05"),
			c.Writer.Status(),
			time.Since(startedAt),
			c.ClientIP(),
			c.Request.Method,
			route,
		)
	}
}

func (s *Server) servePrivateStatic(c *gin.Context) {
	relativePath := strings.TrimPrefix(c.Param("filepath"), "/")
	publicURL := "/static/" + relativePath
	if !mediaOwnedByOrganization(c, publicURL, relativePath) {
		response.NotFound(c, "media not found")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	s.serveStaticFile(c, relativePath)
}

func mediaOwnedByOrganization(c *gin.Context, publicURL, relativePath string) bool {
	checks := []struct {
		model any
		query string
		args  []any
	}{
		{&models.Asset{}, "url = ? OR local_path = ? OR thumbnail_url = ?", []any{publicURL, relativePath, publicURL}},
		{&models.ImageGeneration{}, "image_url = ? OR local_path = ?", []any{publicURL, relativePath}},
		{&models.VideoGeneration{}, "video_url = ? OR local_path = ?", []any{publicURL, relativePath}},
		{&models.Drama{}, "thumbnail = ?", []any{publicURL}},
		{&models.Episode{}, "video_url = ? OR thumbnail = ?", []any{publicURL, publicURL}},
		{&models.Character{}, "image_url = ? OR local_path = ? OR voice_sample_url = ?", []any{publicURL, relativePath, publicURL}},
		{&models.CharacterTemplate{}, "image_url = ? OR local_path = ?", []any{publicURL, relativePath}},
		{&models.Scene{}, "image_url = ? OR local_path = ?", []any{publicURL, relativePath}},
		{&models.Prop{}, "image_url = ? OR local_path = ?", []any{publicURL, relativePath}},
		{&models.Storyboard{}, "composed_image = ? OR first_frame_image = ? OR last_frame_image = ? OR video_url = ? OR tts_audio_url = ? OR subtitle_url = ? OR composed_video_url = ?", []any{publicURL, publicURL, publicURL, publicURL, publicURL, publicURL, publicURL}},
		{&models.GridHistory{}, "image_url = ?", []any{publicURL}},
	}
	for _, check := range checks {
		var count int64
		if organizationDB(c).Model(check.model).Where(check.query, check.args...).Limit(1).Count(&count).Error == nil && count > 0 {
			return true
		}
	}
	return false
}

func validateLocalMediaOwnership(c *gin.Context, values ...string) error {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "data:") || strings.HasPrefix(value, "mock://") {
			continue
		}
		relativePath := strings.TrimPrefix(value, "/")
		relativePath = strings.TrimPrefix(relativePath, "static/")
		publicURL := "/static/" + relativePath
		if !mediaOwnedByOrganization(c, publicURL, relativePath) {
			return fmt.Errorf("local media is not owned by the current organization")
		}
	}
	return nil
}

func validateReferenceMediaOwnership(c *gin.Context, value string) error {
	items, err := parseReferenceMediaValues(value)
	if err != nil {
		return err
	}
	if len(items) > 8 {
		return fmt.Errorf("at most 8 reference images are allowed")
	}
	for _, item := range items {
		if err := validateLocalMediaOwnership(c, item); err != nil {
			return err
		}
	}
	return nil
}

func parseReferenceMediaValues(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}, nil
	}
	if strings.HasPrefix(value, "data:") {
		return []string{value}, nil
	}
	items := []string(nil)
	if strings.HasPrefix(value, "[") {
		if err := json.Unmarshal([]byte(value), &items); err != nil {
			return nil, fmt.Errorf("invalid reference images: %w", err)
		}
	} else {
		items = strings.Split(value, ",")
	}
	clean := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			clean = append(clean, item)
		}
	}
	return clean, nil
}

func (s *Server) securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Cross-Origin-Opener-Policy", "same-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.Cfg.Auth.SecureCookies {
			c.Header("Strict-Transport-Security", "max-age=31536000")
		}
		c.Next()
	}
}

func (s *Server) authSecurity() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isAuthPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("Pragma", "no-cache")
		if !isUnsafeMethod(c.Request.Method) || c.Request.Body == nil {
			c.Next()
			return
		}
		if c.Request.ContentLength > authRequestBodyLimit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"code": http.StatusRequestEntityTooLarge, "message": "request body too large"})
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, authRequestBodyLimit+1))
		_ = c.Request.Body.Close()
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid body"})
			return
		}
		if int64(len(body)) > authRequestBodyLimit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"code": http.StatusRequestEntityTooLarge, "message": "request body too large"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Request.ContentLength = int64(len(body))
		c.Next()
	}
}

func isAuthPath(path string) bool {
	return path == "/api/v1/auth" || strings.HasPrefix(path, "/api/v1/auth/")
}

func (s *Server) cors() gin.HandlerFunc {
	origins := map[string]bool{}
	for _, o := range s.Cfg.Server.CORSOrigins {
		if o = strings.TrimSpace(o); o != "" {
			origins[o] = true
		}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			c.Header("Vary", "Origin")
		}
		allowed := origin != "" && (origins[origin] || sameRequestOrigin(c.Request, origin))
		wildcardAllowed := origin != "" && origins["*"] && !s.Cfg.Auth.Enabled
		if allowed {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
		} else if wildcardAllowed {
			c.Header("Access-Control-Allow-Origin", "*")
		}
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		if origin != "" && !allowed && !wildcardAllowed && (isUnsafeMethod(c.Request.Method) || c.Request.Method == http.MethodOptions) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "origin not allowed"})
			return
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func sameRequestOrigin(request *http.Request, origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return false
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	} else if forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return strings.EqualFold(parsed.Scheme, scheme) && strings.EqualFold(parsed.Host, request.Host)
}
