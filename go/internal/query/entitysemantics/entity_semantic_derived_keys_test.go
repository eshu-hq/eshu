// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entitysemantics

import (
	"sort"
	"testing"
)

// derivedKeysFixtures are rows that together make AttachSemanticSummary write
// every field it can: the language blocks are gated by language and by each
// profile's Present(), so no single row produces all six.
func derivedKeysFixtures() map[string]map[string]any {
	return map[string]map[string]any{
		"python": {
			"name": "load", "entity_name": "load", "entity_type": "Function", "language": "python",
			"file_path": "src/load.py",
			"metadata":  map[string]any{"docstring": "Loads the rows.", "decorators": []any{"cached"}},
		},
		"javascript": {
			"name": "getTab", "entity_name": "getTab", "entity_type": "Function", "language": "javascript",
			"file_path": "src/tab.js",
			"metadata":  map[string]any{"docstring": "Returns the tab.", "method_kind": "getter"},
		},
		"typescript": {
			"name": "Panel", "entity_name": "Panel", "entity_type": "Function", "language": "typescript",
			"file_path": "src/Panel.tsx",
			"metadata":  map[string]any{"docstring": "Renders the panel.", "component_wrapper_kind": "memo"},
		},
	}
}

// TestDerivedSemanticKeysMatchWhatAttachSemanticSummaryWrites pins the #7234
// invariant: ReattachSemanticSummary drops derivedSemanticKeys before it
// re-derives, so the list must be exactly the set of fields
// AttachSemanticSummary writes. A key Attach writes but the list misses would
// keep its full-docstring echo past the read-time clip; a listed key Attach
// never writes would delete a field another step set.
func TestDerivedSemanticKeysMatchWhatAttachSemanticSummaryWrites(t *testing.T) {
	t.Parallel()

	written := map[string]bool{}
	for language, row := range derivedKeysFixtures() {
		before := map[string]bool{}
		for key := range row {
			before[key] = true
		}
		AttachSemanticSummary(row)
		gained := 0
		for key := range row {
			if !before[key] {
				written[key] = true
				gained++
			}
		}
		if gained == 0 {
			t.Fatalf("%s fixture: AttachSemanticSummary wrote nothing, so the fixture no longer exercises it", language)
		}
	}

	listed := map[string]bool{}
	for _, key := range derivedSemanticKeys {
		if listed[key] {
			t.Errorf("derivedSemanticKeys lists %q twice", key)
		}
		listed[key] = true
	}
	for _, key := range sortedKeys(written) {
		if !listed[key] {
			t.Errorf("AttachSemanticSummary writes %q but derivedSemanticKeys does not list it; a clipped row would keep the full-docstring echo in it", key)
		}
	}
	for _, key := range sortedKeys(listed) {
		if !written[key] {
			t.Errorf("derivedSemanticKeys lists %q but no fixture row makes AttachSemanticSummary write it", key)
		}
	}
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
