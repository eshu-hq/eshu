// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fms

import (
	"sort"
	"strings"
)

// firstNonEmpty returns the first trimmed, non-empty value, or the empty string
// when every value is blank.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// sortedUnique returns the trimmed, deduplicated, lexically sorted set of the
// input values. The scanner uses it for member account ids so the emitted
// relationship set is stable and never keyed on AWS response order; a stable
// order keeps the generation-to-generation fact set deterministic.
func sortedUnique(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	output := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		output = append(output, trimmed)
	}
	if len(output) == 0 {
		return nil
	}
	sort.Strings(output)
	return output
}
