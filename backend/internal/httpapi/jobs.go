package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/gin-gonic/gin"
)

func (s *Server) registerJobs(api *gin.RouterGroup) {
	group := api.Group("/jobs")
	group.GET("", s.listJobs)
	group.POST("/batch-cancel", s.batchCancelJobs)
	group.GET("/:id", s.getJob)
	group.GET("/:id/events", s.getJobEvents)
	group.POST("/:id/cancel", s.cancelJob)
	group.POST("/:id/retry", s.retryJob)
}

func (s *Server) getJobEvents(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid job id")
		return
	}
	events, err := s.Jobs.EventsOrganization(currentOrganizationID(c), uint(id))
	if errors.Is(err, jobs.ErrJobNotFound) {
		response.NotFound(c, "job not found")
		return
	}
	if err != nil {
		response.ServerError(c, "failed to load job events")
		return
	}
	response.Success(c, events)
}

func (s *Server) batchCancelJobs(c *gin.Context) {
	var body struct {
		JobIDs []uint `json:"job_ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.JobIDs) == 0 {
		response.BadRequest(c, "job_ids is required")
		return
	}
	canceled, failures, err := s.Jobs.BatchCancelOrganization(currentOrganizationID(c), body.JobIDs)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"canceled": canceled, "failures": failures})
}

func (s *Server) retryJob(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid job id")
		return
	}
	job, err := s.Jobs.GetOrganization(currentOrganizationID(c), uint(id))
	if err != nil {
		if errors.Is(err, jobs.ErrJobNotFound) {
			response.NotFound(c, "job not found")
			return
		}
		response.ServerError(c, "failed to load job")
		return
	}
	if err := s.Jobs.Retry(uint(id)); err != nil {
		if errors.Is(err, jobs.ErrJobNotFound) {
			response.NotFound(c, "job not found")
			return
		}
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	// Re-submit the original target through the normal adapter path. The
	// existing running job is reused by CreateForTarget, so no duplicate job
	// record is created.
	switch job.TargetType {
	case "image_generation":
		var rec models.ImageGeneration
		if err := organizationDB(c).First(&rec, job.TargetID).Error; err != nil {
			response.NotFound(c, "image generation not found")
			return
		}
		if err := s.Images.Generate(c.Request.Context(), &rec, job.ConfigID); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	case "video_generation":
		var rec models.VideoGeneration
		if err := organizationDB(c).First(&rec, job.TargetID).Error; err != nil {
			response.NotFound(c, "video generation not found")
			return
		}
		if err := s.Videos.Generate(c.Request.Context(), &rec, job.ConfigID); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	job, _ = s.Jobs.GetOrganization(currentOrganizationID(c), uint(id))
	response.Success(c, job)
}

func (s *Server) listJobs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	rows, err := s.Jobs.ListOrganization(currentOrganizationID(c), c.Query("status"), c.Query("kind"), limit)
	if err != nil {
		response.ServerError(c, "failed to list jobs")
		return
	}
	response.Success(c, rows)
}

func (s *Server) getJob(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid job id")
		return
	}
	job, err := s.Jobs.GetOrganization(currentOrganizationID(c), uint(id))
	if errors.Is(err, jobs.ErrJobNotFound) {
		response.NotFound(c, "job not found")
		return
	}
	if err != nil {
		response.ServerError(c, "failed to load job")
		return
	}
	response.Success(c, job)
}

func (s *Server) cancelJob(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid job id")
		return
	}
	if _, err := s.Jobs.GetOrganization(currentOrganizationID(c), uint(id)); err != nil {
		if errors.Is(err, jobs.ErrJobNotFound) {
			response.NotFound(c, "job not found")
			return
		}
		response.ServerError(c, "failed to load job")
		return
	}
	err = s.Jobs.Cancel(uint(id))
	if errors.Is(err, jobs.ErrJobNotFound) {
		response.NotFound(c, "job not found")
		return
	}
	if errors.Is(err, jobs.ErrTerminalJob) {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "job is already finished"})
		return
	}
	if err != nil {
		response.ServerError(c, "failed to cancel job")
		return
	}
	job, _ := s.Jobs.GetOrganization(currentOrganizationID(c), uint(id))
	response.Success(c, job)
}
