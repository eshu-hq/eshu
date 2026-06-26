// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestInfraResourceAggregateToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"count_infra_resources", "get_infra_resource_inventory"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		if _, ok := properties["category"]; !ok {
			t.Fatalf("tool %s properties missing category", name)
		}
	}
}

func TestResolveRouteMapsCountInfraResources(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("count_infra_resources", map[string]any{
		"category": "k8s",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/infra/resources/count"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetInfraResourceInventory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_infra_resource_inventory", map[string]any{
		"category": "terraform",
		"group_by": "provider",
		"limit":    float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/infra/resources/inventory"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
