package httpapi

import (
	"errors"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/gin-gonic/gin"
)

func bindOptionalJSON(c *gin.Context, target any) error {
	err := c.ShouldBindJSON(target)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
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
