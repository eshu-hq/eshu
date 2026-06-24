// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestResolveRouteMapsReplatformingOwnership(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_unmanaged_resource_owners", map[string]any{
		"account_id":    "123456789012",
		"region":        "us-east-1",
		"finding_kinds": []any{"unmanaged_cloud_resource"},
		"limit":         float64(25),
		"offset":        float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/replatforming/ownership-packets" {
		t.Fatalf("route.path = %q, want /api/v0/replatforming/ownership-packets", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["account_id"], "123456789012"; got != want {
		t.Fatalf("body[account_id] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	// The ownership surface is account/scope scoped over a bounded page; it must
	// not forward an arn that would narrow to one resource.
	if _, ok := body["arn"]; ok {
		t.Fatalf("body must not carry arn for the ownership page: %#v", body)
	}
}

func TestReplatformingOwnershipSchemaDocumentsScopeAndSafety(t *testing.T) {
	t.Parallel()

	tool := replatformingOwnershipTool()
	schema := tool.InputSchema.(map[string]any)
	if _, ok := schema["anyOf"]; ok {
		t.Fatal("schema must not advertise top-level anyOf")
	}
	if !strings.Contains(tool.Description, "Provide scope_id or account_id") {
		t.Fatalf("tool description = %q, want scope guidance", tool.Description)
	}
	if !strings.Contains(tool.Description, "candidate") {
		t.Fatalf("tool description = %q, want candidate (not fabricated owner) guidance", tool.Description)
	}
	if !strings.Contains(tool.Description, "provenance-only") {
		t.Fatalf("tool description = %q, want raw-tag provenance guidance", tool.Description)
	}
}
