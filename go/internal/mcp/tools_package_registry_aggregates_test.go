// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestPackageRegistryAggregateToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"count_package_registry_packages", "get_package_registry_package_inventory"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		if _, ok := properties["ecosystem"]; !ok {
			t.Fatalf("tool %s properties missing ecosystem", name)
		}
		if _, ok := properties["visibility"]; !ok {
			t.Fatalf("tool %s properties missing visibility", name)
		}
	}
}

func TestResolveRouteMapsCountPackageRegistryPackages(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("count_package_registry_packages", map[string]any{
		"ecosystem":  "npm",
		"visibility": "public",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/package-registry/packages/count"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetPackageRegistryPackageInventory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_package_registry_package_inventory", map[string]any{
		"ecosystem": "pypi",
		"group_by":  "registry",
		"limit":     float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/package-registry/packages/inventory"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
