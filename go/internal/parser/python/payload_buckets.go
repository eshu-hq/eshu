// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
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
