package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/agents"
	"github.com/eqzhou/flyaimovie/internal/services/skillregistry"
	"github.com/gin-gonic/gin"
)

const maxSkillRequestBytes int64 = skillregistry.MaxMainBytes + skillregistry.MaxReferencesBytes + 64*1024

func (s *Server) registerSkillRegistryRoutes(api *gin.RouterGroup) {
	api.GET("/skills", s.listSkillRegistry)
	api.GET("/skills/:id", s.getSkillRegistry)
	api.POST("/skills/:id/versions", s.createSkillVersion)
	api.POST("/skills/:id/versions/:versionID/publish", s.publishSkillVersion)
	api.POST("/skills/:id/versions/:versionID/rollback", s.rollbackSkillVersion)
	api.POST("/skills/:id/archive", s.archiveSkill)
}

func (s *Server) skillService() *skillregistry.Service { return skillregistry.New(db.DB) }

func (s *Server) listSkillRegistry(c *gin.Context) {
	rows, err := s.skillService().List(currentOrganizationID(c), true)
	if err != nil {
		response.ServerError(c, "failed to list skills")
		return
	}
	byAgent := make(map[string]models.Skill, len(rows))
	for _, row := range rows {
		byAgent[row.AgentType] = row
	}
	out := make([]gin.H, 0, len(agents.ValidAgentTypes))
	for _, agentType := range agents.ValidAgentTypes {
		item := gin.H{"id": agentType, "agent_type": agentType, "source": "builtin"}
		if row, ok := byAgent[agentType]; ok {
			item["registry"] = row
			// The runner only uses the database skill when a non-archived
			// published version exists; otherwise it falls back to builtin.
			if row.ArchivedAt == nil && row.PublishedVersionID != nil {
				item["source"] = "database"
			}
		}
		out = append(out, item)
	}
	response.Success(c, out)
}

func (s *Server) getSkillRegistry(c *gin.Context) {
	agentType := c.Param("id")
	if !skillregistry.IsValidAgent(agentType) {
		response.NotFound(c, "skill not found")
		return
	}
	organizationID := currentOrganizationID(c)
	detail, err := s.skillService().Get(organizationID, agentType)
	if err == nil {
		if actor, ok := currentAuth(c); ok && actor.Membership.Role != "owner" && actor.Membership.Role != "admin" {
			response.Success(c, sanitizedPublishedSkillDetail(*detail))
			return
		}
		response.Success(c, detail)
		return
	}
	if !errors.Is(err, skillregistry.ErrNotFound) {
		response.ServerError(c, "failed to load skill")
		return
	}
	b, readErr := os.ReadFile(filepath.Join(s.Agents.SkillsDir, agentType, "SKILL.md"))
	if readErr != nil {
		response.NotFound(c, "skill not found")
		return
	}
	response.Success(c, gin.H{"id": agentType, "agent_type": agentType, "source": "builtin", "content": string(b), "versions": []any{}, "publications": []any{}})
}

func sanitizedPublishedSkillDetail(detail skillregistry.Detail) gin.H {
	out := gin.H{
		"id": detail.ID, "agent_type": detail.AgentType, "source": "database",
		"published_version": nil,
	}
	if version := detail.PublishedVersion; version != nil && detail.ArchivedAt == nil {
		out["published_version"] = gin.H{
			"id": version.ID, "version": version.Version,
			"main_markdown": version.MainMarkdown, "references_json": version.ReferencesJSON,
			"content_sha256": version.ContentSHA256, "created_at": version.CreatedAt,
		}
	}
	return out
}

func (s *Server) requireSkillAdmin(c *gin.Context) (uint, uint, bool) {
	if !s.Cfg.Auth.Enabled {
		return 0, 0, true
	}
	actor, ok := currentAuth(c)
	if !ok || (actor.Membership.Role != "owner" && actor.Membership.Role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "admin role required"})
		return 0, 0, false
	}
	return actor.Organization.ID, actor.User.ID, true
}

func bindSkillVersionInput(c *gin.Context) (skillregistry.VersionInput, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSkillRequestBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var input skillregistry.VersionInput
	if err := decoder.Decode(&input); err != nil {
		response.BadRequest(c, "invalid skill version payload")
		return input, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		response.BadRequest(c, "invalid skill version payload")
		return input, false
	}
	return input, true
}

func (s *Server) createSkillVersion(c *gin.Context) {
	organizationID, userID, ok := s.requireSkillAdmin(c)
	if !ok {
		return
	}
	if !skillregistry.IsValidAgent(c.Param("id")) {
		response.BadRequest(c, "invalid agent type")
		return
	}
	input, ok := bindSkillVersionInput(c)
	if !ok {
		return
	}
	row, err := s.skillService().CreateVersion(organizationID, userID, c.Param("id"), input)
	if err != nil {
		if errors.Is(err, skillregistry.ErrInvalid) {
			response.BadRequest(c, err.Error())
		} else {
			response.ServerError(c, "failed to create skill version")
		}
		return
	}
	response.Created(c, row)
}

func parseSkillVersionID(c *gin.Context) (uint, bool) {
	value, err := strconv.ParseUint(c.Param("versionID"), 10, 64)
	if err != nil || value == 0 {
		response.BadRequest(c, "invalid version id")
		return 0, false
	}
	return uint(value), true
}

func (s *Server) publishSkillVersion(c *gin.Context)  { s.changeSkillPublication(c, false) }
func (s *Server) rollbackSkillVersion(c *gin.Context) { s.changeSkillPublication(c, true) }

func (s *Server) changeSkillPublication(c *gin.Context, rollback bool) {
	organizationID, userID, ok := s.requireSkillAdmin(c)
	if !ok {
		return
	}
	if !skillregistry.IsValidAgent(c.Param("id")) {
		response.BadRequest(c, "invalid agent type")
		return
	}
	versionID, ok := parseSkillVersionID(c)
	if !ok {
		return
	}
	var row any
	var err error
	if rollback {
		row, err = s.skillService().Rollback(organizationID, userID, c.Param("id"), versionID)
	} else {
		row, err = s.skillService().Publish(organizationID, userID, c.Param("id"), versionID)
	}
	if err != nil {
		skillServiceError(c, err)
		return
	}
	response.Success(c, row)
}

func (s *Server) archiveSkill(c *gin.Context) {
	organizationID, userID, ok := s.requireSkillAdmin(c)
	if !ok {
		return
	}
	if !skillregistry.IsValidAgent(c.Param("id")) {
		response.BadRequest(c, "invalid agent type")
		return
	}
	row, err := s.skillService().Archive(organizationID, userID, c.Param("id"))
	if err != nil {
		skillServiceError(c, err)
		return
	}
	response.Success(c, row)
}

func skillServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, skillregistry.ErrNotFound):
		response.NotFound(c, "skill or version not found")
	case errors.Is(err, skillregistry.ErrInvalid):
		response.BadRequest(c, err.Error())
	default:
		response.ServerError(c, "skill registry operation failed")
	}
}
