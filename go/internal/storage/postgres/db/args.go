// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import (
	"fmt"
	"strings"
)

// CleanIDs trims, drops empties, and deduplicates an identifier list while
// preserving first-seen order. Mark/handoff statements use it so a repeated
// caller-supplied id binds once and an all-blank list fails fast upstream.
func CleanIDs(ids []string) []string {
	cleaned := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	return cleaned
}

// IDPlaceholders renders "$1, $2, ..." for count bind parameters.
func IDPlaceholders(count int) string {
	placeholders := make([]string, count)
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	return strings.Join(placeholders, ", ")
}

// IDArgs binds an identifier list followed by trailing statement arguments.
func IDArgs(ids []string, extra ...any) []any {
	args := make([]any, 0, len(ids)+len(extra))
	for _, id := range ids {
		args = append(args, id)
	}
	return append(args, extra...)
}
