package httpapi

import (
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/textutil"
	"gorm.io/gorm"
)

func uniquePositiveIDs(value any) []uint {
	return textutil.UniquePositiveIDs(value, false)
}

func validateStoryboardResources(tx *gorm.DB, dramaID, sceneID uint, characterIDs []uint) error {
	if sceneID > 0 {
		var scene models.Scene
		if err := tx.Where("id = ? AND deleted_at IS NULL", sceneID).First(&scene).Error; err != nil {
			return err
		}
		if scene.DramaID != dramaID {
			return errOwnershipMismatch
		}
	}
	for _, characterID := range characterIDs {
		var character models.Character
		if err := tx.Where("id = ? AND deleted_at IS NULL", characterID).First(&character).Error; err != nil {
			return err
		}
		if character.DramaID != dramaID {
			return errOwnershipMismatch
		}
	}
	return nil
}

func asString(v any) string {
	return textutil.AsString(v, false)
}

func asInt(v any) int {
	return textutil.AsInt(v, false)
}

func asUint(v any) uint { return textutil.AsUint(v, false) }
