// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestResolveRouteMapsReplatformingRollups(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_replatforming_rollups", map[string]any{
		"account_id":    "123456789012",
		"region":        "us-east-1",
		"finding_kinds": []any{"unmanaged_cloud_resource"},
		"limit":         float64(25),
		"offset":        float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/replatforming/rollups" {
		t.Fatalf("route.path = %q, want /api/v0/replatforming/rollups", route.path)
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
	// The rollup is account/env/service scoped, not single-resource; it must not
	// forward an arn that would narrow to one resource.
	if _, ok := body["arn"]; ok {
		t.Fatalf("body must not carry arn for the rollup: %#v", body)
	}
}

func TestReplatformingRollupsSchemaDocumentsScope(t *testing.T) {
	t.Parallel()

	tool := replatformingRollupsTool()
	schema := tool.InputSchema.(map[string]any)
	if _, ok := schema["anyOf"]; ok {
		t.Fatal("schema must not advertise top-level anyOf")
	}
	if !strings.Contains(tool.Description, "Provide scope_id or account_id") {
		t.Fatalf("tool description = %q, want scope guidance", tool.Description)
	}
	if !strings.Contains(tool.Description, "rejected") {
		t.Fatalf("tool description = %q, want source-state taxonomy guidance", tool.Description)
	}
}
