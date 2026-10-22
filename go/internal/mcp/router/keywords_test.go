// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"slices"
	"testing"
)

// TestUnsupportedSchemaFeaturesFlagsSeededSchema is the seeded RED half of the
// validator drift guard: a schema using a keyword, type, or format the
// validator does not implement must be reported, so the registry walk in the
// mcp root fails the day a tool adopts one.
func TestUnsupportedSchemaFeaturesFlagsSeededSchema(t *testing.T) {
	t.Parallel()

	seeded := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"selector": map[string]any{
				"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}},
			},
			"when":  map[string]any{"type": "string", "format": "email"},
			"blob":  map[string]any{"type": "null"},
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "string", "minItems": 1}},
		},
	}
	got := UnsupportedSchemaFeatures(seeded)
	for _, want := range []string{
		"properties.selector: keyword oneOf",
		"properties.when: format email",
		"properties.blob: type null",
		"properties.items.items: keyword minItems",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("UnsupportedSchemaFeatures() = %v, missing %q", got, want)
		}
	}
}

// TestUnsupportedSchemaFeaturesAcceptsImplementedKeywords is the GREEN half:
// every keyword the validator implements passes the walk.
func TestUnsupportedSchemaFeaturesAcceptsImplementedKeywords(t *testing.T) {
	t.Parallel()

	if got := UnsupportedSchemaFeatures(validateFixtureSchema()); len(got) != 0 {
		t.Fatalf("UnsupportedSchemaFeatures(fixture) = %v, want none", got)
	}
	for _, tool := range Tools() {
		if got := UnsupportedSchemaFeatures(tool.InputSchema); len(got) != 0 {
			t.Fatalf("UnsupportedSchemaFeatures(%s) = %v, want none", tool.Name, got)
		}
	}
}

// TestUnsupportedSchemaFeaturesRejectsUncompilablePattern proves a pattern the
// Go regexp engine cannot compile is reported rather than silently accepted.
func TestUnsupportedSchemaFeaturesRejectsUncompilablePattern(t *testing.T) {
	t.Parallel()

	got := UnsupportedSchemaFeatures(map[string]any{"type": "string", "pattern": `(?<=a)b`})
	if len(got) != 1 {
		t.Fatalf("UnsupportedSchemaFeatures(lookbehind) = %v, want one finding", got)
	}
}
