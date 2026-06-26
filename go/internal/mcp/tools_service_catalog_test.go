// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestServiceCatalogToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_service_catalog_correlations")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"scope_id", "provider", "entity_ref", "repository_id", "service_id", "workload_id", "owner_ref", "outcome", "drift_status", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsServiceCatalogCorrelations(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_service_catalog_correlations", map[string]any{
		"repository_id": "repo-1",
		"provider":      "backstage",
		"limit":         float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/service-catalog/correlations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
