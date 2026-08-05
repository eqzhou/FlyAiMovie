// Package textutil holds small string helpers shared across packages.
//
// The two fallback helpers below differ only in how they treat whitespace-only
// values. Both semantics already existed in the codebase, so they are kept
// distinct rather than merged: collapsing them would silently change which
// fallback wins for values like " ".
package textutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

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

// ErrInvalidJSONList reports that a value looked like a JSON array but could
// not be decoded. Callers wrap it with a domain-specific message.
var ErrInvalidJSONList = errors.New("invalid JSON list")

// ParseStringList decodes a stored list field that may be either a JSON array
// or a comma-separated string, then trims each item and drops blank ones.
//
// A value starting with "[" is decoded as JSON; anything else is split on
// commas. The result is always non-nil, so callers can persist it directly.
// Values needing a data-URI escape hatch or a length cap must handle those
// before and after the call respectively, because those rules differ per
// caller.
func ParseStringList(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}, nil
	}
	var items []string
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidJSONList, err)
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
