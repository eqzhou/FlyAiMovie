package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func respondGenerationError(c *gin.Context, err error) {
	if errors.Is(err, jobs.ErrQuotaExceeded) {
		c.JSON(http.StatusTooManyRequests, gin.H{"code": http.StatusTooManyRequests, "message": err.Error()})
		return
	}
	response.BadRequest(c, err.Error())
}

func (s *Server) registerOrganizationQuota(api *gin.RouterGroup) {
	group := api.Group("/organization")
	group.GET("/quota", s.getOrganizationQuota)
	group.PUT("/quota", s.updateOrganizationQuota)
}

func (s *Server) getOrganizationQuota(c *gin.Context) {
	organizationID := currentOrganizationID(c)
	quota := models.OrganizationQuota{OrganizationID: organizationID, DailyJobLimit: 200, MaxActiveJobs: 10}
	err := organizationDB(c).Where("organization_id = ?", organizationID).First(&quota).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		response.ServerError(c, "failed to load quota")
		return
	}
	startOfDay := time.Now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339)
	var daily, active int64
	organizationDB(c).Model(&models.GenerationJob{}).Where("created_at >= ?", startOfDay).Count(&daily)
	organizationDB(c).Model(&models.GenerationJob{}).Where("status NOT IN ?", []string{jobs.StatusSucceeded, jobs.StatusFailed, jobs.StatusCanceled}).Count(&active)
	response.Success(c, gin.H{"daily_job_limit": quota.DailyJobLimit, "max_active_jobs": quota.MaxActiveJobs, "daily_jobs_used": daily, "active_jobs": active})
}

func (s *Server) updateOrganizationQuota(c *gin.Context) {
	var body struct {
		DailyJobLimit int `json:"daily_job_limit"`
		MaxActiveJobs int `json:"max_active_jobs"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.DailyJobLimit < 1 || body.DailyJobLimit > 100000 || body.MaxActiveJobs < 1 || body.MaxActiveJobs > 1000 {
		response.BadRequest(c, "daily_job_limit or max_active_jobs is invalid")
		return
	}
	now := response.Now()
	quota := models.OrganizationQuota{OrganizationID: currentOrganizationID(c), DailyJobLimit: body.DailyJobLimit, MaxActiveJobs: body.MaxActiveJobs, CreatedAt: now, UpdatedAt: now}
	if err := organizationDB(c).Where("organization_id = ?", quota.OrganizationID).Assign(map[string]any{"daily_job_limit": body.DailyJobLimit, "max_active_jobs": body.MaxActiveJobs, "updated_at": now}).FirstOrCreate(&quota).Error; err != nil {
		response.ServerError(c, "failed to update quota")
		return
	}
	response.Success(c, quota)
}
