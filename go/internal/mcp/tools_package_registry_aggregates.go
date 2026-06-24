// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// packageRegistryAggregateTools returns the cheap-summary aggregate tools
// shipped alongside the existing list_package_registry_packages list tool.
// They give callers an O(1) answer to ecosystem-level questions like "how
// many packages per ecosystem?" without paging through the list endpoint.
func packageRegistryAggregateTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "count_package_registry_packages",
			Description: "Return graph-backed (:Package) totals for one optional scope without paging through individual packages. Provides total packages and a per-ecosystem rollup. Use before list_package_registry_packages when the question is a count, not a list.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ecosystem": map[string]any{
						"type":        "string",
						"description": "Optional ecosystem identifier (such as `npm`, `pypi`, `maven`, or `hex`) to scope the totals.",
					},
					"registry": map[string]any{
						"type":        "string",
						"description": "Optional registry identifier to scope the totals.",
					},
					"namespace": map[string]any{
						"type":        "string",
						"description": "Optional namespace / group to scope the totals.",
					},
					"package_manager": map[string]any{
						"type":        "string",
						"description": "Optional package manager identifier (such as `npm`, `pip`, `mvn`) to scope the totals.",
					},
					"visibility": map[string]any{
						"type":        "string",
						"description": "Optional visibility filter applied before counting.",
						"enum":        []string{"public", "private", "unknown"},
					},
				},
			},
		},
		{
			Name:        "get_package_registry_package_inventory",
			Description: "Return a paginated grouped count of graph-backed (:Package) nodes along one dimension (ecosystem, registry, namespace, package_manager, visibility). Replaces the page-and-iterate caller pattern for ecosystem-level inventory questions.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group_by": map[string]any{
						"type":        "string",
						"description": "Grouping dimension. ecosystem (default) groups by ecosystem; registry groups by registry host; namespace groups by namespace; package_manager groups by manager; visibility groups by visibility tier.",
						"enum":        []string{"ecosystem", "registry", "namespace", "package_manager", "visibility"},
						"default":     "ecosystem",
					},
					"ecosystem":       map[string]any{"type": "string", "description": "Optional ecosystem identifier to scope the inventory."},
					"registry":        map[string]any{"type": "string", "description": "Optional registry identifier."},
					"namespace":       map[string]any{"type": "string", "description": "Optional namespace / group."},
					"package_manager": map[string]any{"type": "string", "description": "Optional package manager identifier."},
					"visibility": map[string]any{
						"type":        "string",
						"description": "Optional visibility filter applied before grouping.",
						"enum":        []string{"public", "private", "unknown"},
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
