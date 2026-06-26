// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestCollectorExtractionReadinessToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"list_collector_extraction_readiness", "get_collector_extraction_readiness"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		if _, ok := properties["limit"]; !ok && name == "list_collector_extraction_readiness" {
			t.Fatalf("tool %s properties missing %q", name, "limit")
		}
		if _, ok := properties["family"]; !ok && name == "get_collector_extraction_readiness" {
			t.Fatalf("tool %s properties missing %q", name, "family")
		}
	}
}

func TestResolveRouteMapsListCollectorExtractionReadiness(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_collector_extraction_readiness", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/collector-extraction-readiness"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetCollectorExtractionReadiness(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_collector_extraction_readiness", map[string]any{
		"family": "git",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/collector-extraction-readiness/git"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
