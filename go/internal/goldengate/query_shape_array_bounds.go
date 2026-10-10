// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import "fmt"

// evaluateJSONArrayBound applies an exact path to one nested response array.
// Requiring one resolved array prevents wildcard paths from silently selecting
// whichever sibling happens to satisfy the bound.
func evaluateJSONArrayBound(root any, path string, bound QueryArrayBound) string {
	if bound.MinimumResults < 0 || bound.MaximumResults < 0 ||
		(bound.MaximumResults > 0 && bound.MaximumResults < bound.MinimumResults) {
		return fmt.Sprintf("JSON array bound %q has invalid range [%d,%d]", path, bound.MinimumResults, bound.MaximumResults)
	}
	values, err := resolveJSONPath(root, path)
	if err != nil {
		return fmt.Sprintf("JSON array bound %q failed: %v", path, err)
	}
	if len(values) != 1 {
		return fmt.Sprintf("JSON array bound %q resolved %d values, want one array", path, len(values))
	}
	items, ok := values[0].([]any)
	if !ok {
		return fmt.Sprintf("JSON array bound %q did not resolve an array", path)
	}
	if len(items) < bound.MinimumResults || (bound.MaximumResults > 0 && len(items) > bound.MaximumResults) {
		return fmt.Sprintf("JSON array bound %q has %d results, want [%d,%d]", path, len(items), bound.MinimumResults, bound.MaximumResults)
	}
	if len(bound.ResultItemRequiredFields) == 0 {
		return ""
	}
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return fmt.Sprintf("JSON array bound %q item %d is not an object", path, index)
		}
		for _, field := range bound.ResultItemRequiredFields {
			if _, ok := object[field]; !ok {
				return fmt.Sprintf("JSON array bound %q item %d missing required field %q", path, index, field)
			}
		}
	}
	return ""
}
