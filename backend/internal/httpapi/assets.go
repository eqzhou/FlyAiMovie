package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (s *Server) registerAssets(api *gin.RouterGroup) {
	group := api.Group("/assets")
	group.GET("", s.listAssets)
	group.GET("/:id", s.getAsset)
	group.POST("", s.createAsset)
	group.PUT("/:id", s.updateAsset)
	group.DELETE("/:id", s.deleteAsset)
	group.POST("/:id/apply", s.applyAsset)
	group.POST("/:id/probe", s.probeAsset)
	group.POST("/metadata/repair", s.repairAssetMetadata)
}

func (s *Server) listAssets(c *gin.Context) {
	query := organizationDB(c).Where("deleted_at IS NULL").Order("id desc").Limit(200)
	for _, filter := range []string{"drama_id", "episode_id", "storyboard_id", "type", "category"} {
		if value := strings.TrimSpace(c.Query(filter)); value != "" {
			query = query.Where(filter+" = ?", value)
		}
	}
	var rows []models.Asset
	if err := query.Find(&rows).Error; err != nil {
		response.ServerError(c, "failed to list assets")
		return
	}
	response.Success(c, rows)
}

func (s *Server) getAsset(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid asset id")
		return
	}
	var asset models.Asset
	if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", id).First(&asset).Error; err != nil {
		response.NotFound(c, "asset not found")
		return
	}
	response.Success(c, asset)
}

func (s *Server) createAsset(c *gin.Context) {
	var body models.Asset
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.URL) == "" || strings.TrimSpace(body.Type) == "" {
		response.BadRequest(c, "name, type and url are required")
		return
	}
	if err := validateAssetOwnership(c, body.DramaID, body.EpisodeID, body.StoryboardID); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := validateLocalMediaOwnership(c, body.URL, body.ThumbnailURL, body.LocalPath); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	now := response.Now()
	asset := models.Asset{
		OrganizationID: currentOrganizationID(c), DramaID: body.DramaID, EpisodeID: body.EpisodeID, StoryboardID: body.StoryboardID,
		StoryboardNum: body.StoryboardNum, Name: strings.TrimSpace(body.Name), Description: body.Description,
		Type: strings.TrimSpace(body.Type), Category: body.Category, URL: body.URL, ThumbnailURL: body.ThumbnailURL,
		LocalPath: body.LocalPath, FileSize: body.FileSize, MimeType: body.MimeType, Width: body.Width,
		Height: body.Height, Duration: body.Duration, Format: body.Format, ImageGenID: body.ImageGenID,
		VideoGenID: body.VideoGenID, CreatedAt: now, UpdatedAt: now,
	}
	if err := organizationDB(c).Create(&asset).Error; err != nil {
		response.ServerError(c, "failed to create asset")
		return
	}
	response.Created(c, asset)
}

func (s *Server) updateAsset(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid asset id")
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	if err := rejectUnknownFields(body, "name", "description", "type", "category", "url", "thumbnail_url", "local_path", "mime_type", "format", "file_size", "width", "height", "duration", "is_favorite"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	updates := map[string]any{"updated_at": response.Now()}
	for _, key := range []string{"name", "description", "type", "category", "url", "thumbnail_url", "local_path", "mime_type", "format"} {
		maxRunes := maxTextRunes
		if key == "name" || key == "type" || key == "category" || key == "mime_type" || key == "format" {
			maxRunes = maxNameRunes
		}
		value, ok, fieldErr := stringUpdate(body, key, maxRunes)
		if fieldErr != nil {
			response.BadRequest(c, fieldErr.Error())
			return
		}
		if ok {
			if key == "name" || key == "type" || key == "url" {
				value = strings.TrimSpace(value)
				if value == "" {
					response.BadRequest(c, key+" must not be empty")
					return
				}
			}
			updates[key] = value
		}
	}
	for _, key := range []string{"file_size", "width", "height", "duration"} {
		if value, ok := body[key]; ok {
			number, valid := nonNegativeJSONInt(value)
			if !valid {
				response.BadRequest(c, key+" must be a non-negative integer")
				return
			}
			updates[key] = number
		}
	}
	if value, ok := body["is_favorite"]; ok {
		favorite, valid := value.(bool)
		if !valid {
			response.BadRequest(c, "is_favorite must be a boolean")
			return
		}
		updates["is_favorite"] = favorite
	}
	if len(updates) == 1 {
		response.BadRequest(c, "at least one asset field is required")
		return
	}
	result := organizationDB(c).Model(&models.Asset{}).Where("id = ? AND deleted_at IS NULL", id).Updates(updates)
	if result.Error != nil {
		response.ServerError(c, "failed to update asset")
		return
	}
	if result.RowsAffected == 0 {
		response.NotFound(c, "asset not found")
		return
	}
	var asset models.Asset
	_ = organizationDB(c).First(&asset, id).Error
	response.Success(c, asset)
}

func (s *Server) deleteAsset(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid asset id")
		return
	}
	result := organizationDB(c).Model(&models.Asset{}).Where("id = ? AND deleted_at IS NULL", id).Update("deleted_at", response.Now())
	if result.Error != nil {
		response.ServerError(c, "failed to delete asset")
		return
	}
	if result.RowsAffected == 0 {
		response.NotFound(c, "asset not found")
		return
	}
	response.Success(c, nil)
}

func (s *Server) applyAsset(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid asset id")
		return
	}
	var body struct {
		StoryboardID uint   `json:"storyboard_id"`
		FrameType    string `json:"frame_type"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.StoryboardID == 0 {
		response.BadRequest(c, "storyboard_id is required")
		return
	}
	if body.FrameType == "" {
		body.FrameType = "first_frame"
	}
	if body.FrameType != "first_frame" && body.FrameType != "last_frame" && body.FrameType != "composed" {
		response.BadRequest(c, "invalid frame_type")
		return
	}
	var asset models.Asset
	if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", id).First(&asset).Error; err != nil {
		response.NotFound(c, "asset not found")
		return
	}
	if !strings.HasPrefix(asset.MimeType, "image") && asset.Type != "image" {
		response.BadRequest(c, "only image assets can be applied to storyboard frames")
		return
	}
	var storyboard models.Storyboard
	if err := organizationDB(c).First(&storyboard, body.StoryboardID).Error; err != nil {
		response.NotFound(c, "storyboard not found")
		return
	}
	if err := validateAssetOwnership(c, asset.DramaID, asset.EpisodeID, &storyboard.ID); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error()})
		return
	}
	updates := map[string]any{"updated_at": response.Now()}
	switch body.FrameType {
	case "last_frame":
		updates["last_frame_image"] = asset.URL
	case "composed":
		updates["composed_image"] = asset.URL
	default:
		updates["first_frame_image"] = asset.URL
	}
	if err := organizationDB(c).Model(&storyboard).Updates(updates).Error; err != nil {
		response.ServerError(c, "failed to apply asset")
		return
	}
	response.Success(c, gin.H{"asset_id": asset.ID, "storyboard_id": storyboard.ID, "frame_type": body.FrameType, "url": asset.URL})
}

func parsePositiveID(raw string) (uint, error) {
	id, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || id == 0 {
		return 0, errors.New("invalid id")
	}
	return uint(id), nil
}

func validateAssetOwnership(c *gin.Context, dramaID, episodeID, storyboardID *uint) error {
	var expectedDrama uint
	if dramaID != nil {
		var drama models.Drama
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *dramaID).First(&drama).Error; err != nil {
			return errors.New("drama not found")
		}
		expectedDrama = drama.ID
	}
	if episodeID != nil {
		var episode models.Episode
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *episodeID).First(&episode).Error; err != nil {
			return errors.New("episode not found")
		}
		if expectedDrama != 0 && episode.DramaID != expectedDrama {
			return errors.New("episode does not belong to drama")
		}
		expectedDrama = episode.DramaID
	}
	if storyboardID != nil {
		var storyboard models.Storyboard
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *storyboardID).First(&storyboard).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("storyboard not found")
			}
			return errors.New("failed to validate storyboard")
		}
		if episodeID != nil && storyboard.EpisodeID != *episodeID {
			return errors.New("storyboard does not belong to episode")
		}
		var storyboardEpisode models.Episode
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", storyboard.EpisodeID).First(&storyboardEpisode).Error; err != nil {
			return errors.New("storyboard episode not found")
		}
		if expectedDrama != 0 && storyboardEpisode.DramaID != expectedDrama {
			return errors.New("storyboard does not belong to drama")
		}
	}
	return nil
}
