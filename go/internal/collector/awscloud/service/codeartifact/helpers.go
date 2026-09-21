// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeartifact

import (
	"strings"
	"time"
)

// firstNonEmpty returns the first trimmed non-empty value, or the empty string
// when every value is blank. It selects a stable identity from a
// preferred-to-fallback list (for example ARN then name).
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// timeOrNil returns the UTC time, or nil when the time is zero, so a missing
// AWS timestamp serializes as a null attribute rather than the Go zero time.
func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}
