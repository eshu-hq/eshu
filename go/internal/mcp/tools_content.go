package mcp

func contentTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "get_file_content",
			Description: "Return source for a repo-relative file using a repository selector plus relative path.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Repository selector: canonical ID, name, repo slug, or indexed path",
					},
					"relative_path": map[string]any{
						"type":        "string",
						"description": "Repository-relative path to the file",
					},
				},
				"required": []string{"repo_id", "relative_path"},
			},
		},
		{
			Name:        "get_file_lines",
			Description: "Return a line range for a repo-relative file using a repository selector plus relative path.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Repository selector: canonical ID, name, repo slug, or indexed path",
					},
					"relative_path": map[string]any{
						"type":        "string",
						"description": "Repository-relative path to the file",
					},
					"start_line": map[string]any{
						"type":        "integer",
						"description": "Starting line number (1-indexed)",
					},
					"end_line": map[string]any{
						"type":        "integer",
						"description": "Ending line number (1-indexed)",
					},
				},
				"required": []string{"repo_id", "relative_path", "start_line", "end_line"},
			},
		},
		{
			Name:        "get_entity_content",
			Description: "Return source for a content-bearing graph entity.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{
						"type":        "string",
						"description": "Canonical entity identifier",
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name:        "search_file_content",
			Description: "Search indexed file content across repositories.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{
						"type":        "string",
						"description": "Search pattern or regular expression",
					},
					"repo_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by repository selectors: canonical IDs, names, repo slugs, or indexed paths",
					},
					"languages": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by programming languages",
					},
					"artifact_types": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by artifact types",
					},
					"template_dialects": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by template dialects",
					},
					"iac_relevant": map[string]any{
						"type":        "boolean",
						"description": "Filter for infrastructure-as-code relevant content",
					},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:        "search_entity_content",
			Description: "Search cached entity source snippets across repositories.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{
						"type":        "string",
						"description": "Search pattern or regular expression",
					},
					"entity_types": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by entity types",
					},
					"repo_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by repository selectors: canonical IDs, names, repo slugs, or indexed paths",
					},
					"languages": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by programming languages",
					},
					"artifact_types": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by artifact types",
					},
					"template_dialects": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by template dialects",
					},
					"iac_relevant": map[string]any{
						"type":        "boolean",
						"description": "Filter for infrastructure-as-code relevant content",
					},
				},
				"required": []string{"pattern"},
			},
		},
	}
}
