package httpapi

import (
	"strconv"

	"github.com/eqzhou/flyaimovie/internal/models"
	"gorm.io/gorm"
)

func uniquePositiveIDs(value any) []uint {
	items, _ := value.([]any)
	seen := make(map[uint]struct{}, len(items))
	result := make([]uint, 0, len(items))
	for _, item := range items {
		id := asUint(item)
		if id == 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
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
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}

func asUint(v any) uint { return uint(asInt(v)) }
