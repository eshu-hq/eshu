// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func repositoryLanguageTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "count_repositories_by_language",
			Description: "Count indexed repositories and files for one language family without fetching per-repository coverage.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"language": map[string]any{
						"type":        "string",
						"description": "Language family to count, such as typescript, javascript, terraform, go, python, java, or php.",
					},
				},
				"required": []string{"language"},
			},
		},
		{
			Name:        "list_repositories_by_language",
			Description: "List a bounded page of indexed repositories that contain files for one language family.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"language": map[string]any{
						"type":        "string",
						"description": "Language family to list, such as typescript, javascript, terraform, go, python, java, or php.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum repositories to return.",
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
				"required": []string{"language"},
			},
		},
		{
			Name:        "get_repository_language_inventory",
			Description: "Return aggregate repository and file counts for indexed language buckets.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum language buckets to return.",
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
				"required": []string{},
			},
		},
	}
}
