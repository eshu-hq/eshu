// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestVisualizationToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "derive_visualization_packet")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"view", "source_response", "source_truth"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsDeriveVisualizationPacket(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("derive_visualization_packet", map[string]any{
		"view":            "service_story",
		"source_response": map[string]any{"data": "test"},
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/visualizations/derive"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
