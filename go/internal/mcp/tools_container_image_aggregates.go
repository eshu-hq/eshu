// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// containerImageIdentityAggregateTools returns the cheap-summary aggregate
// tools shipped alongside the existing list_container_image_identities list
// tool. They give callers an O(1) answer to ecosystem-level questions like
// "how many images resolved by exact digest vs tag?" without paging through
// the list endpoint.
func containerImageIdentityAggregateTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "count_container_image_identities",
			Description: "Return reducer-owned container image identity totals for one optional scope without paging through individual identity rows. Provides total identities and rollups by outcome (exact_digest / tag_resolved) and identity_strength. Use before list_container_image_identities when the question is a count, not a list.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"digest": map[string]any{
						"type":        "string",
						"description": "Optional image digest (such as `sha256:...`) to scope the totals.",
					},
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Optional original image reference observed in source or runtime evidence to scope the totals.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional OCI repository identity (such as `oci-registry://registry.example/team/api`) to scope the totals.",
					},
					"source_repository_id": map[string]any{
						"type":        "string",
						"description": "Optional source repository id or selector for bridge-scoped totals; this is not an OCI image repository identity.",
					},
					"outcome": map[string]any{
						"type":        "string",
						"description": "Optional reducer identity outcome filter applied before counting.",
						"enum":        []string{"exact_digest", "tag_resolved"},
					},
				},
			},
		},
		{
			Name:        "get_container_image_identity_inventory",
			Description: "Return a paginated grouped count of reducer-owned container image identities along one dimension (outcome, identity_strength, repository_id). Replaces the page-and-iterate caller pattern for ecosystem-level inventory questions.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group_by": map[string]any{
						"type":        "string",
						"description": "Grouping dimension. outcome (default) groups by reducer outcome; identity_strength groups by reducer identity strength; repository_id groups by OCI repository.",
						"enum":        []string{"outcome", "identity_strength", "repository_id"},
						"default":     "outcome",
					},
					"digest": map[string]any{
						"type":        "string",
						"description": "Optional image digest to scope the inventory.",
					},
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Optional original image reference to scope the inventory.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional OCI repository identity to scope the inventory.",
					},
					"source_repository_id": map[string]any{
						"type":        "string",
						"description": "Optional source repository id or selector for bridge-scoped inventory; this is not an OCI image repository identity.",
					},
					"outcome": map[string]any{
						"type":        "string",
						"description": "Optional reducer identity outcome filter applied before grouping.",
						"enum":        []string{"exact_digest", "tag_resolved"},
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
