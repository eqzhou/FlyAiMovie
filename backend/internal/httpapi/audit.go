package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
)

func (s *Server) auditMutations() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isUnsafeMethod(c.Request.Method) {
			c.Next()
			return
		}
		if c.Request.Method == http.MethodDelete && c.Request.URL.Path == "/api/v1/organization" {
			c.Next()
			return
		}
		c.Next()
		actor, ok := currentAuth(c)
		organizationID, userID, role := uint(0), uint(0), "system"
		if ok {
			organizationID, userID, role = actor.Organization.ID, actor.User.ID, actor.Membership.Role
		}
		resourceType, resourceID := auditResource(c.Request.URL.Path)
		entry := models.AuditLog{
			OrganizationID: organizationID, UserID: userID, Role: role,
			Action:       strings.ToLower(c.Request.Method) + "." + resourceType,
			ResourceType: resourceType, ResourceID: resourceID,
			Method: c.Request.Method, Path: c.FullPath(), StatusCode: c.Writer.Status(),
			SourceIP: remoteIP(c.Request.RemoteAddr), CreatedAt: response.Now(),
		}
		if entry.Path == "" {
			entry.Path = c.Request.URL.Path
		}
		_ = db.DB.Create(&entry).Error
	}
}

func auditResource(path string) (string, string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 {
		return "unknown", ""
	}
	resource := parts[2]
	if resource == "agent" && len(parts) > 3 {
		return "agent:" + parts[3], ""
	}
	if len(parts) > 3 {
		if _, err := strconv.ParseUint(parts[3], 10, 64); err == nil {
			return resource, parts[3]
		}
	}
	return resource, ""
}

func (s *Server) registerAuditLogs(api *gin.RouterGroup) {
	api.GET("/audit-logs", s.listAuditLogs)
}

func (s *Server) listAuditLogs(c *gin.Context) {
	actor, ok := currentAuth(c)
	if s.Cfg.Auth.Enabled && (!ok || (actor.Membership.Role != "owner" && actor.Membership.Role != "admin")) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "admin role required"})
		return
	}
	page, size := positiveInt(c.Query("page"), 1), positiveInt(c.Query("page_size"), 50)
	if size > 100 {
		size = 100
	}
	query := organizationDB(c).Model(&models.AuditLog{})
	if action := strings.TrimSpace(c.Query("action")); action != "" {
		query = query.Where("action = ?", action)
	}
	if resourceType := strings.TrimSpace(c.Query("resource_type")); resourceType != "" {
		query = query.Where("resource_type = ?", resourceType)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.ServerError(c, "failed to query audit logs")
		return
	}
	var rows []models.AuditLog
	if err := query.Order("id desc").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		response.ServerError(c, "failed to query audit logs")
		return
	}
	response.Success(c, gin.H{"items": rows, "pagination": gin.H{"page": page, "page_size": size, "total": total}})
}

func positiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	return value
}
