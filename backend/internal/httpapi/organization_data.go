package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/mediacleanup"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Server) registerOrganizationData(api *gin.RouterGroup) {
	api.GET("/organization/export", s.exportOrganizationData)
	api.DELETE("/organization", s.deleteOrganizationData)
}

func requireOrganizationOwner(c *gin.Context) (authContext, bool) {
	actor, ok := currentAuth(c)
	if !ok || actor.Membership.Role != "owner" {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "owner role required"})
		return actor, false
	}
	return actor, true
}

func (s *Server) exportOrganizationData(c *gin.Context) {
	actor, ok := requireOrganizationOwner(c)
	if !ok {
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Content-Disposition", `attachment; filename="flyaimovie-organization-export.json"`)
	organizationID := actor.Organization.ID
	export := gin.H{"exported_at": response.Now(), "organization": actor.Organization}
	if err := loadExportRows(export, organizationID); err != nil {
		response.ServerError(c, "failed to export organization data")
		return
	}
	var bundles []models.AIServiceBundle
	if err := db.DB.Where("organization_id = ?", organizationID).Order("id").Find(&bundles).Error; err != nil {
		response.ServerError(c, "failed to export service bundles")
		return
	}
	safeBundles := make([]gin.H, 0, len(bundles))
	for _, bundle := range bundles {
		var services []models.AIServiceBundleTemplateItem
		if err := json.Unmarshal([]byte(bundle.ServicesJSON), &services); err != nil {
			response.ServerError(c, "failed to export service bundles")
			return
		}
		safeBundles = append(safeBundles, gin.H{
			"id": bundle.ID, "key": bundle.Key, "name": bundle.Name, "description": bundle.Description,
			"services": services, "is_builtin": bundle.IsBuiltin, "is_active": bundle.IsActive,
			"created_at": bundle.CreatedAt, "updated_at": bundle.UpdatedAt,
		})
	}
	export["service_bundles"] = safeBundles
	var configs []models.AIServiceConfig
	if err := db.DB.Where("organization_id = ?", organizationID).Order("id").Find(&configs).Error; err != nil {
		response.ServerError(c, "failed to export AI configs")
		return
	}
	safeConfigs := make([]gin.H, 0, len(configs))
	for _, config := range configs {
		safeConfigs = append(safeConfigs, aiConfigResponse(config))
	}
	export["ai_configs"] = safeConfigs
	var memberships []models.Membership
	if err := db.DB.Where("organization_id = ?", organizationID).Find(&memberships).Error; err != nil {
		response.ServerError(c, "failed to export members")
		return
	}
	members := make([]gin.H, 0, len(memberships))
	for _, membership := range memberships {
		var user models.User
		if err := db.DB.First(&user, membership.UserID).Error; err != nil {
			response.ServerError(c, "failed to export member")
			return
		}
		members = append(members, gin.H{"user_id": user.ID, "email": user.Email, "display_name": user.DisplayName, "status": user.Status, "role": membership.Role})
	}
	export["members"] = members
	response.Success(c, export)
}

func loadExportRows(out gin.H, organizationID uint) error {
	queries := []struct {
		key    string
		target any
	}{
		{"dramas", &[]models.Drama{}}, {"episodes", &[]models.Episode{}}, {"characters", &[]models.Character{}},
		{"character_templates", &[]models.CharacterTemplate{}},
		{"episode_characters", &[]models.EpisodeCharacter{}}, {"scenes", &[]models.Scene{}}, {"episode_scenes", &[]models.EpisodeScene{}},
		{"storyboards", &[]models.Storyboard{}}, {"storyboard_characters", &[]models.StoryboardCharacter{}}, {"props", &[]models.Prop{}},
		{"assets", &[]models.Asset{}}, {"grid_history", &[]models.GridHistory{}}, {"image_generations", &[]models.ImageGeneration{}},
		{"video_generations", &[]models.VideoGeneration{}}, {"video_merges", &[]models.VideoMerge{}}, {"generation_jobs", &[]models.GenerationJob{}}, {"production_runs", &[]models.ProductionRun{}},
		{"job_events", &[]models.JobEvent{}}, {"agent_runs", &[]models.AgentRun{}}, {"agent_run_events", &[]models.AgentRunEvent{}},
		{"media_migrations", &[]models.MediaMigration{}},
		{"media_deletion_tasks", &[]models.MediaDeletionTask{}},
		{"media_cache_objects", &[]models.MediaCacheObject{}}, {"media_cache_references", &[]models.MediaCacheReference{}},
		{"agent_configs", &[]models.AgentConfig{}}, {"voices", &[]models.AIVoice{}}, {"audit_logs", &[]models.AuditLog{}}, {"quota", &[]models.OrganizationQuota{}},
		{"prompt_templates", &[]models.PromptTemplate{}}, {"prompt_template_revisions", &[]models.PromptTemplateRevision{}},
		{"skills", &[]models.Skill{}}, {"skill_versions", &[]models.SkillVersion{}}, {"skill_publications", &[]models.SkillPublication{}},
		{"invitations", &[]models.OrganizationInvitation{}},
	}
	for _, query := range queries {
		if err := db.DB.Where("organization_id = ?", organizationID).Find(query.target).Error; err != nil {
			return err
		}
		out[query.key] = query.target
	}
	return nil
}

func (s *Server) deleteOrganizationData(c *gin.Context) {
	actor, ok := requireOrganizationOwner(c)
	if !ok {
		return
	}
	var body struct {
		Password     string `json:"password"`
		Confirmation string `json:"confirmation"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(actor.User.PasswordHash), []byte(body.Password)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "invalid password"})
		return
	}
	if strings.TrimSpace(body.Confirmation) != actor.Organization.Slug {
		response.BadRequest(c, "confirmation must match organization slug")
		return
	}
	paths, err := s.organizationMediaPaths(actor.Organization.ID)
	if err != nil {
		response.ServerError(c, "failed to prepare media deletion")
		return
	}
	var userIDs []uint
	if err := db.DB.Model(&models.Membership{}).Where("organization_id = ?", actor.Organization.ID).Pluck("user_id", &userIDs).Error; err != nil {
		response.ServerError(c, "failed to prepare deletion")
		return
	}
	pathList := make([]string, 0, len(paths))
	for path := range paths {
		pathList = append(pathList, path)
	}
	if err := purgeOrganization(db.DB, s.Store, actor.Organization.ID, userIDs, pathList); err != nil {
		log.Printf("organization deletion failed organization_id=%d: %v", actor.Organization.ID, err)
		response.ServerError(c, "failed to delete organization")
		return
	}
	cleanup, cleanupErr := mediacleanup.New(db.DB, s.Store).ProcessOrganization(actor.Organization.ID, 1000)
	if cleanupErr != nil {
		cleanup.Failed++
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: s.Cfg.Auth.CookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Cfg.Auth.SecureCookies, SameSite: http.SameSiteLaxMode})
	response.Success(c, gin.H{"deleted": true, "deleted_files": cleanup.Completed, "cleanup_pending": cleanup.Failed})
}

func purgeOrganization(database *gorm.DB, store *storage.LocalStorage, organizationID uint, userIDs []uint, paths []string) error {
	return database.Transaction(func(tx *gorm.DB) error {
		var organization models.Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", organizationID).First(&organization).Error; err != nil {
			return err
		}
		if err := mediacleanup.New(tx, store).Queue(organizationID, paths); err != nil {
			return err
		}
		resources := []any{
			&models.AgentRunEvent{}, &models.AgentRun{}, &models.JobEvent{}, &models.MediaMigration{}, &models.MediaCacheReference{}, &models.MediaCacheObject{},
			&models.SkillPublication{}, &models.SkillVersion{}, &models.Skill{}, &models.AIServiceBundle{},
			&models.PromptTemplateRevision{}, &models.PromptTemplate{},
			&models.StoryboardCharacter{}, &models.EpisodeCharacter{}, &models.EpisodeScene{}, &models.Asset{}, &models.GridHistory{},
			&models.ImageGeneration{}, &models.VideoGeneration{}, &models.VideoMerge{}, &models.GenerationJob{}, &models.ProductionRun{}, &models.Storyboard{},
			&models.CharacterTemplate{}, &models.Character{}, &models.Scene{}, &models.Prop{}, &models.Episode{}, &models.Drama{}, &models.AIServiceConfig{},
			&models.AIVoice{}, &models.AgentConfig{}, &models.AuditLog{}, &models.OrganizationQuota{}, &models.Session{}, &models.Membership{},
			&models.OrganizationInvitation{},
		}
		for _, resource := range resources {
			if err := tx.Where("organization_id = ?", organizationID).Delete(resource).Error; err != nil {
				return err
			}
		}
		if len(userIDs) > 0 {
			if err := tx.Where("user_id IN ?", userIDs).Delete(&models.PasswordResetToken{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("id = ?", organizationID).Delete(&models.Organization{}).Error; err != nil {
			return err
		}
		for _, userID := range userIDs {
			var remaining int64
			if err := tx.Model(&models.Membership{}).Where("user_id = ?", userID).Count(&remaining).Error; err != nil {
				return err
			}
			if remaining == 0 {
				if err := tx.Where("id = ?", userID).Delete(&models.User{}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *Server) organizationMediaPaths(organizationID uint) (map[string]struct{}, error) {
	values := make([]string, 0)
	var assets []models.Asset
	if err := db.DB.Where("organization_id = ?", organizationID).Find(&assets).Error; err != nil {
		return nil, err
	}
	for _, row := range assets {
		values = append(values, row.LocalPath, row.URL, row.ThumbnailURL)
	}
	var images []models.ImageGeneration
	if err := db.DB.Where("organization_id = ?", organizationID).Find(&images).Error; err != nil {
		return nil, err
	}
	for _, row := range images {
		values = append(values, row.LocalPath, row.ImageURL)
	}
	var videos []models.VideoGeneration
	if err := db.DB.Where("organization_id = ?", organizationID).Find(&videos).Error; err != nil {
		return nil, err
	}
	for _, row := range videos {
		values = append(values, row.LocalPath, row.VideoURL)
	}
	var storyboards []models.Storyboard
	if err := db.DB.Where("organization_id = ?", organizationID).Find(&storyboards).Error; err != nil {
		return nil, err
	}
	for _, row := range storyboards {
		values = append(values, row.FirstFrameImage, row.LastFrameImage, row.ComposedImage, row.VideoURL, row.TTSAudioURL, row.SubtitleURL, row.ComposedVideoURL)
	}
	var cacheObjects []models.MediaCacheObject
	if err := db.DB.Where("organization_id = ?", organizationID).Find(&cacheObjects).Error; err != nil {
		return nil, err
	}
	for _, row := range cacheObjects {
		values = append(values, row.LocalPath, row.PublicURL)
	}
	var episodes []models.Episode
	if err := db.DB.Where("organization_id = ?", organizationID).Find(&episodes).Error; err != nil {
		return nil, err
	}
	for _, row := range episodes {
		values = append(values, row.VideoURL, row.Thumbnail)
	}
	paths := make(map[string]struct{})
	for _, value := range values {
		if value != "" {
			if path, err := s.Store.Resolve(value); err == nil {
				paths[path] = struct{}{}
			}
		}
	}
	return paths, nil
}
