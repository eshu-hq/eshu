// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

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

// TestEcosystemTraceDeploymentChainMaxDepthSchemaDoesNotGateOnValuesTheHandlerClamps
// is the #5720 round-7 P1-5/P2-4 fix, and reverses the round-2 assertion that
// stood here.
//
// Round 2 pinned "minimum": 0, "maximum": 1000, and "default": 8 onto this
// schema to keep it in lockstep with the HTTP bound. Both halves were wrong
// for a clamping route. The default contradicted this route's own OpenAPI
// fragment, which states in the same commit that omitting max_depth resolves
// to the handler's default search limit of 25, not to 8: an MCP client
// applying advertised defaults would send max_depth: 8, resolving to
// boundedTraceEnrichmentLimit(8) = 80 -- a 3.2x wider read and a different
// result set for a caller that changed nothing. The minimum/maximum
// reintroduced the very rejection normalizeTraceDeploymentChainMaxDepth was
// written to avoid: a validating MCP client would 400 a max_depth: 5000 or
// max_depth: -1 that the server clamps and answers.
//
// So the schema must advertise none of the three, and the bounds must live in
// the description instead. This test is what stops any of them coming back.
func TestEcosystemTraceDeploymentChainMaxDepthSchemaDoesNotGateOnValuesTheHandlerClamps(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "trace_deployment_chain")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	maxDepth, ok := properties["max_depth"].(map[string]any)
	if !ok {
		t.Fatalf("trace_deployment_chain schema max_depth missing or wrong type: %#v", properties["max_depth"])
	}
	for _, key := range []string{"minimum", "maximum", "default"} {
		if value, present := maxDepth[key]; present {
			t.Fatalf(
				"trace_deployment_chain max_depth[%s] = %#v, want the key absent (the handler clamps out-of-range max_depth instead of rejecting it, and applies its own default search limit of 25; advertising %s makes a validating MCP client gate or rewrite a value the server answers)",
				key, value, key,
			)
		}
	}
	description, _ := maxDepth["description"].(string)
	for _, fragment := range []string{"clamped to 0-1000", "does not apply a default"} {
		if !strings.Contains(description, fragment) {
			t.Fatalf(
				"trace_deployment_chain max_depth[description] = %q, want it to document %q (the bound has to be stated somewhere now that the schema does not gate on it)",
				description, fragment,
			)
		}
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
