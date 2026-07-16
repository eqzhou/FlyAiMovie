package httpapi

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxEpisodesPerDrama = 500

func (s *Server) registerDramas(api *gin.RouterGroup) {
	g := api.Group("/dramas")
	g.GET("", s.listDramas)
	g.POST("", s.createDrama)
	g.GET("/stats", s.dramaStats)
	g.GET("/:id", s.getDrama)
	g.PUT("/:id", s.updateDrama)
	g.DELETE("/:id", s.deleteDrama)
}

func (s *Server) listDramas(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")
	keyword := c.Query("keyword")
	if page < 1 {
		page = 1
	} else if page > 1_000_000 {
		page = 1_000_000
	}
	if pageSize < 1 {
		pageSize = 20
	} else if pageSize > 100 {
		pageSize = 100
	}
	var rows []models.Drama
	q := organizationDB(c).Where("deleted_at IS NULL").Order("updated_at desc")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if keyword != "" {
		q = q.Where("title LIKE ?", "%"+keyword+"%")
	}
	var total int64
	q.Model(&models.Drama{}).Count(&total)
	q.Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows)

	items := make([]gin.H, 0, len(rows))
	for _, d := range rows {
		var eps []models.Episode
		var chars []models.Character
		var scenes []models.Scene
		organizationDB(c).Where("drama_id = ? AND deleted_at IS NULL", d.ID).Find(&eps)
		organizationDB(c).Where("drama_id = ? AND deleted_at IS NULL", d.ID).Find(&chars)
		organizationDB(c).Where("drama_id = ? AND deleted_at IS NULL", d.ID).Find(&scenes)
		var tags any
		if d.Tags != "" {
			_ = json.Unmarshal([]byte(d.Tags), &tags)
		}
		items = append(items, gin.H{
			"id": d.ID, "title": d.Title, "description": d.Description, "genre": d.Genre,
			"style": d.Style, "status": d.Status, "thumbnail": d.Thumbnail, "tags": tags,
			"total_episodes": len(eps), "episodes": eps, "characters": chars, "scenes": scenes,
			"created_at": d.CreatedAt, "updated_at": d.UpdatedAt,
		})
	}
	response.Success(c, gin.H{
		"items":      items,
		"pagination": gin.H{"page": page, "page_size": pageSize, "total": total, "total_pages": (int(total) + pageSize - 1) / pageSize},
	})
}

func (s *Server) createDrama(c *gin.Context) {
	var body struct {
		Title         string   `json:"title"`
		Description   string   `json:"description"`
		Genre         string   `json:"genre"`
		Style         string   `json:"style"`
		Tags          []string `json:"tags"`
		Metadata      string   `json:"metadata"`
		TotalEpisodes int      `json:"total_episodes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	if body.Title == "" {
		response.BadRequest(c, "title required")
		return
	}
	if body.TotalEpisodes < 0 || body.TotalEpisodes > maxEpisodesPerDrama {
		response.BadRequest(c, "total_episodes must be between 0 and "+strconv.Itoa(maxEpisodesPerDrama))
		return
	}
	ts := response.Now()
	tags, _ := json.Marshal(body.Tags)
	style := body.Style
	if style == "" {
		style = "realistic"
	}
	n := body.TotalEpisodes
	if n < 1 {
		n = 1
	}
	d := models.Drama{
		OrganizationID: currentOrganizationID(c),
		Title:          body.Title, Description: body.Description, Genre: body.Genre, Style: style,
		Tags: string(tags), Metadata: body.Metadata, Status: "draft", TotalEpisodes: n,
		CreatedAt: ts, UpdatedAt: ts,
	}
	err := organizationDB(c).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		episodes := make([]models.Episode, 0, n)
		for i := 1; i <= n; i++ {
			episodes = append(episodes, models.Episode{
				OrganizationID: d.OrganizationID, DramaID: d.ID, EpisodeNumber: i, Title: "第" + strconv.Itoa(i) + "集",
				Status: "draft", CreatedAt: ts, UpdatedAt: ts,
			})
		}
		return tx.Create(&episodes).Error
	})
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.Created(c, d)
}

func (s *Server) getDrama(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var d models.Drama
	if err := organizationDB(c).First(&d, id).Error; err != nil || d.DeletedAt != nil {
		response.NotFound(c, "剧本不存在")
		return
	}
	var eps []models.Episode
	var chars []models.Character
	var scenes []models.Scene
	var props []models.Prop
	organizationDB(c).Where("drama_id = ? AND deleted_at IS NULL", d.ID).Order("episode_number").Find(&eps)
	organizationDB(c).Where("drama_id = ? AND deleted_at IS NULL", d.ID).Find(&chars)
	organizationDB(c).Where("drama_id = ? AND deleted_at IS NULL", d.ID).Find(&scenes)
	organizationDB(c).Where("drama_id = ? AND deleted_at IS NULL", d.ID).Find(&props)
	var tags any
	if d.Tags != "" {
		_ = json.Unmarshal([]byte(d.Tags), &tags)
	}
	response.Success(c, gin.H{
		"id": d.ID, "title": d.Title, "description": d.Description, "genre": d.Genre, "style": d.Style,
		"status": d.Status, "thumbnail": d.Thumbnail, "tags": tags, "metadata": d.Metadata,
		"total_episodes": len(eps), "episodes": eps, "characters": chars, "scenes": scenes, "props": props,
		"created_at": d.CreatedAt, "updated_at": d.UpdatedAt,
	})
}

func (s *Server) updateDrama(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		response.BadRequest(c, "invalid drama id")
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	updates := map[string]any{"updated_at": response.Now()}
	for _, k := range []string{"title", "description", "genre", "style", "status", "thumbnail", "metadata"} {
		maxRunes := maxTextRunes
		if k == "title" || k == "genre" || k == "style" || k == "status" {
			maxRunes = maxNameRunes
		}
		v, ok, fieldErr := stringUpdate(body, k, maxRunes)
		if fieldErr != nil {
			response.BadRequest(c, fieldErr.Error())
			return
		}
		if ok {
			if k == "title" {
				v = strings.TrimSpace(v)
				if v == "" {
					response.BadRequest(c, "title must not be empty")
					return
				}
			}
			updates[k] = v
		}
	}
	if tags, ok := body["tags"]; ok {
		items, valid := tags.([]any)
		if !valid || len(items) > 100 {
			response.BadRequest(c, "tags must be an array with at most 100 items")
			return
		}
		for _, item := range items {
			tag, valid := item.(string)
			if !valid || len([]rune(tag)) > maxNameRunes {
				response.BadRequest(c, "each tag must be a string of at most 200 characters")
				return
			}
		}
		b, _ := json.Marshal(items)
		updates["tags"] = string(b)
	}
	if len(updates) == 1 {
		response.BadRequest(c, "at least one drama field is required")
		return
	}
	result := organizationDB(c).Model(&models.Drama{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		response.ServerError(c, "failed to update drama")
		return
	}
	if result.RowsAffected == 0 {
		response.NotFound(c, "剧本不存在")
		return
	}
	response.Success(c, nil)
}

func (s *Server) deleteDrama(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	now := response.Now()
	result := organizationDB(c).Model(&models.Drama{}).Where("id = ? AND deleted_at IS NULL", id).Update("deleted_at", now)
	if result.Error != nil {
		response.ServerError(c, "failed to delete drama")
		return
	}
	if result.RowsAffected == 0 {
		response.NotFound(c, "剧本不存在")
		return
	}
	response.Success(c, nil)
}

func (s *Server) dramaStats(c *gin.Context) {
	var rows []models.Drama
	organizationDB(c).Where("deleted_at IS NULL").Find(&rows)
	by := map[string]int{}
	for _, d := range rows {
		by[d.Status]++
	}
	list := make([]gin.H, 0)
	for k, v := range by {
		list = append(list, gin.H{"status": k, "count": v})
	}
	response.Success(c, gin.H{"total": len(rows), "by_status": list})
}
