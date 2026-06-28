// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestSemanticSearchToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "search_semantic_context")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"repo_id", "query", "mode", "limit", "timeout_ms", "service_id", "workload_id", "environment", "source_kinds", "languages", "rerank"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsSearchSemanticContext(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("search_semantic_context", map[string]any{
		"repo_id":    "repo-1",
		"query":      "authentication flow",
		"mode":       "hybrid",
		"limit":      float64(10),
		"timeout_ms": float64(5000),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/search/semantic"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, _ := route.body.(map[string]any)
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
}
