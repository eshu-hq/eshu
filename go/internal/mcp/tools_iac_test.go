// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestIaCToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"get_iac_management_status", "explain_iac_management_status",
		"propose_terraform_import_plan", "compose_replatforming_plan",
		"list_aws_runtime_drift_findings", "get_replatforming_rollups",
		"find_unmanaged_resource_owners",
	} {
		_ = requireToolDefinition(t, name)
	}
}

func TestIaCAwsDriftAndRollupsSchema(t *testing.T) {
	t.Parallel()

	tools := map[string][]string{
		"list_aws_runtime_drift_findings": {"scope_id", "account_id", "finding_kinds", "limit", "offset"},
		"get_replatforming_rollups":       {"scope_id", "account_id"},
		"find_unmanaged_resource_owners":  {"scope_id", "account_id"},
	}
	for name, fields := range tools {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		for _, field := range fields {
			if _, ok := properties[field]; !ok {
				t.Fatalf("tool %s schema missing %q", name, field)
			}
		}
	}
}

func TestIacGetIacManagementStatusSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "get_iac_management_status")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("get_iac_management_status InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("get_iac_management_status properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"scope_id", "account_id", "region"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("get_iac_management_status schema missing %q", field)
		}
	}
}

func TestIacProposeTerraformImportPlanSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "propose_terraform_import_plan")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("propose_terraform_import_plan InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("propose_terraform_import_plan properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"scope_id", "account_id", "region"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("propose_terraform_import_plan schema missing %q", field)
		}
	}
}

func TestIacComposeReplatformingPlanSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "compose_replatforming_plan")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("compose_replatforming_plan InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("compose_replatforming_plan properties type = %T, want map[string]any", schema["properties"])
	}
	if _, ok := properties["repo_id"]; !ok {
		t.Fatalf("compose_replatforming_plan schema missing repo_id")
	}
}

func TestIacResolveRouteGetIacManagementStatus(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_iac_management_status", map[string]any{
		"scope_id": "scope-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/iac/management-status"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestIacResolveRouteExplainIacManagementStatus(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("explain_iac_management_status", map[string]any{
		"scope_id": "scope-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/iac/management-status/explain"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestIacResolveRouteComposeReplatformingPlan(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("compose_replatforming_plan", map[string]any{
		"repo_id": "repo-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/replatforming/plans"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestIacResolveRouteFindUnmanagedResourceOwners(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_unmanaged_resource_owners", map[string]any{
		"repo_id": "repo-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/replatforming/ownership-packets"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
