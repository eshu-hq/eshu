// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

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
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/dead-code/investigate"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, _ := route.body.(map[string]any)
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
}
