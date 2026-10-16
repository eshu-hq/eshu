// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package env

import (
	"fmt"
	"strings"
)

// FirstNonEmpty returns the first trimmed value that is not blank, or ""
// when every value is blank.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

// StringValue formats raw for rule-option decoding: nil reads as "", and
// any other value is trimmed.
func StringValue(raw any) string {
	if raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

// BoolFromEnv parses a truthy environment value: 1, true, yes, or on,
// case-insensitively. Anything else reads as false.
func BoolFromEnv(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
