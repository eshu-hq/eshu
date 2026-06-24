// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func serviceCatalogTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_service_catalog_correlations",
			Description: "List reducer-owned service catalog ownership and drift correlations by entity, repository, service, workload, owner, or scope, including repository-local descriptor evidence and external confirmation state.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Optional ingestion scope ID for a service catalog generation.",
					},
					"provider": map[string]any{
						"type":        "string",
						"description": "Catalog provider such as backstage, opslevel, or cortex.",
					},
					"entity_ref": map[string]any{
						"type":        "string",
						"description": "Provider-native catalog entity reference.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Repository selector to anchor catalog correlation lookup; accepts canonical ID, name, slug, indexed path, or local path.",
					},
					"service_id": map[string]any{
						"type":        "string",
						"description": "Service ID to anchor catalog correlation lookup.",
					},
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Workload ID to anchor catalog correlation lookup.",
					},
					"owner_ref": map[string]any{
						"type":        "string",
						"description": "Provider-native owner reference.",
					},
					"outcome": map[string]any{
						"type":        "string",
						"description": "Optional reducer outcome filter.",
						"enum":        []string{"exact", "derived", "ambiguous", "unresolved", "stale", "rejected"},
					},
					"drift_status": map[string]any{
						"type":        "string",
						"description": "Optional drift status filter.",
					},
					"after_correlation_id": map[string]any{
						"type":        "string",
						"description": "Correlation ID from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum correlation rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
	}
}
