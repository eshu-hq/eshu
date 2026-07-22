// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func codebaseTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "find_code",
			Description: "Find code entities by case-sensitive name. Repository-selected calls use indexed graph lookup. Global substring calls use the content entity-name index and require at least three Unicode characters; set exact=true for complete names, including shorter names.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Case-sensitive entity name or literal substring",
					},
					"exact": map[string]any{
						"type":        "boolean",
						"description": "Require a complete case-sensitive entity-name match",
						"default":     false,
					},
					"edit_distance": map[string]any{
						"type":        "number",
						"description": "Deprecated compatibility field; ignored by case-sensitive name matching",
						"default":     2,
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional canonical repository identifier to scope the search",
					},
					"language": map[string]any{
						"type":        "string",
						"description": "Optional language filter applied before the bounded result limit",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return",
						"default":     10,
						"minimum":     1,
						"maximum":     200,
					},
					"scope": map[string]any{
						"type":        "string",
						"description": "Deprecated compatibility field; repo_id controls repository scope",
						"default":     "auto",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "find_symbol",
			Description: "Find exact or fuzzy symbol definitions with bounded, paged results and source handles.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"symbol": map[string]any{
						"type":        "string",
						"description": "Symbol name to locate",
					},
					"match_mode": map[string]any{
						"type":        "string",
						"description": "Symbol match mode",
						"enum":        []string{"exact", "fuzzy"},
						"default":     "exact",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional canonical repository identifier to scope the lookup",
					},
					"language": map[string]any{
						"type":        "string",
						"description": "Optional language filter",
					},
					"entity_type": map[string]any{
						"type":        "string",
						"description": "Optional single entity type filter",
					},
					"entity_types": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional entity type filters such as function, class, component, or module",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum definitions to return",
						"default":     25,
						"maximum":     200,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Zero-based result offset for paging",
						"default":     0,
						"maximum":     10000,
					},
				},
				"required": []string{"symbol"},
			},
		},
		structuralInventoryTool(),
		importDependencyTool(),
		callGraphMetricsTool(),
		routeToCallerTool(),
		codeTopicInvestigationTool(),
		securityInvestigationTool(),
		codeRelationshipStoryTool(),
		{
			Name:        "analyze_code_relationships",
			Description: "Analyze code relationships like 'who calls this function' or 'class hierarchy'. Relationship-story query types return per-row provenance blocks. Supported query types include: find_callers, find_callees, find_all_callers, find_all_callees, find_cross_repo_callers, find_cross_repo_callees, find_importers, find_cross_repo_importers, who_modifies, class_hierarchy, cross_repo_class_hierarchy, overrides, cross_repo_overrides, dead_code, call_chain, find_cross_repo_call_chain, module_deps, variable_scope, find_complexity, find_functions_by_argument, find_functions_by_decorator.",
			InputSchema: analyzeCodeRelationshipsSchema(),
		},
		{
			Name:        "find_dead_code",
			Description: "Find potentially unused functions (dead code) across the indexed codebase, optionally scoped to a canonical repository identifier and excluding functions with specific decorators.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"exclude_decorated_with": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "List of decorator names to exclude from dead code analysis",
						"default":     []any{},
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional canonical repository identifier",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum dead-code candidates to return",
						"default":     100,
					},
					"scope": map[string]any{
						"type":        "string",
						"description": "Search scope",
						"default":     "auto",
					},
				},
				"required": []string{},
			},
		},
		deadCodeInvestigationTool(),
		crossRepoDeadCodeTool(),
		{
			Name:        "find_dead_iac",
			Description: "Find unused or ambiguous Terraform modules, Helm charts, Kustomize paths, Ansible roles, and Docker Compose services across an explicit set of canonical repository identifiers.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional single canonical repository identifier",
					},
					"repo_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Canonical repository identifiers to include in the IaC reachability scope",
					},
					"families": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional IaC families to include: terraform, helm, kustomize, ansible, compose",
					},
					"include_ambiguous": map[string]any{
						"type":        "boolean",
						"description": "Whether to include dynamically referenced artifacts that need stronger evidence",
						"default":     false,
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum IaC cleanup findings to return",
						"default":     100,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Zero-based result offset for paging materialized or derived findings",
						"default":     0,
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "find_unmanaged_resources",
			Description: "Find AWS cloud resources whose active reducer drift facts show no Terraform config owner or only Terraform state ownership.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Exact AWS collector scope, for example aws:123456789012:us-east-1:lambda",
					},
					"account_id": map[string]any{
						"type":        "string",
						"description": "AWS account ID used to bound the active finding read",
					},
					"region": map[string]any{
						"type":        "string",
						"description": "Optional AWS region when account_id is supplied",
					},
					"finding_kinds": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, or ambiguous_cloud_resource",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum unmanaged resource findings to return",
						"default":     100,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Zero-based result offset for paging findings",
						"default":     0,
					},
				},
				"required": []string{},
			},
		},
		iacManagementStatusTool(),
		iacManagementExplanationTool(),
		terraformImportPlanTool(),
		composeReplatformingPlanTool(),
		awsRuntimeDriftFindingsTool(),
		terraformConfigStateDriftFindingsTool(),
		replatformingRollupsTool(),
		replatformingOwnershipTool(),
		{
			Name:        "calculate_cyclomatic_complexity",
			Description: "Calculate the cyclomatic complexity of a specific function to measure its complexity.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{
						"type":        "string",
						"description": "Exact entity identifier returned by an ambiguity response",
					},
					"function_name": map[string]any{
						"type":        "string",
						"description": "Name of the function to analyze when entity_id is unknown",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Optional file path containing the function",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional canonical repository identifier",
					},
					"scope": map[string]any{
						"type":        "string",
						"description": "Analysis scope",
						"default":     "auto",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "find_most_complex_functions",
			Description: "Find the most complex functions in the codebase based on cyclomatic complexity.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return",
						"default":     10,
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional canonical repository identifier",
					},
				},
				"required": []string{},
			},
		},
		codeQualityInspectionTool(),
		{
			Name:        "execute_cypher_query",
			Description: "Fallback tool to run a direct, read-only Cypher query against the code graph. Shared-key/all-scope callers only: the query text is caller-supplied and unbounded, so it cannot be intersected against a tenant grant. A scoped or browser-session token is rejected before this tool's request ever reaches the graph.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"cypher_query": map[string]any{
						"type":        "string",
						"description": "Read-only Cypher query to execute",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum rows to return when the query does not already include a LIMIT",
						"default":     100,
						"minimum":     1,
						"maximum":     1000,
					},
				},
				"required": []string{"cypher_query"},
			},
		},
		{
			Name:        "visualize_graph_query",
			Description: "Executes a read-only Cypher query and returns a bounded, renderable graph visualization packet (nodes and edges) projected from the graph nodes, relationships, and paths in the result. RETURN whole graph entities (for example RETURN n, r, m) rather than scalar properties; scalar columns are not renderable and yield an explicit unsupported packet. The query is bounded with an injected LIMIT and executed against a read-only session. Shared-key/all-scope callers only: the query text is caller-supplied and unbounded, so it cannot be intersected against a tenant grant. A scoped or browser-session token is rejected before this tool's request ever reaches the graph.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"cypher_query": map[string]any{
						"type":        "string",
						"description": "Read-only Cypher query whose returned graph nodes, relationships, and paths are projected into the visualization packet",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum result rows to project when the query does not already include a LIMIT",
						"default":     100,
						"minimum":     1,
						"maximum":     1000,
					},
				},
				"required": []string{"cypher_query"},
			},
		},
		{
			Name:        "search_registry_bundles",
			Description: "Search the pre-indexed package registry catalog (package bundles) by package name, namespace, or PURL. Supply a non-empty query or ecosystem scope; unscoped requests are rejected.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"minLength":   1,
						"pattern":     "\\S",
						"description": "Case-insensitive substring matched against package normalized name, namespace, or PURL. Required unless ecosystem is supplied; must contain a non-whitespace character.",
					},
					"ecosystem": map[string]any{
						"type":        "string",
						"minLength":   1,
						"pattern":     "\\S",
						"description": "Ecosystem scope (e.g. npm, pypi, maven, nuget) to bound the catalog read. Required unless query is supplied; must contain a non-whitespace character.",
					},
					"unique_only": map[string]any{
						"type":        "boolean",
						"description": "Return only distinct package bundles",
						"default":     false,
					},
					"limit": map[string]any{"type": "integer", "description": "Maximum bundles to return", "default": 50, "minimum": 1, "maximum": 200},
				},
				// A non-empty query or ecosystem scope is required, but the
				// constraint lives in the descriptions above and in handler
				// validation: exported MCP tool schemas must not use top-level
				// anyOf/oneOf/allOf (OpenAI-restricted keywords).
				"required": []string{},
			},
		},
		{
			Name:        "list_indexed_repositories",
			Description: "List a bounded page of indexed repositories. For an exact indexed-repository count, use the authoritative total field, which is independent of page size; count is only the number of rows in the current page. Cite list_indexed_repositories.total as the evidence source.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit":  map[string]any{"type": "integer", "description": "Maximum repositories to return", "default": 100, "minimum": 1, "maximum": 500},
					"offset": map[string]any{"type": "integer", "description": "Zero-based result offset for paging", "default": 0, "minimum": 0, "maximum": 10000},
				},
				"required": []string{},
			},
		},
		{
			Name:        "get_repository_stats",
			Description: "Get bounded read-model statistics about an indexed repository, scoped by repository selector.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector: canonical ID, name, repo slug, or indexed path",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "execute_language_query",
			Description: "Execute a language-specific query to find code entities (functions, classes, structs, etc.) filtered by programming language. Supports 15 languages: c, cpp, csharp, dart, go, haskell, java, javascript, perl, python, ruby, rust, scala, swift, typescript.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"language": map[string]any{
						"type":        "string",
						"description": "Programming language to filter by (e.g., python, go, rust)",
					},
					"entity_type": map[string]any{
						"type":        "string",
						"description": "Type of code entity to search for",
						"enum":        []string{"repository", "directory", "file", "module", "function", "class", "struct", "enum", "union", "macro", "variable"},
					},
					"query": map[string]any{
						"type":        "string",
						"description": "Optional name pattern to filter results",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional canonical repository identifier to scope the search",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return",
						"default":     50,
					},
				},
				"required": []string{"language", "entity_type"},
			},
		},
		{
			Name:        "find_function_call_chain",
			Description: "Find the transitive call chain between two functions by following CALLS edges in the code graph. Returns shortest paths up to a configurable depth.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"start": map[string]any{
						"type":        "string",
						"description": "Optional starting function name; use start_entity_id for an exact code graph entity selector",
					},
					"end": map[string]any{
						"type":        "string",
						"description": "Optional ending function name; use end_entity_id for an exact code graph entity selector",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional canonical repository identifier used to scope name-based call-chain resolution",
					},
					"cross_repo": map[string]any{
						"type":        "boolean",
						"description": "Explicit opt-in for bounded cross-repository call-chain traversal",
						"default":     false,
					},
					"start_repo_id": map[string]any{
						"type":        "string",
						"description": "Optional starting repository selector for cross-repo call-chain resolution",
					},
					"end_repo_id": map[string]any{
						"type":        "string",
						"description": "Optional ending repository selector for cross-repo call-chain resolution",
					},
					"start_entity_id": map[string]any{
						"type":        "string",
						"description": "Optional exact starting code entity ID; avoids ambiguous name resolution when provided",
					},
					"end_entity_id": map[string]any{
						"type":        "string",
						"description": "Optional exact ending code entity ID; avoids ambiguous name resolution when provided",
					},
					"max_depth": map[string]any{
						"type":        "integer",
						"description": "Maximum chain depth (1-10)",
						"default":     5,
					},
				},
				"required": []string{},
			},
		},
	}
}
