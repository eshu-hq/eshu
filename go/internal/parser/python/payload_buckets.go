// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"path/filepath"
	"slices"
	"strings"
)

// sortNamedBucket sorts a named payload bucket by (line_number, name) so the
// emitted payload is byte-stable across runs.
func sortNamedBucket(payload map[string]any, key string) {
	items, _ := payload[key].([]map[string]any)
	slices.SortFunc(items, func(left, right map[string]any) int {
		leftLine, _ := left["line_number"].(int)
		rightLine, _ := right["line_number"].(int)
		if leftLine != rightLine {
			return leftLine - rightLine
		}
		leftName, _ := left["name"].(string)
		rightName, _ := right["name"].(string)
		return strings.Compare(leftName, rightName)
	})
	payload[key] = items
}

// collectBucketNames returns the cleaned, non-empty "name" values across the
// given payload buckets.
func collectBucketNames(payload map[string]any, keys ...string) []string {
	var names []string
	for _, key := range keys {
		items, _ := payload[key].([]map[string]any)
		for _, item := range items {
			name, _ := item["name"].(string)
			if strings.TrimSpace(name) != "" {
				names = append(names, filepath.Clean(name))
			}
		}
	}
	return names
}
