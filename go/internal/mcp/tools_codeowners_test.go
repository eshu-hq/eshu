// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestCodeownersOwnershipToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_codeowners_ownership")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	required, ok := schema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "repository_id" {
		t.Fatalf("required = %#v, want [repository_id]", schema["required"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"repository_id", "limit", "after_order_index", "after_pattern", "after_ref"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsCodeownersOwnership(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_codeowners_ownership", map[string]any{
		"repository_id": "repo-1",
		"limit":         float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/codeowners/ownership"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["repository_id"], "repo-1"; got != want {
		t.Fatalf("route.query[repository_id] = %q, want %q", got, want)
	}
	if got, want := route.query["limit"], "25"; got != want {
		t.Fatalf("route.query[limit] = %q, want %q", got, want)
	}
	if got, want := route.query["after_order_index"], ""; got != want {
		t.Fatalf("route.query[after_order_index] = %q, want %q (absent cursor must stay empty, not coerce to 0)", got, want)
	}
}

func TestResolveRouteMapsCodeownersOwnershipCursor(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_codeowners_ownership", map[string]any{
		"repository_id":     "repo-1",
		"after_order_index": float64(3),
		"after_pattern":     "*.go",
		"after_ref":         "@org/team-a",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.query["after_order_index"], "3"; got != want {
		t.Fatalf("route.query[after_order_index] = %q, want %q", got, want)
	}
	if got, want := route.query["after_pattern"], "*.go"; got != want {
		t.Fatalf("route.query[after_pattern] = %q, want %q", got, want)
	}
	if got, want := route.query["after_ref"], "@org/team-a"; got != want {
		t.Fatalf("route.query[after_ref] = %q, want %q", got, want)
	}
}
