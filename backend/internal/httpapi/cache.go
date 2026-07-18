package httpapi

import (
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/mediacleanup"
	"github.com/gin-gonic/gin"
)

func (s *Server) registerMediaCache(api *gin.RouterGroup) {
	group := api.Group("/organization/cache")
	group.GET("", s.getMediaCacheStats)
	group.POST("/purge", s.purgeMediaCache)
}

func (s *Server) getMediaCacheStats(c *gin.Context) {
	stats, err := s.Cache.Stats(currentOrganizationID(c))
	if err != nil {
		response.ServerError(c, "failed to load cache statistics")
		return
	}
	response.Success(c, stats)
}

func (s *Server) purgeMediaCache(c *gin.Context) {
	var body struct {
		Limit int `json:"limit"`
	}
	if err := bindOptionalJSON(c, &body); err != nil || body.Limit < 0 || body.Limit > 1000 {
		response.BadRequest(c, "limit must be between 1 and 1000")
		return
	}
	if body.Limit == 0 {
		body.Limit = 100
	}
	organizationID := currentOrganizationID(c)
	purged, err := s.Cache.PurgeExpired(organizationID, body.Limit)
	if err != nil {
		response.ServerError(c, "failed to purge cache")
		return
	}
	cleanup, err := mediacleanup.New(s.Cache.DB, s.Store).ProcessOrganization(organizationID, body.Limit)
	if err != nil {
		response.ServerError(c, "cache entries were queued but file cleanup failed")
		return
	}
	response.Success(c, gin.H{"purged": purged, "cleanup": cleanup})
}
