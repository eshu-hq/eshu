// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestPreChangeImpactToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"analyze_pre_change_impact", "plan_developer_change"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		for _, field := range []string{"repo_id", "base_ref", "head_ref", "changed_paths", "changes", "target", "target_type", "service_name", "workload_id", "resource_id", "max_depth", "limit"} {
			if _, ok := properties[field]; !ok {
				t.Fatalf("tool %s properties missing %q", name, field)
			}
		}
	}
}

func TestResolveRouteMapsAnalyzePreChangeImpact(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_pre_change_impact", map[string]any{
		"repo_id":   "repo-1",
		"base_ref":  "main",
		"head_ref":  "feature-x",
		"max_depth": float64(4),
		"limit":     float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/impact/pre-change"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsPlanDeveloperChange(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("plan_developer_change", map[string]any{
		"repo_id":          "repo-1",
		"base_ref":         "main",
		"head_ref":         "feature-x",
		"developer_intent": "add auth middleware",
		"max_depth":        float64(4),
		"limit":            float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/impact/developer-change-plan"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
