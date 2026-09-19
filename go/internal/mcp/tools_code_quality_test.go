// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

// TestCodebaseToolsSplicePreservesQualityOrder pins the long-standing
// registration positions of the three quality definitions inside
// codebaseTools. The names are literal here, independent of the child
// package's own order, so a reorder of codequalitytools.Tools that would
// silently move the splice fails this test rather than shifting both sides
// of the comparison.
func TestCodebaseToolsSplicePreservesQualityOrder(t *testing.T) {
	t.Parallel()

	want := []string{
		"calculate_cyclomatic_complexity",
		"find_most_complex_functions",
		"inspect_code_quality",
	}
	codebase := codebaseTools()
	start := -1
	for i, tool := range codebase {
		if tool.Name == want[0] {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%q not found in codebaseTools()", want[0])
	}
	for offset, name := range want {
		got := codebase[start+offset].Name
		if got != name {
			t.Fatalf("codebaseTools()[%d] = %q, want %q", start+offset, got, name)
		}
	}
}

func TestCodeQualityToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "inspect_code_quality")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"check", "repo_id", "language", "entity_id", "function_name", "min_complexity", "min_lines", "min_arguments", "limit", "offset"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsInspectCodeQuality(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("inspect_code_quality", map[string]any{
		"check":          "complexity",
		"repo_id":        "repo-1",
		"language":       "go",
		"min_complexity": float64(10),
		"limit":          float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/quality/inspect"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["check"], "complexity"; got != want {
		t.Fatalf("body[check] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
}
