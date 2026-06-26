// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestCodebaseToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"find_code", "find_symbol", "inspect_code_inventory",
		"investigate_import_dependencies", "inspect_call_graph_metrics",
		"investigate_code_topic", "investigate_hardcoded_secrets",
		"get_code_relationship_story", "analyze_code_relationships",
		"find_dead_code", "investigate_dead_code", "find_dead_iac",
		"find_unmanaged_resources", "get_iac_management_status",
		"explain_iac_management_status", "propose_terraform_import_plan",
		"compose_replatforming_plan", "list_aws_runtime_drift_findings",
		"get_replatforming_rollups", "find_unmanaged_resource_owners",
		"calculate_cyclomatic_complexity", "find_most_complex_functions",
		"inspect_code_quality", "execute_cypher_query", "visualize_graph_query",
		"search_registry_bundles", "list_indexed_repositories",
		"get_repository_stats", "execute_language_query", "find_function_call_chain",
	} {
		_ = requireToolDefinition(t, name)
	}
}

func TestCodebaseFindCodeSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "find_code")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"query", "exact", "edit_distance", "repo_id", "limit", "scope"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("find_code schema missing %q", field)
		}
	}
}

func TestCodebaseFindSymbolSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "find_symbol")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"symbol", "match_mode", "repo_id", "language", "entity_type", "entity_types", "limit", "offset"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("find_symbol schema missing %q", field)
		}
	}
}

func TestCodebaseExecuteCypherQuerySchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "execute_cypher_query")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	if _, ok := properties["cypher_query"]; !ok {
		t.Fatalf("execute_cypher_query schema missing cypher_query")
	}
	if _, ok := properties["limit"]; !ok {
		t.Fatalf("execute_cypher_query schema missing limit")
	}
}

func TestCodebaseExecuteLanguageQuerySchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "execute_language_query")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"language", "entity_type", "query", "repo_id", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("execute_language_query schema missing %q", field)
		}
	}
}

func TestCodebaseFindDeadIacSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "find_dead_iac")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"repo_id", "repo_ids", "families", "include_ambiguous", "limit", "offset"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("find_dead_iac schema missing %q", field)
		}
	}
}

func TestCodebaseListIndexedRepositoriesSchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_indexed_repositories")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	if _, ok := properties["limit"]; !ok {
		t.Fatalf("list_indexed_repositories schema missing limit")
	}
	if _, ok := properties["offset"]; !ok {
		t.Fatalf("list_indexed_repositories schema missing offset")
	}
}

func TestCodebaseResolveRouteFindCode(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_code", map[string]any{
		"query":   "auth",
		"repo_id": "repo-1",
		"limit":   float64(10),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/search"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, _ := route.body.(map[string]any)
	if got, want := body["query"], "auth"; got != want {
		t.Fatalf("body[query] = %#v, want %#v", got, want)
	}
}

func TestCodebaseResolveRouteFindSymbol(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_symbol", map[string]any{
		"symbol":   "MyFunc",
		"repo_id":  "repo-1",
		"language": "go",
		"limit":    float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/symbols/search"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestCodebaseResolveRouteExecuteLanguageQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("execute_language_query", map[string]any{
		"language":    "go",
		"entity_type": "function",
		"limit":       float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/language-query"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestCodebaseResolveRouteSearchRegistryBundles(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("search_registry_bundles", map[string]any{
		"query":     "lodash",
		"ecosystem": "npm",
		"limit":     float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/bundles"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestCodebaseCalculateCyclomaticComplexityDescription(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "calculate_cyclomatic_complexity")
	if !strings.Contains(strings.ToLower(tool.Description), "cyclomatic") {
		t.Fatalf("description missing cyclomatic: %s", tool.Description)
	}
}

func TestCodebaseIacToolsSchemaCheck(t *testing.T) {
	t.Parallel()

	iacTools := map[string][]string{
		"find_unmanaged_resources":      {"scope_id", "account_id", "region", "finding_kinds", "limit", "offset"},
		"get_iac_management_status":     {"scope_id", "account_id", "region"},
		"explain_iac_management_status": {"scope_id", "region", "resource_id"},
		"compose_replatforming_plan":    {"scope_kind", "scope_id", "account_id", "region", "repo_id"},
	}
	for toolName, requiredFields := range iacTools {
		tool := requireToolDefinition(t, toolName)
		schema, _ := tool.InputSchema.(map[string]any)
		properties, _ := schema["properties"].(map[string]any)
		for _, field := range requiredFields {
			if _, ok := properties[field]; !ok {
				t.Fatalf("tool %s schema missing %q", toolName, field)
			}
		}
	}
}
