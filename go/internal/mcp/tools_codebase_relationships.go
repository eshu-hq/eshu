package mcp

func analyzeCodeRelationshipsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query_type": map[string]any{
				"type":        "string",
				"description": "Type of relationship analysis to perform",
				"enum": []string{
					"find_callers",
					"find_callees",
					"find_all_callers",
					"find_all_callees",
					"find_importers",
					"who_modifies",
					"class_hierarchy",
					"overrides",
					"dead_code",
					"call_chain",
					"module_deps",
					"variable_scope",
					"find_complexity",
					"find_functions_by_argument",
					"find_functions_by_decorator",
				},
			},
			"target": map[string]any{
				"type":        "string",
				"description": "Target entity to analyze. Optional for repo-scoped overrides queries.",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Optional context for the analysis",
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
			"max_depth": map[string]any{
				"type":        "integer",
				"description": "Maximum transitive CALLS depth for all-callers or all-callees queries",
				"default":     5,
				"minimum":     1,
				"maximum":     10,
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum relationships or candidates to return",
				"default":     25,
				"minimum":     1,
				"maximum":     200,
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Relationship offset for paged direct relationship queries",
				"default":     0,
				"minimum":     0,
			},
		},
		"required": []string{"query_type"},
		"anyOf": []map[string]any{
			{
				"required": []string{"query_type", "target"},
			},
			{
				"required": []string{"query_type", "repo_id"},
				"properties": map[string]any{
					"query_type": map[string]any{
						"enum": []string{"overrides"},
					},
				},
			},
		},
	}
}
