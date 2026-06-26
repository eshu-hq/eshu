// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestCICDRunCorrelationAggregateToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"count_ci_cd_run_correlations", "get_ci_cd_run_correlation_inventory"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		for _, field := range []string{"scope_id", "repository_id", "commit_sha", "provider", "artifact_digest", "image_ref", "environment", "outcome"} {
			if _, ok := properties[field]; !ok {
				t.Fatalf("tool %s properties missing %q", name, field)
			}
		}
	}
}

func TestResolveRouteMapsCountCICDRunCorrelations(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("count_ci_cd_run_correlations", map[string]any{
		"scope_id":      "scope-1",
		"repository_id": "repo-1",
		"environment":   "prod",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/ci-cd/run-correlations/count"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["scope_id"], "scope-1"; got != want {
		t.Fatalf("route.query[scope_id] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsGetCICDRunCorrelationInventory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_ci_cd_run_correlation_inventory", map[string]any{
		"scope_id":    "scope-1",
		"group_by":    "outcome",
		"environment": "prod",
		"limit":       float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/ci-cd/run-correlations/inventory"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["group_by"], "outcome"; got != want {
		t.Fatalf("route.query[group_by] = %#v, want %#v", got, want)
	}
	if got, want := route.query["limit"], "50"; got != want {
		t.Fatalf("route.query[limit] = %#v, want %#v", got, want)
	}
}
