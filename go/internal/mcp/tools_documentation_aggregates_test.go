// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestDocumentationFindingAggregateToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"count_documentation_findings", "get_documentation_finding_inventory"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		for _, field := range []string{"scope_id", "finding_type", "source_id", "document_id", "status", "truth_level", "freshness_state"} {
			if _, ok := properties[field]; !ok {
				t.Fatalf("tool %s properties missing %q", name, field)
			}
		}
	}
}

func TestResolveRouteMapsCountDocumentationFindings(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("count_documentation_findings", map[string]any{
		"scope_id": "scope-1",
		"status":   "active",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/documentation/findings/count"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetDocumentationFindingInventory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_documentation_finding_inventory", map[string]any{
		"scope_id": "scope-1",
		"group_by": "status",
		"limit":    float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/documentation/findings/inventory"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
