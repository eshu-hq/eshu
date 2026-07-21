// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestResolveRouteMapsTerraformConfigStateDriftFindings(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_terraform_config_state_drift_findings", map[string]any{
		"scope_id":    "state_snapshot:s3:hash-1",
		"outcome":     "exact",
		"drift_kinds": []any{"added_in_state"},
		"limit":       float64(25),
		"offset":      float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/terraform/config-state-drift/findings" {
		t.Fatalf("route.path = %q, want /api/v0/terraform/config-state-drift/findings", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["scope_id"], "state_snapshot:s3:hash-1"; got != want {
		t.Fatalf("body[scope_id] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	kinds := body["drift_kinds"].([]any)
	if len(kinds) != 1 || kinds[0] != "added_in_state" {
		t.Fatalf("drift_kinds = %#v, want added_in_state", kinds)
	}
}

func TestTerraformConfigStateDriftFindingsSchemaRequiresScopeID(t *testing.T) {
	t.Parallel()

	tool := terraformConfigStateDriftFindingsTool()
	schema := tool.InputSchema.(map[string]any)
	required, ok := schema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "scope_id" {
		t.Fatalf("schema[required] = %#v, want [scope_id]", schema["required"])
	}
	if !strings.Contains(tool.Description, "Provide scope_id") {
		t.Fatalf("tool description = %q, want scope_id guidance", tool.Description)
	}
}
