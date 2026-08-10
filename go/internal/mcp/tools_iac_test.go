// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

const expectedIaCFindingKindsDescription = "Optional finding kinds. When omitted, defaults to actionable existence findings: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, and ambiguous_cloud_resource. Explicitly select image_version_drift or value_comparison_inconclusive to include managed value drift or degraded comparison evidence."

func TestIACToolsAreRegistered(t *testing.T) {
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

func TestIACToolFindingKindsDescriptionsStayInParity(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"find_unmanaged_resources",
		"get_iac_management_status",
		"explain_iac_management_status",
		"propose_terraform_import_plan",
		"compose_replatforming_plan",
		"get_replatforming_rollups",
		"find_unmanaged_resource_owners",
	} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("%s input schema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s properties type = %T, want map[string]any", name, schema["properties"])
		}
		findingKinds, ok := properties["finding_kinds"].(map[string]any)
		if !ok {
			t.Fatalf("%s finding_kinds schema type = %T, want map[string]any", name, properties["finding_kinds"])
		}
		if got := findingKinds["description"]; got != expectedIaCFindingKindsDescription {
			t.Errorf("%s finding_kinds description = %q, want %q", name, got, expectedIaCFindingKindsDescription)
		}
	}
}

func TestFindUnmanagedResourcesDescriptionAllowsExplicitDriftSelection(t *testing.T) {
	t.Parallel()

	description := requireToolDefinition(t, "find_unmanaged_resources").Description
	for _, required := range []string{"default page", "explicitly select", "image_version_drift", "value_comparison_inconclusive"} {
		if !strings.Contains(description, required) {
			t.Errorf("find_unmanaged_resources description = %q, want wording containing %q", description, required)
		}
	}
}

func TestIacGetIacManagementStatusSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "get_iac_management_status")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"scope_id", "account_id", "region"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("get_iac_management_status schema missing %q", field)
		}
	}
}

func TestIacProposeTerraformImportPlanSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "propose_terraform_import_plan")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"scope_id", "account_id", "region"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("propose_terraform_import_plan schema missing %q", field)
		}
	}
}

func TestIacComposeReplatformingPlanSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "compose_replatforming_plan")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
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
