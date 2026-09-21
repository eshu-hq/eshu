// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestCloudRuntimeDriftToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_cloud_runtime_drift_findings")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"scope_id", "account_id", "project_id", "subscription_id", "provider", "cloud_resource_uid", "finding_kinds", "limit", "offset"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsCloudRuntimeDrift(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_cloud_runtime_drift_findings", map[string]any{
		"scope_id": "aws:123456789012:us-east-1",
		"provider": "aws",
		"limit":    float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.Method, "POST"; got != want {
		t.Fatalf("route.Method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/cloud/runtime-drift/findings"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
	}
	body, ok := route.Body.(map[string]any)
	if !ok {
		t.Fatalf("route.Body type = %T, want map[string]any", route.Body)
	}
	if got, want := body["scope_id"], "aws:123456789012:us-east-1"; got != want {
		t.Fatalf("body[scope_id] = %#v, want %#v", got, want)
	}
	if got, want := body["provider"], "aws"; got != want {
		t.Fatalf("body[provider] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 50; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
}
