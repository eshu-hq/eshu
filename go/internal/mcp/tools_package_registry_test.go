// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestPackageRegistryToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"list_package_registry_dependencies", "list_package_registry_correlations"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		if _, ok := properties["limit"]; !ok {
			t.Fatalf("tool %s properties missing limit", name)
		}
	}
}

func TestResolveRouteMapsListPackageRegistryDependencies(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_package_registry_dependencies", map[string]any{
		"package_id": "pkg-1",
		"limit":      float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/package-registry/dependencies"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsListPackageRegistryCorrelations(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_package_registry_correlations", map[string]any{
		"package_id":        "pkg-1",
		"relationship_kind": "ownership",
		"limit":             float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/package-registry/correlations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
