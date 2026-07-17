package httpapi

import (
	"context"
	"fmt"
	"log"
	"net/http"
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
	"github.com/eqzhou/flyaimovie/internal/services/mediacleanup"
	"github.com/eqzhou/flyaimovie/internal/services/mediaref"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"github.com/gin-gonic/gin"
)

type Server struct {
	Cfg          *config.Config
	Store        *storage.LocalStorage
	Agents       *agents.Runner
	Images       *generation.ImageService
	Videos       *generation.VideoService
	TTS          *generation.TTSService
	Jobs         *jobs.Service
	Frontend     string // dist path optional
	ResetSender  PasswordResetSender
	agentRunMu   sync.Mutex
	agentCancels map[uint]context.CancelFunc
}

func NewServer(cfg *config.Config, store *storage.LocalStorage, skillsDir, frontendDist string) *Server {
	jobService := jobs.New(db.DB)
	references := &mediaref.Resolver{Store: store}
	server := &Server{
		Cfg:          cfg,
		Store:        store,
		Agents:       agents.NewRunner(skillsDir),
		Images:       &generation.ImageService{Store: store, Jobs: jobService, References: references},
		Videos:       &generation.VideoService{Store: store, Jobs: jobService, References: references},
		TTS:          &generation.TTSService{Store: store},
		Jobs:         jobService,
		Frontend:     frontendDist,
		ResetSender:  NoopPasswordResetSender{},
		agentCancels: make(map[uint]context.CancelFunc),
	}
	timestamp := response.Now()
	if err := db.DB.Model(&models.AgentRun{}).Where("status = ?", "running").Updates(map[string]any{
		"status": "failed", "last_error": "server restarted during agent execution", "completed_at": timestamp, "updated_at": timestamp,
	}).Error; err != nil {
		log.Printf("recover interrupted agent runs: %v", err)
	}
	if cleanup, err := mediacleanup.New(db.DB, store).ProcessOrganization(0, 100); err != nil {
		log.Printf("retry media cleanup tasks: %v", err)
	} else if cleanup.Failed > 0 {
		log.Printf("media cleanup retry left %d failed tasks", cleanup.Failed)
	}
	if cfg != nil && cfg.Email.SMTPHost != "" && cfg.Email.SMTPPort > 0 && cfg.Email.SMTPUsername != "" && cfg.Email.SMTPPassword != "" && cfg.Email.From != "" && cfg.Email.ResetURLBase != "" {
		server.ResetSender = NewSMTPPasswordResetSender(cfg.Email)
	}
	return server
}

func (s *Server) Router() *gin.Engine {
	if !s.Cfg.App.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger(), s.securityHeaders())
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
		s.registerAgentConfigs(api)
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
		s.registerAuditLogs(api)
		s.registerOrganizationQuota(api)
		s.registerOrganizationMembers(api)
		s.registerOrganizationData(api)
	}

	if s.Cfg.Auth.Enabled {
		r.GET("/static/*filepath", s.requireSession(), s.servePrivateStatic)
	} else {
		r.Static("/static", s.Store.Root)
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

func (s *Server) servePrivateStatic(c *gin.Context) {
	relativePath := strings.TrimPrefix(c.Param("filepath"), "/")
	publicURL := "/static/" + relativePath
	if !mediaOwnedByOrganization(c, publicURL, relativePath) {
		response.NotFound(c, "media not found")
		return
	}
	path, err := s.Store.Resolve(publicURL)
	if err != nil {
		response.NotFound(c, "media not found")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.File(path)
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
	for _, item := range strings.Split(value, ",") {
		if err := validateLocalMediaOwnership(c, item); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Next()
	}
}

func (s *Server) cors() gin.HandlerFunc {
	origins := map[string]bool{}
	for _, o := range s.Cfg.Server.CORSOrigins {
		origins[o] = true
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			c.Header("Vary", "Origin")
		}
		if origin != "" && origins[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
		} else if origin != "" && origins["*"] {
			c.Header("Access-Control-Allow-Origin", "*")
		}
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
