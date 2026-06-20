package mcp

// relationshipTypeEnum lists the relationship types the bounded relationship
// story query path can follow.
var relationshipTypeEnum = []string{"CALLS", "IMPORTS", "REFERENCES", "INHERITS", "OVERRIDES", "TAINT_FLOWS_TO"}

// codeRelationshipStoryTool defines the get_code_relationship_story MCP tool: a
// bounded, budget-aware relationship story for one resolved code symbol.
func codeRelationshipStoryTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_code_relationship_story",
		Description: "Get a bounded relationship story for one resolved code symbol, including ambiguity candidates, direct callers/callees/imports, per-row provenance blocks, optional transitive CALLS traversal, an optional token_budget that trims to fit and reports what was cut, truncation, and source handles. Provide target or entity_id.",
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
					"enum":        relationshipTypeEnum,
					"default":     "CALLS",
				},
				"relationship_types": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string", "enum": relationshipTypeEnum},
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
