// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshiptools

import "github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"

// relationshipTypeEnum lists the relationship types the bounded relationship
// story query path can follow.
func relationshipTypeEnum() []string {
	return []string{"CALLS", "IMPORTS", "REFERENCES", "INHERITS", "OVERRIDES", "TAINT_FLOWS_TO"}
}

// codeRelationshipStoryTool defines the get_code_relationship_story MCP tool: a
// bounded, budget-aware relationship story for one resolved code symbol.
func codeRelationshipStoryTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "get_code_relationship_story",
		Description: "Get a bounded relationship story for one resolved code symbol, including ambiguity candidates, direct callers/callees/imports, per-row provenance blocks, optional transitive CALLS traversal, an optional token_budget that trims to fit and reports what was cut, truncation, and source handles. Provide target or entity_id. Scoped tokens receive only granted repositories, in the relationships, the class-hierarchy enrichment and the ambiguity candidate list alike; an ungranted repository selector is rejected.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target": map[string]any{
					"type":        "string",
					"description": "Symbol name to resolve when entity_id is not supplied.",
				},
				"entity_id": map[string]any{
					"type":        "string",
					"description": "Exact entity identifier to anchor the relationship query.",
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional canonical repository identifier to scope name resolution.",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional language filter for name resolution.",
				},
				"relationship_type": map[string]any{
					"type":        "string",
					"description": "Relationship type to follow.",
					"enum":        relationshipTypeEnum(),
					"default":     "CALLS",
				},
				"relationship_types": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string", "enum": relationshipTypeEnum()},
					"description": "Optional additive multi-type filter; supersedes relationship_type and merges each type's bounded results. Not supported with include_transitive, class_hierarchy, or overrides.",
				},
				"direction": map[string]any{
					"type":        "string",
					"description": "Relationship direction from the target entity.",
					"enum":        []string{"incoming", "outgoing", "both"},
					"default":     "both",
				},
				"token_budget": map[string]any{
					"type":        "integer",
					"description": "Optional cap on the estimated response token cost. Applied after limit; trims rows to fit and reports what was cut with guidance to narrow.",
					"minimum":     0,
				},
				"min_confidence": map[string]any{
					"type":        "number",
					"description": "Optional confidence floor from 0 through 1. Omitted preserves low-confidence and missing-confidence rows; positive values keep only returned rows with numeric confidence at or above the floor.",
					"minimum":     float64(0),
					"maximum":     float64(1),
				},
				"include_transitive": map[string]any{
					"type":        "boolean",
					"description": "When true, follow CALLS edges with bounded breadth-first traversal.",
					"default":     false,
				},
				"max_depth": map[string]any{
					"type":        "integer",
					"description": "Maximum transitive CALLS depth.",
					"default":     5,
					"maximum":     10,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum relationship rows or ambiguity candidates to return.",
					"default":     25,
					"maximum":     200,
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Zero-based result offset for direct relationship paging.",
					"default":     0,
					"maximum":     10000,
				},
			},
		},
	}
}

// CodeTools returns fresh definitions for the relationship-story and
// relationship-analysis tools in their canonical registry order.
func CodeTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		codeRelationshipStoryTool(),
		{
			Name:        "analyze_code_relationships",
			Description: "Analyze code relationships like 'who calls this function' or 'class hierarchy'. Relationship-story query types return per-row provenance blocks. Supported query types include: find_callers, find_callees, find_all_callers, find_all_callees, find_cross_repo_callers, find_cross_repo_callees, find_importers, find_cross_repo_importers, who_modifies, class_hierarchy, cross_repo_class_hierarchy, overrides, cross_repo_overrides, dead_code, call_chain, find_cross_repo_call_chain, module_deps, variable_scope, find_complexity, find_functions_by_argument, find_functions_by_decorator. The relationship-story and call-chain query types return only granted repositories for a scoped token and reject an ungranted repository selector.",
			InputSchema: AnalyzeCodeRelationshipsSchema(),
		},
	}
}
