// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

// TestCodebaseToolsSplicePreservesDeadPositions pins the absolute
// registration indices of the three dead-code definitions owned by the
// code/dead package, with the analyze-relationships predecessor and the
// find-dead-iac successor. Every name and index is literal here,
// independent of the child's own order, so a reorder of
// deadcodetools.Tools or a move of the whole splice fails this test
// instead of shifting with the code under test.
func TestCodebaseToolsSplicePreservesDeadPositions(t *testing.T) {
	t.Parallel()

	codebase := codebaseTools()
	want := map[int]string{
		9:  "analyze_code_relationships",
		10: "find_dead_code",
		11: "investigate_dead_code",
		12: "find_cross_repo_dead_code",
		13: "find_dead_iac",
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

func TestInvestigateDeadCodeToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "investigate_dead_code")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"repo_id", "language", "limit", "offset", "exclude_decorated_with"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsInvestigateDeadCodeToolRoute(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("investigate_dead_code", map[string]any{
		"repo_id":                "repo-1",
		"language":               "go",
		"limit":                  float64(25),
		"exclude_decorated_with": []any{"deprecated"},
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.Method, "POST"; got != want {
		t.Fatalf("route.Method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/code/dead-code/investigate"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
	}
	body, _ := route.Body.(map[string]any)
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
}
