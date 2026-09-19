// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeinteltools

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// Tools returns the code-intelligence tool definitions owned by this
// package: the find_code and find_symbol search tools, the structural
// inventory, call-graph metrics, route-to-caller, and code-topic
// investigation tools, and the language-query and call-chain tools. The
// root codebase group splices them at their long-standing positions, so
// this order only fixes the family sequence, never the client-visible
// registration order.
func Tools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		findCodeTool(),
		findSymbolTool(),
		structuralInventoryTool(),
		callGraphMetricsTool(),
		routeToCallerTool(),
		codeTopicInvestigationTool(),
		executeLanguageQueryTool(),
		findFunctionCallChainTool(),
	}
}

func findCodeTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
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
	}
}

func findSymbolTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "find_symbol",
		Description: "Find exact or fuzzy symbol definitions with bounded, paged results and source handles. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected.",
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
	}
}

func structuralInventoryTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "inspect_code_inventory",
		Description: "Inspect bounded structural code inventory such as functions, classes, top-level file elements, dataclasses, documented functions, decorated methods, classes with a method, and super calls. Provide at least one scope filter: repo_id, file_path, language, entity_kind, or symbol. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional canonical repository identifier to scope the inventory",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional language filter",
				},
				"inventory_kind": map[string]any{
					"type":        "string",
					"description": "Structural inventory filter",
					"enum":        []string{"entity", "top_level", "dataclass", "documented", "documented_function", "decorated", "class_with_method", "super_call", "function_count_by_file"},
					"default":     "entity",
				},
				"entity_kind": map[string]any{
					"type":        "string",
					"description": "Optional entity kind such as function, class, module, variable, component, type_alias, or sql_function. Must be function for function_count_by_file inventory.",
				},
				"file_path": map[string]any{
					"type":        "string",
					"description": "Optional repo-relative file path anchor",
				},
				"symbol": map[string]any{
					"type":        "string",
					"description": "Optional exact entity name filter",
				},
				"decorator": map[string]any{
					"type":        "string",
					"description": "Optional decorator filter for decorated inventory",
				},
				"method_name": map[string]any{
					"type":        "string",
					"description": "Method name required for class_with_method inventory",
				},
				"class_name": map[string]any{
					"type":        "string",
					"description": "Optional class or implementation context filter",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum inventory rows to return",
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
		},
	}
}

func callGraphMetricsTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "inspect_call_graph_metrics",
		Description: "Inspect bounded call-graph metrics for recursive functions and highly connected hub functions within one repository. Requires repo_id and returns source handles, truncation, truth metadata, hub call-degree counts, and recursion evidence. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"repo_id"},
			"properties": map[string]any{
				"metric_type": map[string]any{
					"type":        "string",
					"description": "Call-graph metric to inspect",
					"enum":        []string{"hub_functions", "recursive_functions"},
					"default":     "hub_functions",
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Canonical repository identifier",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional language filter",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum function rows to return",
					"default":     25,
					"minimum":     1,
					"maximum":     200,
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Zero-based result offset for paging",
					"default":     0,
					"minimum":     0,
					"maximum":     10000,
				},
			},
		},
	}
}

func routeToCallerTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "trace_route_callers",
		Description: "Resolve an exact framework route handler from HANDLES_ROUTE truth, then return bounded CALLS callers/callees and impacted workloads/repositories. Dynamic or unsupported routes are reported without guessing handlers.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Canonical repository identifier to scope route resolution",
				},
				"service_id": map[string]any{
					"type":        "string",
					"description": "Optional exact service/workload identifier to scope route resolution when repo_id is not supplied",
				},
				"service_name": map[string]any{
					"type":        "string",
					"description": "Optional exact service/workload name to scope route resolution when repo_id is not supplied",
				},
				"method": map[string]any{
					"type":        "string",
					"description": "HTTP method to match exactly against HANDLES_ROUTE.http_method; omit only when the caller intentionally wants method ambiguity surfaced",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Exact route path as projected on the Endpoint node",
				},
				"max_depth": map[string]any{
					"type":        "integer",
					"description": "Maximum CALLS traversal depth",
					"default":     2,
					"minimum":     1,
					"maximum":     5,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum combined caller/callee rows to return",
					"default":     25,
					"minimum":     1,
					"maximum":     100,
				},
			},
			"required": []string{"path"},
		},
	}
}

func codeTopicInvestigationTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "investigate_code_topic",
		Description: "Investigate a broad code topic or behavior with ranked files, symbols, coverage metadata, truncation, and exact next-call handles for source reads and relationship stories. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"topic": map[string]any{
					"type":        "string",
					"description": "Natural-language topic or behavior to investigate, for example repo sync authentication or workspace locking",
				},
				"intent": map[string]any{
					"type":        "string",
					"description": "Optional intent such as explain_flow, find_owners, or debug_issue",
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional canonical repository identifier to scope the investigation",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional language filter",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum evidence groups to return",
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
			"required": []string{"topic"},
		},
	}
}

func executeLanguageQueryTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "execute_language_query",
		Description: "Execute a language-specific query to find code entities (functions, classes, structs, etc.) filtered by programming language. Supports 15 languages: c, cpp, csharp, dart, go, haskell, java, javascript, perl, python, ruby, rust, scala, swift, typescript. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected.",
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
					"description": "Maximum number of results to return (default 50, maximum 200)",
					"default":     50,
					"minimum":     1,
					"maximum":     200,
				},
			},
			"required": []string{"language", "entity_type"},
		},
	}
}

func findFunctionCallChainTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "find_function_call_chain",
		Description: "Find the transitive call chain between two functions by following CALLS edges in the code graph. Returns shortest paths up to a configurable depth. Scoped tokens receive only chains whose every hop is in a granted repository; an ungranted repository selector is rejected.",
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
	}
}
