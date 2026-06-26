// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestCICDRunCorrelationsToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_ci_cd_run_correlations")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"scope_id", "provider", "provider_run_id", "repository_id", "commit_sha", "artifact_digest", "image_ref", "environment", "outcome", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsCICDRunCorrelations(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_ci_cd_run_correlations", map[string]any{
		"scope_id":      "scope-1",
		"repository_id": "repo-1",
		"limit":         float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/ci-cd/run-correlations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["scope_id"], "scope-1"; got != want {
		t.Fatalf("route.query[scope_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["repository_id"], "repo-1"; got != want {
		t.Fatalf("route.query[repository_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["limit"], "25"; got != want {
		t.Fatalf("route.query[limit] = %#v, want %#v", got, want)
	}
}
