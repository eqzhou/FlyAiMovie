package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	"github.com/eqzhou/flyaimovie/internal/services/mediafetch"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (s *Server) registerUpload(api *gin.RouterGroup) {
	api.POST("/upload/image", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, mediafetch.MaxImageUploadBytes+(1<<20))
		file, err := c.FormFile("file")
		if err != nil {
			response.BadRequest(c, "file is required")
			return
		}
		if file.Size < 1 || file.Size > mediafetch.MaxImageUploadBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": http.StatusRequestEntityTooLarge, "message": "image exceeds 20 MiB limit"})
			return
		}
		f, err := file.Open()
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		defer f.Close()
		info, err := mediafetch.ValidateImageUpload(f, file.Size)
		if err != nil {
			c.JSON(http.StatusUnsupportedMediaType, gin.H{"code": http.StatusUnsupportedMediaType, "message": err.Error()})
			return
		}
		if _, err := f.Seek(0, 0); err != nil {
			response.ServerError(c, "failed to read image")
			return
		}
		rel, err := s.Images.SaveUpload(f, "upload"+info.Extension)
		if err != nil {
			response.ServerError(c, err.Error())
			return
		}
		savedRel := rel
		url := s.Store.PublicURL(rel)
		hash, size, err := mediacache.HashFile(s.Store.Abs(rel))
		if err != nil {
			_ = os.Remove(s.Store.Abs(rel))
			response.ServerError(c, "failed to hash image")
			return
		}
		temporaryKey := "upload:" + rel
		cacheObject, reused, err := s.Cache.Put(mediacache.PutInput{OrganizationID: currentOrganizationID(c), Namespace: "image_upload", Key: temporaryKey,
			ContentHash: hash, Kind: "image", LocalPath: rel, PublicURL: url, MimeType: info.MIME, Size: size})
		if err != nil {
			_ = os.Remove(s.Store.Abs(rel))
			response.ServerError(c, "failed to cache image")
			return
		}
		rel, url = cacheObject.LocalPath, cacheObject.PublicURL
		if reused && savedRel != rel {
			_ = os.Remove(s.Store.Abs(savedRel))
		}
		if err := s.bindUploadedImage(c, url, rel, hash, size, temporaryKey); err != nil {
			_ = s.Cache.Release(currentOrganizationID(c), "image_upload", temporaryKey)
			if !reused {
				_ = os.Remove(s.Store.Abs(savedRel))
			}
			response.BadRequest(c, err.Error())
			return
		}
		response.Success(c, gin.H{"url": url, "path": rel, "width": info.Width, "height": info.Height})
	})
	api.POST("/upload/media", s.uploadMedia)
}

func (s *Server) bindUploadedImage(c *gin.Context, url, rel, contentHash string, fileSize int64, temporaryCacheKey string) error {
	return db.DB.Transaction(func(tx *gorm.DB) error {
		return s.bindUploadedImageTx(c, tx, url, rel, contentHash, fileSize, temporaryCacheKey)
	})
}

func (s *Server) bindUploadedImageTx(c *gin.Context, tx *gorm.DB, url, rel, contentHash string, fileSize int64, temporaryCacheKey string) error {
	organizationQuery := func() *gorm.DB {
		if actor, ok := currentAuth(c); ok {
			return tx.Where("organization_id = ?", actor.Organization.ID)
		}
		return tx
	}
	parseID := func(key string) (*uint, error) {
		raw := strings.TrimSpace(c.PostForm(key))
		if raw == "" {
			return nil, nil
		}
		id, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("invalid %s", key)
		}
		value := uint(id)
		return &value, nil
	}
	characterID, err := parseID("character_id")
	if err != nil {
		return err
	}
	sceneID, err := parseID("scene_id")
	if err != nil {
		return err
	}
	propID, err := parseID("prop_id")
	if err != nil {
		return err
	}
	storyboardID, err := parseID("storyboard_id")
	if err != nil {
		return err
	}
	episodeID, err := parseID("episode_id")
	if err != nil {
		return err
	}
	dramaID, err := parseID("drama_id")
	if err != nil {
		return err
	}
	bindingTargets := 0
	for _, target := range []*uint{characterID, sceneID, propID, storyboardID} {
		if target != nil {
			bindingTargets++
		}
	}
	if bindingTargets > 1 {
		return errors.New("upload accepts only one binding target")
	}
	if storyboardID != nil || episodeID != nil || dramaID != nil {
		if err := validateAssetOwnership(c, dramaID, episodeID, storyboardID); err != nil {
			return err
		}
	}
	category := c.PostForm("category")
	if category == "" {
		category = "upload"
	}
	name := c.PostForm("name")
	if name == "" {
		name = "上传图片"
	}
	now := response.Now()
	if characterID != nil {
		var row models.Character
		if err := organizationQuery().First(&row, *characterID).Error; err != nil {
			return errors.New("character not found")
		}
		if dramaID != nil && row.DramaID != *dramaID {
			return errors.New("character does not belong to drama")
		}
		if episodeID != nil {
			var ep models.Episode
			if err := organizationQuery().First(&ep, *episodeID).Error; err != nil || ep.DramaID != row.DramaID {
				return errors.New("character does not belong to episode")
			}
		}
		if err := organizationQuery().Model(&row).Updates(map[string]any{"image_url": url, "local_path": rel, "updated_at": now}).Error; err != nil {
			return err
		}
		dramaID = &row.DramaID
		category = "character"
	}
	if sceneID != nil {
		var row models.Scene
		if err := organizationQuery().First(&row, *sceneID).Error; err != nil {
			return errors.New("scene not found")
		}
		if dramaID != nil && row.DramaID != *dramaID {
			return errors.New("scene does not belong to drama")
		}
		if err := organizationQuery().Model(&row).Updates(map[string]any{"image_url": url, "local_path": rel, "status": "completed", "updated_at": now}).Error; err != nil {
			return err
		}
		dramaID = &row.DramaID
		category = "scene"
	}
	if propID != nil {
		var row models.Prop
		if err := organizationQuery().First(&row, *propID).Error; err != nil {
			return errors.New("prop not found")
		}
		if dramaID != nil && row.DramaID != *dramaID {
			return errors.New("prop does not belong to drama")
		}
		if err := organizationQuery().Model(&row).Updates(map[string]any{"image_url": url, "local_path": rel, "updated_at": now}).Error; err != nil {
			return err
		}
		dramaID = &row.DramaID
		category = "prop"
	}
	asset := models.Asset{OrganizationID: currentOrganizationID(c), DramaID: dramaID, EpisodeID: episodeID, StoryboardID: storyboardID, Name: name, Type: "image", Category: category,
		URL: url, LocalPath: rel, MimeType: "image", ContentHash: contentHash, FileSize: fileSize, CreatedAt: now, UpdatedAt: now}
	if err := organizationQuery().Create(&asset).Error; err != nil {
		return err
	}
	cache := mediacache.New(tx, s.Store)
	if _, _, err := cache.Put(mediacache.PutInput{OrganizationID: asset.OrganizationID, Namespace: "asset", Key: strconv.FormatUint(uint64(asset.ID), 10),
		ContentHash: contentHash, Kind: "image", LocalPath: rel, PublicURL: url, MimeType: "image", Size: fileSize}); err != nil {
		return err
	}
	return cache.Release(asset.OrganizationID, "image_upload", temporaryCacheKey)
}

func validateGenerationOwnership(c *gin.Context, storyboardID, dramaID, sceneID, characterID, episodeID *uint) error {
	var expectedDrama uint
	var expectedEpisode uint
	mergeDrama := func(id uint) error {
		if expectedDrama != 0 && expectedDrama != id {
			return errors.New("generation resources belong to different dramas")
		}
		expectedDrama = id
		return nil
	}
	mergeEpisode := func(id uint) error {
		if expectedEpisode != 0 && expectedEpisode != id {
			return errors.New("generation resources belong to different episodes")
		}
		expectedEpisode = id
		return nil
	}
	loadEpisode := func(id uint) error {
		var episode models.Episode
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", id).First(&episode).Error; err != nil {
			return errors.New("episode not found")
		}
		if err := mergeEpisode(episode.ID); err != nil {
			return err
		}
		return mergeDrama(episode.DramaID)
	}
	if episodeID != nil {
		if err := loadEpisode(*episodeID); err != nil {
			return err
		}
	}
	if storyboardID != nil {
		var storyboard models.Storyboard
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *storyboardID).First(&storyboard).Error; err != nil {
			return errors.New("storyboard not found")
		}
		if err := loadEpisode(storyboard.EpisodeID); err != nil {
			return err
		}
	}
	if sceneID != nil {
		var scene models.Scene
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *sceneID).First(&scene).Error; err != nil {
			return errors.New("scene not found")
		}
		if err := mergeDrama(scene.DramaID); err != nil {
			return err
		}
		if scene.EpisodeID != nil {
			if err := mergeEpisode(*scene.EpisodeID); err != nil {
				return err
			}
		}
	}
	if characterID != nil {
		var character models.Character
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *characterID).First(&character).Error; err != nil {
			return errors.New("character not found")
		}
		if err := mergeDrama(character.DramaID); err != nil {
			return err
		}
	}
	if dramaID != nil {
		var drama models.Drama
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *dramaID).First(&drama).Error; err != nil {
			return errors.New("drama not found")
		}
		if err := mergeDrama(drama.ID); err != nil {
			return err
		}
	}
	return nil
}

func validateGridOwnership(c *gin.Context, dramaID, episodeID *uint, storyboardIDs []uint) error {
	var expectedDrama uint
	var expectedEpisode uint
	mergeDrama := func(id uint) error {
		if expectedDrama != 0 && expectedDrama != id {
			return errors.New("grid resources belong to different dramas")
		}
		expectedDrama = id
		return nil
	}
	mergeEpisode := func(id uint) error {
		if expectedEpisode != 0 && expectedEpisode != id {
			return errors.New("grid resources belong to different episodes")
		}
		expectedEpisode = id
		return nil
	}
	if dramaID != nil {
		var drama models.Drama
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *dramaID).First(&drama).Error; err != nil {
			return errors.New("drama not found")
		}
		if err := mergeDrama(drama.ID); err != nil {
			return err
		}
	}
	if episodeID != nil {
		var episode models.Episode
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", *episodeID).First(&episode).Error; err != nil {
			return errors.New("episode not found")
		}
		if err := mergeEpisode(episode.ID); err != nil {
			return err
		}
		if err := mergeDrama(episode.DramaID); err != nil {
			return err
		}
	}
	seen := make(map[uint]struct{}, len(storyboardIDs))
	for _, storyboardID := range storyboardIDs {
		if storyboardID == 0 {
			return errors.New("invalid storyboard id")
		}
		if _, exists := seen[storyboardID]; exists {
			continue
		}
		seen[storyboardID] = struct{}{}
		var storyboard models.Storyboard
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", storyboardID).First(&storyboard).Error; err != nil {
			return errors.New("storyboard not found")
		}
		var episode models.Episode
		if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", storyboard.EpisodeID).First(&episode).Error; err != nil {
			return errors.New("storyboard episode not found")
		}
		if err := mergeEpisode(episode.ID); err != nil {
			return err
		}
		if err := mergeDrama(episode.DramaID); err != nil {
			return err
		}
	}
	return nil
}
