// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestWorkItemToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_work_item_evidence")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"scope_id", "project_key", "work_item_key", "provider_work_item_id", "external_url", "url_fingerprint", "observed_after", "after_fact_id", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsListWorkItemEvidence(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_work_item_evidence", map[string]any{
		"scope_id":      "scope-1",
		"project_key":   "PROJ",
		"work_item_key": "PROJ-123",
		"limit":         float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/work-items/evidence"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["scope_id"], "scope-1"; got != want {
		t.Fatalf("route.query[scope_id] = %#v, want %#v", got, want)
	}
}
