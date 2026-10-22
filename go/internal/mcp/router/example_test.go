// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import "testing"

// TestExampleArgumentsUsesFirstEnumValueForEitherEnumType covers the #8755
// review note: registered schemas declare enums as []string as well as []any,
// including on required properties, and the example must be directly callable.
func TestExampleArgumentsUsesFirstEnumValueForEitherEnumType(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query_type": map[string]any{"type": "string", "enum": []string{"what_depends_on", "blast_radius"}},
			"view":       map[string]any{"type": "string", "enum": []any{"summary", "full"}},
			"optional":   map[string]any{"type": "string", "enum": []string{"x"}},
		},
		"required": []string{"query_type", "view"},
	}
	example := ExampleArguments(schema)
	if example["query_type"] != "what_depends_on" || example["view"] != "summary" {
		t.Fatalf("ExampleArguments() = %v, want the first enum value of each required property", example)
	}
	if _, ok := example["optional"]; ok {
		t.Fatalf("ExampleArguments() = %v, want optional properties left out", example)
	}
	if errs := ValidateArguments(schema, example); len(errs) != 0 {
		t.Fatalf("example %v fails its own schema: %v", example, errs)
	}
	for _, bad := range []map[string]any{{"query_type": "nope", "view": "summary"}, {"query_type": "blast_radius", "view": "nope"}} {
		if errs := ValidateArguments(schema, bad); len(errs) != 1 || errs[0].Keyword != "enum" {
			t.Fatalf("ValidateArguments(%v) = %v, want one enum error for either enum type", bad, errs)
		}
	}
}
