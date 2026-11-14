// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func entityWorkloadContextTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "resolve_entity",
			Description: "Resolve an exact case-sensitive entity name. Omitting type (and repo_id) searches every kind by exact name (repository, workload, package, and content entity) and returns one ranked list where each match carries its kind, canonical id, and next selector; names under 3 characters are refused. A supplied type narrows the lookup to that index. Repository, directory, and file types require repo_id.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Exact case-sensitive entity name or canonical content-entity ID; the route sends it as name",
					},
					"type": map[string]any{
						"type":        "string",
						"description": "Single entity type. Optional: omit it (with no repo_id) to search every kind by exact name; workload keeps its authoritative graph path.",
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
					"evidence_detail": map[string]any{
						"type":        "string",
						"enum":        []string{"full", "handles"},
						"description": "Row detail for deployment evidence artifacts and API surface endpoints. handles (the default for this tool) returns identity rows only (id, relationship_type, resolved_id for artifacts; id, path, methods for endpoints), drops the evidence_index and the content-derived evidence lists (deployment_artifacts, delivery_paths, topology_story and similar), keeps every count, and names each reduced family with its total in truth.omissions; repeat the call with full for the rows. full returns every row in full and can exceed the response budget on a populated service.",
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
