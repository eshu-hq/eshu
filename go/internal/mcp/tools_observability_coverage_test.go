// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestObservabilityCoverageToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_observability_coverage_correlations")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"scope_id", "provider", "coverage_signal", "observability_object_ref", "target_uid", "target_service_ref", "source_class", "resource_class", "outcome", "coverage_status", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsObservabilityCoverageCorrelations(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_observability_coverage_correlations", map[string]any{
		"scope_id": "scope-1",
		"provider": "aws",
		"limit":    float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/observability/coverage/correlations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
