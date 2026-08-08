// Package textutil holds small string helpers shared across packages.
//
// The two fallback helpers below differ only in how they treat whitespace-only
// values. Both semantics already existed in the codebase, so they are kept
// distinct rather than merged: collapsing them would silently change which
// fallback wins for values like " ".
package textutil

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// AsString converts JSON-like values to strings. Agent payloads historically
// accepted float64 values, while HTTP payload parsing intentionally does not.
func AsString(value any, allowFloat bool) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if allowFloat {
			return fmt.Sprintf("%v", typed)
		}
	}
	return ""
}

// AsInt converts the numeric shapes produced by encoding/json and form data.
// allowLeadingDigits preserves the Agent parser's historical fmt.Sscanf
// behavior for strings such as "12px"; strict HTTP parsing uses strconv.Atoi.
func AsInt(value any, allowLeadingDigits bool) int {
	switch typed := value.(type) {
	case uint:
		if allowLeadingDigits {
			return int(typed)
		}
	case uint64:
		if allowLeadingDigits {
			return int(typed)
		}
	case int64:
		if allowLeadingDigits {
			return int(typed)
		}
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		if allowLeadingDigits {
			var parsed int
			_, _ = fmt.Sscanf(typed, "%d", &parsed)
			return parsed
		}
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
	return 0
}

func AsUint(value any, allowLeadingDigits bool) uint {
	return uint(AsInt(value, allowLeadingDigits))
}

// UniquePositiveIDs normalizes the []any shape returned by JSON decoding,
// drops zero values, and preserves first-seen order without duplicates.
func UniquePositiveIDs(value any, allowLeadingDigits bool) []uint {
	items, _ := value.([]any)
	seen := make(map[uint]struct{}, len(items))
	result := make([]uint, 0, len(items))
	for _, item := range items {
		id := AsUint(item, allowLeadingDigits)
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

// FirstNonEmpty returns the first value that is not the empty string.
// Whitespace-only values are considered non-empty and are returned as-is.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// FirstNonBlank returns the first value that contains a non-whitespace
// character. The value is returned unmodified (not trimmed).
func FirstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// ParseStringList decodes a stored list field that may be either a JSON array
// or a comma-separated string, then trims each item and drops blank ones.
//
// A value starting with "[" is decoded as JSON; anything else is split on
// commas. The result is always non-nil, so callers can persist it directly.
// Values needing a data-URI escape hatch or a length cap must handle those
// before and after the call respectively, because those rules differ per
// caller.
//
// A decode failure is returned unwrapped so each caller can attach its own
// message without the client seeing two layers of prefix.
func ParseStringList(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}, nil
	}
	var items []string
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return nil, err
		}
	} else {
		items = strings.Split(raw, ",")
	}
	clean := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			clean = append(clean, item)
		}
	}
	return clean, nil
}
