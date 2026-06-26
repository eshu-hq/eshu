// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestFreshnessToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"get_generation_lifecycle", "get_changed_since", "get_service_changed_since"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		if _, ok := properties["limit"]; !ok && name == "get_generation_lifecycle" {
			t.Fatalf("tool %s properties missing limit", name)
		}
	}
}

func TestResolveRouteMapsGetGenerationLifecycle(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_generation_lifecycle", map[string]any{
		"scope_id": "scope-1",
		"limit":    float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/freshness/generations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetChangedSince(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_changed_since", map[string]any{
		"scope_id":            "scope-1",
		"since_generation_id": "gen-1",
		"sample_limit":        float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/freshness/changed-since"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetServiceChangedSince(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_service_changed_since", map[string]any{
		"service_id":          "svc-1",
		"since_generation_id": "gen-1",
		"sample_limit":        float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/freshness/services/changed-since"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
