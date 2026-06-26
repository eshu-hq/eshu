// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestComponentExtensionToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"list_component_extensions", "get_component_extension_diagnostics"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		if fileProperties, ok := properties["limit"]; ok {
			if _, ok := fileProperties.(map[string]any); !ok {
				t.Fatalf("tool %s limit schema type = %T, want map[string]any", name, fileProperties)
			}
		}
	}
}

func TestResolveRouteMapsListComponentExtensions(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_component_extensions", map[string]any{
		"limit": float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/component-extensions"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetComponentExtensionDiagnostics(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_component_extension_diagnostics", map[string]any{
		"component_id": "comp-123",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/component-extensions/comp-123/diagnostics"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
