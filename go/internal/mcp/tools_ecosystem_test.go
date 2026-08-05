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
	schema, _ := tool.InputSchema.(map[string]any)
	_, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("get_ecosystem_overview properties type incorrect")
	}
}

func TestEcosystemTraceDeploymentChainSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "trace_deployment_chain")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"service_name", "direct_only", "max_depth", "include_related_module_usage"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("trace_deployment_chain schema missing %q", field)
		}
	}
}

// TestEcosystemTraceDeploymentChainMaxDepthSchemaIsBounded is the #5720
// round-2 P2-2 fix: HTTP/OpenAPI now clamps max_depth to [0, 1000] (see
// openapi_paths_impact.go and impact_trace_deployment.go), but this MCP
// tool's schema advertised max_depth as a plain unbounded integer, and
// TestEcosystemTraceDeploymentChainSchema above only asserted the field
// exists, not its bound -- nothing caught the drift. Pins the schema to the
// resolved HTTP bound so schema and handler stay in lockstep.
func TestEcosystemTraceDeploymentChainMaxDepthSchemaIsBounded(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "trace_deployment_chain")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	maxDepth, ok := properties["max_depth"].(map[string]any)
	if !ok {
		t.Fatalf("trace_deployment_chain schema max_depth missing or wrong type: %#v", properties["max_depth"])
	}
	if got, want := maxDepth["minimum"], 0; got != want {
		t.Fatalf("trace_deployment_chain max_depth[minimum] = %#v, want %v", got, want)
	}
	if got, want := maxDepth["maximum"], 1000; got != want {
		t.Fatalf("trace_deployment_chain max_depth[maximum] = %#v, want %v", got, want)
	}
	if got, want := maxDepth["default"], 8; got != want {
		t.Fatalf("trace_deployment_chain max_depth[default] = %#v, want %v", got, want)
	}
}

func TestEcosystemInvestigateDeploymentConfigSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "investigate_deployment_config")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	if _, ok := properties["service_name"]; !ok {
		t.Fatalf("investigate_deployment_config schema missing service_name")
	}
}

func TestEcosystemGetRelationshipEvidenceSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "get_relationship_evidence")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
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
		"repo_id": "repo-1",
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

// TestEcosystemAnalyzeInfraRelationshipsSchemaAdvertisesWhatRunsLambdaImage
// proves the analyze_infra_relationships tool schema's query_type enum
// advertises what_runs_lambda_image, so a tool-calling model has a declared
// way to reach the AWS_lambda_function_uses_image edge (#5738). A schema-only
// assertion is not sufficient on its own; see
// TestEcosystemResolveRouteAnalyzeInfraRelationshipsWhatRunsLambdaImage for
// the dispatch-forwarding half of the contract.
func TestEcosystemAnalyzeInfraRelationshipsSchemaAdvertisesWhatRunsLambdaImage(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "analyze_infra_relationships")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	queryType, _ := properties["query_type"].(map[string]any)
	enum, _ := queryType["enum"].([]string)
	if !schemaEnumContains(enum, "what_runs_lambda_image") {
		t.Fatalf("analyze_infra_relationships query_type enum missing %q: %#v", "what_runs_lambda_image", enum)
	}
}

// TestEcosystemResolveRouteAnalyzeInfraRelationshipsWhatRunsLambdaImage
// proves dispatch actually forwards query_type=what_runs_lambda_image to the
// HTTP route as relationship_type (#5738). Removing the enum entry, or a
// dispatch change that stops forwarding query_type, must fail this test.
func TestEcosystemResolveRouteAnalyzeInfraRelationshipsWhatRunsLambdaImage(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_infra_relationships", map[string]any{
		"query_type": "what_runs_lambda_image",
		"target":     "arn:aws:lambda:us-east-1:000000000000:function:image-consumer",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/infra/relationships"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body := requireRouteBody(t, route)
	if got, want := body["relationship_type"], "what_runs_lambda_image"; got != want {
		t.Fatalf("body[relationship_type] = %#v, want %#v", got, want)
	}
	if got, want := body["entity_id"], "arn:aws:lambda:us-east-1:000000000000:function:image-consumer"; got != want {
		t.Fatalf("body[entity_id] = %#v, want %#v", got, want)
	}
}
