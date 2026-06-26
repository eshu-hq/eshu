// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestCodeRelationshipStoryToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "get_code_relationship_story")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"target", "entity_id", "repo_id", "language", "relationship_type", "relationship_types", "direction", "token_budget", "min_confidence", "include_transitive", "max_depth", "limit", "offset"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsCodeRelationshipStory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_code_relationship_story", map[string]any{
		"target":  "MyFunc",
		"repo_id": "repo-1",
		"limit":   float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/relationships/story"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["target"], "MyFunc"; got != want {
		t.Fatalf("body[target] = %#v, want %#v", got, want)
	}
}
