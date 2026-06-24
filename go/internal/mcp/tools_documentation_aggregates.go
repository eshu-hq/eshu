// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// documentationFindingAggregateTools returns the cheap-summary aggregate
// tools shipped alongside the existing list_documentation_findings tool.
// They give callers an O(1) answer to ecosystem-level questions like
// "how many findings per status?" without paging through the list endpoint.
//
// The aggregate inherits the same permission predicates the list endpoint
// uses (`viewer_can_read_source`, `source_acl_evaluated`,
// `permission_decision`), so a caller cannot use these tools to enumerate
// counts from documents they could not read directly.
func documentationFindingAggregateTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "count_documentation_findings",
			Description: "Return durable documentation finding totals for one optional scope without paging through individual findings. Provides total findings and rollups by status, truth_level, and freshness_state. Inherits the same per-document read permissions as list_documentation_findings.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id":        map[string]any{"type": "string", "description": "Optional ingestion scope identifier to scope the totals."},
					"finding_type":    map[string]any{"type": "string", "description": "Optional finding_type filter."},
					"source_id":       map[string]any{"type": "string", "description": "Optional upstream source identifier (Confluence space, Notion workspace, etc.)."},
					"document_id":     map[string]any{"type": "string", "description": "Optional document identifier."},
					"status":          map[string]any{"type": "string", "description": "Optional reducer status filter."},
					"truth_level":     map[string]any{"type": "string", "description": "Optional truth_level filter."},
					"freshness_state": map[string]any{"type": "string", "description": "Optional freshness_state filter."},
				},
			},
		},
		{
			Name:        "get_documentation_finding_inventory",
			Description: "Return a paginated grouped count of durable documentation findings along one dimension (status, truth_level, freshness_state, finding_type, source_id). Replaces the page-and-iterate caller pattern for ecosystem-level inventory questions. Inherits the same per-document read permissions as list_documentation_findings.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group_by": map[string]any{
						"type":        "string",
						"description": "Grouping dimension. status (default) groups by reducer status; truth_level groups by truth_level; freshness_state groups by freshness_state; finding_type groups by finding_type; source_id groups by upstream source.",
						"enum":        []string{"status", "truth_level", "freshness_state", "finding_type", "source_id"},
						"default":     "status",
					},
					"scope_id":        map[string]any{"type": "string", "description": "Optional ingestion scope identifier."},
					"finding_type":    map[string]any{"type": "string", "description": "Optional finding_type filter."},
					"source_id":       map[string]any{"type": "string", "description": "Optional upstream source identifier."},
					"document_id":     map[string]any{"type": "string", "description": "Optional document identifier."},
					"status":          map[string]any{"type": "string", "description": "Optional reducer status filter applied before grouping."},
					"truth_level":     map[string]any{"type": "string", "description": "Optional truth_level filter applied before grouping."},
					"freshness_state": map[string]any{"type": "string", "description": "Optional freshness_state filter applied before grouping."},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum buckets to return per page.",
						"default":     100,
						"minimum":     1,
						"maximum":     500,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Zero-based result offset for paging.",
						"default":     0,
						"minimum":     0,
						"maximum":     10000,
					},
				},
			},
		},
	}
}
