// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestInfraResourceSearchToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "find_infra_resources")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"query", "category", "kind", "provider", "environment", "resource_service", "resource_category", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsFindInfraResources(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_infra_resources", map[string]any{
		"query":    "my-bucket",
		"category": "terraform",
		"limit":    float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/infra/resources/search"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, _ := route.body.(map[string]any)
	if got, want := body["query"], "my-bucket"; got != want {
		t.Fatalf("body[query] = %#v, want %#v", got, want)
	}
}
