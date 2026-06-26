// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestAnalyzeCodeRelationshipsSchemaProperties(t *testing.T) {
	t.Parallel()

	schema := analyzeCodeRelationshipsSchema()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"query_type", "target", "context", "repo_id", "start_entity_id", "end_entity_id", "scope", "max_depth", "limit", "offset", "relationship_types", "token_budget", "min_confidence"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("schema properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsDefault(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "find_callers",
		"target":     "MyFunc",
		"repo_id":    "repo-1",
		"limit":      float64(10),
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
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsCallChainQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type":      "call_chain",
		"start_entity_id": "ent-1",
		"end_entity_id":   "ent-2",
		"repo_id":         "repo-1",
		"max_depth":       float64(5),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/call-chain"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsDeadCode(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "dead_code",
		"repo_id":    "repo-1",
		"limit":      float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/dead-code"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsModuleDeps(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "module_deps",
		"target":     "mypackage",
		"repo_id":    "repo-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/relationships"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
