// Package textutil holds small string helpers shared across packages.
//
// The two fallback helpers below differ only in how they treat whitespace-only
// values. Both semantics already existed in the codebase, so they are kept
// distinct rather than merged: collapsing them would silently change which
// fallback wins for values like " ".
package textutil

import "strings"

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
