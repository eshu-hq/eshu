// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestReachabilityToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "trace_exposure_path")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"source", "source_entity_id", "repo_id", "max_depth"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsTraceExposurePath(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("trace_exposure_path", map[string]any{
		"source":    "handleRequest",
		"repo_id":   "repo-1",
		"max_depth": float64(5),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/impact/trace-exposure-path"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
