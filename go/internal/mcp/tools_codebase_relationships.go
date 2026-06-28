// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
					"find_cross_repo_callers",
					"find_cross_repo_callees",
					"find_importers",
					"find_cross_repo_importers",
					"who_modifies",
					"class_hierarchy",
					"cross_repo_class_hierarchy",
					"overrides",
					"cross_repo_overrides",
					"dead_code",
					"call_chain",
					"find_cross_repo_call_chain",
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
			"cross_repo": map[string]any{
				"type":        "boolean",
				"description": "Explicit opt-in for bounded cross-repository relationship traversal; false preserves repo-scoped behavior",
				"default":     false,
			},
			"start_repo_id": map[string]any{
				"type":        "string",
				"description": "Optional starting repository selector for cross-repo call_chain queries",
			},
			"end_repo_id": map[string]any{
				"type":        "string",
				"description": "Optional ending repository selector for cross-repo call_chain queries",
			},
			"start_entity_id": map[string]any{
				"type":        "string",
				"description": "Optional exact starting code entity ID for call_chain queries; avoids ambiguous name resolution when provided",
			},
			"end_entity_id": map[string]any{
				"type":        "string",
				"description": "Optional exact ending code entity ID for call_chain queries; avoids ambiguous name resolution when provided",
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
			"relationship_types": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "enum": relationshipTypeEnum},
				"description": "Optional additive multi-type filter for direct caller/callee/importer queries; merges each type's bounded results.",
			},
			"token_budget": map[string]any{
				"type":        "integer",
				"description": "Optional cap on the estimated response token cost; trims rows to fit and reports what was cut with guidance to narrow.",
				"minimum":     0,
			},
			"min_confidence": map[string]any{
				"type":        "number",
				"description": "Optional confidence floor from 0 through 1 for relationship-story query types. Omitted preserves low-confidence and missing-confidence rows; positive values keep only returned rows with numeric confidence at or above the floor.",
				"minimum":     float64(0),
				"maximum":     float64(1),
			},
		},
		"required": []string{"query_type"},
	}
}
