package generation

import (
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func registerAsset(asset models.Asset) {
	if asset.URL == "" || asset.Type == "" {
		return
	}
	var existing models.Asset
	if err := db.DB.Where("organization_id = ? AND url = ? AND deleted_at IS NULL", asset.OrganizationID, asset.URL).First(&existing).Error; err == nil {
		return
	}
	if asset.CreatedAt == "" {
		asset.CreatedAt = response.Now()
	}
	if asset.UpdatedAt == "" {
		asset.UpdatedAt = asset.CreatedAt
	}
	_ = db.DB.Create(&asset).Error
}
