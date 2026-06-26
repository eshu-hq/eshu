// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestCodeTopicToolsRegisteredAndRoutable(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "investigate_code_topic")
	if got, want := tool.Name, "investigate_code_topic"; got != want {
		t.Fatalf("tool.Name = %q, want %q", got, want)
	}

	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"topic", "intent", "repo_id", "language", "limit", "offset"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsInvestigateCodeTopicFullBody(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("investigate_code_topic", map[string]any{
		"topic":    "repo sync authentication",
		"intent":   "explain_auth_flow",
		"repo_id":  "repo-1",
		"language": "go",
		"limit":    float64(25),
		"offset":   float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/topics/investigate"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	for key, want := range map[string]any{
		"topic":    "repo sync authentication",
		"intent":   "explain_auth_flow",
		"repo_id":  "repo-1",
		"language": "go",
		"limit":    25,
		"offset":   50,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
}
