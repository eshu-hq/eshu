// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeinteltools

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// Tools returns the code-intelligence tool definitions owned by this
// package: the structural inventory, call-graph metrics, route-to-caller,
// and code-topic investigation tools. The find_code, find_symbol,
// execute_language_query, and find_function_call_chain definitions stay in
// the root codebase group until their own leaf moves them.
func Tools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		structuralInventoryTool(),
		callGraphMetricsTool(),
		routeToCallerTool(),
		codeTopicInvestigationTool(),
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
