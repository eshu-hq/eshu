// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func entityWorkloadContextTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "resolve_entity",
			Description: "Resolve an exact case-sensitive entity name. Global calls require a supported type and use the current content entity index, except canonical content-entity IDs and workload resolution. Repository, directory, and file types require repo_id.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Exact case-sensitive entity name or canonical content-entity ID",
					},
					"type": map[string]any{
						"type":        "string",
						"description": "Single entity type. Required for global name resolution except canonical content-entity IDs; workload keeps its authoritative graph path.",
					},
					"types": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Deprecated compatibility alias; only the first value is used when type is absent",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector to scope the search: canonical ID, name, repo slug, or indexed path",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results",
						"default":     10,
						"minimum":     1,
						"maximum":     100,
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "get_entity_context",
			Description: "Get context for a canonical entity ID.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{
						"type":        "string",
						"description": "Canonical entity identifier",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name:        "get_workload_context",
			Description: "Get logical or environment-specific context for a canonical workload identifier.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Canonical workload identifier",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
				},
				"required": []string{"workload_id"},
			},
		},
		{
			Name:        "get_workload_story",
			Description: "Get a structured story for a workload.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Canonical workload identifier",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
				},
				"required": []string{"workload_id"},
			},
		},
	}
}
