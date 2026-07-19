package generation

import (
	"errors"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"gorm.io/gorm"
)

func registerAsset(asset models.Asset) {
	_ = registerAssetWithDB(db.DB, asset)
}

func registerAssetWithDB(database *gorm.DB, asset models.Asset) error {
	if asset.URL == "" || asset.Type == "" {
		return nil
	}
	var existing models.Asset
	query := database.Where("organization_id = ? AND deleted_at IS NULL", asset.OrganizationID)
	switch {
	case asset.ImageGenID != nil:
		query = query.Where("image_gen_id = ?", *asset.ImageGenID)
	case asset.VideoGenID != nil:
		query = query.Where("video_gen_id = ?", *asset.VideoGenID)
	case asset.StoryboardID != nil:
		query = query.Where("storyboard_id = ? AND category = ? AND url = ?", *asset.StoryboardID, asset.Category, asset.URL)
	default:
		query = query.Where("url = ?", asset.URL)
	}
	if err := query.First(&existing).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if asset.CreatedAt == "" {
		asset.CreatedAt = response.Now()
	}
	if asset.UpdatedAt == "" {
		asset.UpdatedAt = asset.CreatedAt
	}
	return database.Create(&asset).Error
}
