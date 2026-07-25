package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func bindOptionalJSON(c *gin.Context, target any) error {
	err := c.ShouldBindJSON(target)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func bindSingleJSON(c *gin.Context, target any) error {
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

const (
	maxNameRunes = 200
	maxTextRunes = 20_000
)

func rejectUnknownFields(body map[string]any, allowed ...string) error {
	known := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		known[field] = struct{}{}
	}
	unknown := make([]string, 0)
	for field := range body {
		if _, ok := known[field]; !ok {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("unknown field %q", unknown[0])
}

func stringUpdate(body map[string]any, key string, maxRunes int) (string, bool, error) {
	value, exists := body[key]
	if !exists {
		return "", false, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", false, fmt.Errorf("%s must be a string", key)
	}
	if len([]rune(text)) > maxRunes {
		return "", false, fmt.Errorf("%s is too long", key)
	}
	return text, true, nil
}

func positiveJSONInt(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok || number < 1 || number > float64(math.MaxInt) || math.Trunc(number) != number {
		return 0, false
	}
	return int(number), true
}

func nonNegativeJSONInt(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok || number < 0 || number > float64(math.MaxInt) || math.Trunc(number) != number {
		return 0, false
	}
	return int(number), true
}

func positiveJSONIDs(value any) ([]uint, bool) {
	items, ok := value.([]any)
	if !ok || len(items) > 1000 {
		return nil, false
	}
	seen := make(map[uint]struct{}, len(items))
	result := make([]uint, 0, len(items))
	for _, item := range items {
		number, valid := positiveJSONInt(item)
		if !valid {
			return nil, false
		}
		id := uint(number)
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result, true
}

// activeResourceQuery scopes organization reads to non-soft-deleted rows.
func activeResourceQuery(c *gin.Context) *gorm.DB {
	return organizationDB(c).Where("deleted_at IS NULL")
}

func findActiveDrama(c *gin.Context, id uint, drama *models.Drama) error {
	return activeResourceQuery(c).First(drama, id).Error
}

func findActiveEpisode(c *gin.Context, id uint, episode *models.Episode) error {
	return activeResourceQuery(c).First(episode, id).Error
}

func activeTx(tx *gorm.DB) *gorm.DB {
	return tx.Where("deleted_at IS NULL")
}

func findActiveCharacter(c *gin.Context, id uint, character *models.Character) error {
	return activeResourceQuery(c).First(character, id).Error
}

func findActiveScene(c *gin.Context, id uint, scene *models.Scene) error {
	return activeResourceQuery(c).First(scene, id).Error
}

func findActiveProp(c *gin.Context, id uint, prop *models.Prop) error {
	return activeResourceQuery(c).First(prop, id).Error
}

func findActiveStoryboard(c *gin.Context, id uint, storyboard *models.Storyboard) error {
	return activeResourceQuery(c).First(storyboard, id).Error
}
