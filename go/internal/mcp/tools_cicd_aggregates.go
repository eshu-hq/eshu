// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// cicdRunCorrelationAggregateTools returns the cheap-summary aggregate tools
// shipped alongside the existing list_ci_cd_run_correlations list tool. They
// give callers an O(1) answer to ecosystem-level questions like "how many
// runs ended in each outcome per environment?" without paging through the
// list endpoint.
func cicdRunCorrelationAggregateTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "count_ci_cd_run_correlations",
			Description: "Return reducer-owned CI/CD run correlation totals for one optional scope without paging through individual correlation rows. Provides total correlations and rollups by outcome, environment, and provider. Use before list_ci_cd_run_correlations when the question is a count, not a list.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Optional ingestion scope identifier to scope the totals.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional repository identifier to scope the totals.",
					},
					"commit_sha": map[string]any{
						"type":        "string",
						"description": "Optional commit SHA to scope the totals.",
					},
					"provider": map[string]any{
						"type":        "string",
						"description": "Optional CI provider identifier (such as `github_actions`, `gitlab`, `circleci`) to scope the totals.",
					},
					"artifact_digest": map[string]any{
						"type":        "string",
						"description": "Optional artifact digest to scope the totals.",
					},
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Optional image reference to scope the totals.",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional deployment environment filter applied before counting.",
					},
					"outcome": map[string]any{
						"type":        "string",
						"description": "Optional reducer outcome filter applied before counting.",
						"enum":        []string{"exact", "derived", "ambiguous", "unresolved", "rejected"},
					},
				},
			},
		},
		{
			Name:        "get_ci_cd_run_correlation_inventory",
			Description: "Return a paginated grouped count of reducer-owned CI/CD run correlations along one dimension (outcome, environment, repository_id, provider). Replaces the page-and-iterate caller pattern for ecosystem-level inventory questions.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group_by": map[string]any{
						"type":        "string",
						"description": "Grouping dimension. outcome (default) groups by reducer outcome; environment groups by deployment environment; repository_id groups by repository; provider groups by CI provider.",
						"enum":        []string{"outcome", "environment", "repository_id", "provider"},
						"default":     "outcome",
					},
					"scope_id":        map[string]any{"type": "string", "description": "Optional ingestion scope identifier to scope the inventory."},
					"repository_id":   map[string]any{"type": "string", "description": "Optional repository identifier."},
					"commit_sha":      map[string]any{"type": "string", "description": "Optional commit SHA."},
					"provider":        map[string]any{"type": "string", "description": "Optional CI provider identifier."},
					"artifact_digest": map[string]any{"type": "string", "description": "Optional artifact digest."},
					"image_ref":       map[string]any{"type": "string", "description": "Optional image reference."},
					"environment":     map[string]any{"type": "string", "description": "Optional environment filter applied before grouping."},
					"outcome": map[string]any{
						"type":        "string",
						"description": "Optional reducer outcome filter applied before grouping.",
						"enum":        []string{"exact", "derived", "ambiguous", "unresolved", "rejected"},
					},
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
