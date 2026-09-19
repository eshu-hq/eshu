// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

// TestCodebaseToolsSplicePreservesQualityOrder pins the absolute
// registration indices of the three quality definitions inside
// codebaseTools, with the replatforming-ownership predecessor and the
// execute-cypher successor. Every name and index is literal here,
// independent of the child package's own order, so a reorder of
// codequalitytools.Tools or a move of the whole splice fails this test
// instead of shifting with the code under test.
func TestCodebaseToolsSplicePreservesQualityOrder(t *testing.T) {
	t.Parallel()

	codebase := codebaseTools()
	want := map[int]string{
		22: "find_unmanaged_resource_owners",
		23: "calculate_cyclomatic_complexity",
		24: "find_most_complex_functions",
		25: "inspect_code_quality",
		26: "execute_cypher_query",
	}
	for index, name := range want {
		if index >= len(codebase) {
			t.Fatalf("codebaseTools() has %d tools, want index [%d] = %q", len(codebase), index, name)
		}
		if got := codebase[index].Name; got != name {
			t.Fatalf("codebaseTools()[%d] = %q, want %q", index, got, name)
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
