// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestIncidentContextToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "get_incident_context")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"provider_incident_id", "provider", "scope_id", "service_id", "since", "until", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsGetIncidentContext(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_incident_context", map[string]any{
		"provider_incident_id": "inc-123",
		"provider":             "pagerduty",
		"limit":                float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.Method, "GET"; got != want {
		t.Fatalf("route.Method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/incidents/inc-123/context"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
	}
	if got, want := route.Query["provider"], "pagerduty"; got != want {
		t.Fatalf("route.Query[provider] = %#v, want %#v", got, want)
	}
}
