// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestEcosystemToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"get_ecosystem_overview", "trace_deployment_chain",
		"investigate_deployment_config", "find_blast_radius",
		"investigate_resource", "analyze_infra_relationships",
		"get_repo_summary", "get_repo_context",
		"get_relationship_evidence", "list_package_registry_packages",
		"list_package_registry_versions", "get_repo_story",
		"get_repository_coverage", "trace_resource_to_code",
		"explain_dependency_path", "find_change_surface",
		"investigate_change_surface", "compare_environments",
	} {
		_ = requireToolDefinition(t, name)
	}
}

func TestEcosystemGetEcosystemOverviewSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "get_ecosystem_overview")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("get_ecosystem_overview InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	_, ok = schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("get_ecosystem_overview properties type incorrect")
	}
}

func TestEcosystemTraceDeploymentChainSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "trace_deployment_chain")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("trace_deployment_chain InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("trace_deployment_chain properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"service_name", "direct_only", "max_depth", "include_related_module_usage"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("trace_deployment_chain schema missing %q", field)
		}
	}
}

func TestEcosystemInvestigateDeploymentConfigSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "investigate_deployment_config")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("investigate_deployment_config InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("investigate_deployment_config properties type = %T, want map[string]any", schema["properties"])
	}
	if _, ok := properties["service_name"]; !ok {
		t.Fatalf("investigate_deployment_config schema missing service_name")
	}
}

func TestEcosystemGetRelationshipEvidenceSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "get_relationship_evidence")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("get_relationship_evidence InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("get_relationship_evidence properties type = %T, want map[string]any", schema["properties"])
	}
	if _, ok := properties["resolved_id"]; !ok {
		t.Fatalf("get_relationship_evidence schema missing resolved_id")
	}
}

func TestEcosystemResolveRouteGetEcosystemOverview(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_ecosystem_overview", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/ecosystem/overview"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestEcosystemResolveRouteTraceDeploymentChain(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("trace_deployment_chain", map[string]any{
		"service_name": "my-svc",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/impact/trace-deployment-chain"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestEcosystemResolveRouteCompareEnvironments(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("compare_environments", map[string]any{
		"workload_id": "wl-1",
		"left":        "prod",
		"right":       "staging",
		"limit":       float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/compare/environments"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["workload_id"], "wl-1"; got != want {
		t.Fatalf("body[workload_id] = %#v, want %#v", got, want)
	}
	if got, want := body["left"], "prod"; got != want {
		t.Fatalf("body[left] = %#v, want %#v", got, want)
	}
	if got, want := body["right"], "staging"; got != want {
		t.Fatalf("body[right] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 50; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
}

func TestEcosystemResolveRouteGetRepoSummary(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_repo_summary", map[string]any{
		"repo_id": "repo-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/repositories/repo-1/stats"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestEcosystemResolveRouteGetRepositoryCoverage(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_repository_coverage", map[string]any{
		"repo_id": "repo-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/repositories/repo-1/coverage"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
