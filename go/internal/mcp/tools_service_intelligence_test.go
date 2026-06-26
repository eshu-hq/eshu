// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestServiceIntelligenceToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "get_service_intelligence_report")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"workload_id", "service_name", "repo", "repository_id", "repo_id", "environment"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsGetServiceIntelligenceReport(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_service_intelligence_report", map[string]any{
		"workload_id": "wl-svc-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/services/wl-svc-1/intelligence-report"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
