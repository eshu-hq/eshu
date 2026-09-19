// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	codeinteltools "github.com/eshu-hq/eshu/go/internal/mcp/code/intel"
)

func codebaseTools() []ToolDefinition {
	// intel holds the eight code-intelligence definitions owned by the
	// code/intel package, spliced into this block at their long-standing
	// positions around the import-dependency and security helpers. The
	// interleaved neighbors rule out a whole-slice append, so this guard
	// makes an arity change fail fast here instead of silently dropping a
	// ninth definition or panicking on an index below.
	intel := codeinteltools.Tools()
	if len(intel) != 8 {
		panic("codeinteltools.Tools must return exactly the eight spliced definitions")
	}
	tools := []ToolDefinition{
		intel[0],
		intel[1],
		intel[2],
		importDependencyTool(),
		intel[3],
		intel[4],
		intel[5],
		securityInvestigationTool(),
	}
	tools = append(tools, codeRelationshipTools()...)
	// The three dead-code definitions owned by the code/dead package splice
	// in at this position to preserve the long-standing registration order.
	// Appending the whole family slice keeps a future arity change loud at
	// the order test instead of panicking on an index.
	tools = append(tools, deadCodeTools()...)
	tools = append(tools, []ToolDefinition{
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
			Description: "Find AWS cloud resources from active reducer drift facts. The default page is limited to resources with no Terraform config owner or only Terraform state ownership; explicitly select image_version_drift or value_comparison_inconclusive to inspect managed value drift or degraded comparison evidence.",
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
						"description": iacFindingKindsDescription,
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
	}...)
	// The three complexity/quality definitions owned by the code/quality
	// package splice in at this position to preserve the long-standing
	// registration order. Appending the whole family slice keeps a future
	// arity change loud at the order test instead of panicking here.
	tools = append(tools, codeQualityTools()...)
	tools = append(tools, []ToolDefinition{
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
			Description: "Search the pre-indexed package registry catalog (package bundles) by package name, namespace, or PURL. Supply a non-empty query or ecosystem scope; unscoped requests are rejected. Scoped (personal-token) callers are refused with a 403: the bundle catalog is a whole-graph read with no per-repository binding for the caller's grant (#5167 Group B). Use the shared ESHU_API_KEY for this tool.",
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
		intel[6],
		intel[7],
	}...)
	return tools
}
