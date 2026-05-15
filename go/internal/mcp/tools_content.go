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
			Name:        "build_evidence_citation_packet",
			Description: "Hydrate a bounded set of file and entity handles into ranked source, docs, manifest, and deployment citations.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subject": map[string]any{
						"type":        "object",
						"description": "Optional answer subject such as repo, service, workload, or code_topic.",
					},
					"question": map[string]any{
						"type":        "string",
						"description": "Prompt or answer fragment the citations support.",
					},
					"handles": map[string]any{
						"type":        "array",
						"description": "Evidence handles returned by story, investigation, search, or drilldown tools.",
						"maxItems":    500,
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"kind": map[string]any{
									"type":        "string",
									"enum":        []string{"file", "entity"},
									"description": "Handle kind. If omitted, Eshu infers file from repo_id plus relative_path or entity from entity_id.",
								},
								"repo_id": map[string]any{
									"type":        "string",
									"description": "Canonical repository ID for a file handle.",
								},
								"relative_path": map[string]any{
									"type":        "string",
									"description": "Repository-relative file path for a file handle.",
								},
								"entity_id": map[string]any{
									"type":        "string",
									"description": "Canonical entity ID for an entity handle.",
								},
								"evidence_family": map[string]any{
									"type":        "string",
									"description": "Optional family such as source, documentation, manifest, deployment, or relationship.",
								},
								"reason": map[string]any{
									"type":        "string",
									"description": "Why this handle supports the answer.",
								},
								"start_line": map[string]any{
									"type":        "integer",
									"description": "Optional starting line for file citations.",
								},
								"end_line": map[string]any{
									"type":        "integer",
									"description": "Optional ending line for file citations.",
								},
							},
						},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum handles to hydrate in this packet.",
						"default":     10,
						"minimum":     1,
						"maximum":     50,
					},
				},
				"required": []string{"handles"},
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
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum search results to return",
						"default":     10,
						"minimum":     1,
						"maximum":     200,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Zero-based result offset for paging",
						"default":     0,
						"minimum":     0,
						"maximum":     10000,
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
					"repo_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by repository selectors: canonical IDs, names, repo slugs, or indexed paths",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum search results to return",
						"default":     10,
						"minimum":     1,
						"maximum":     200,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Zero-based result offset for paging",
						"default":     0,
						"minimum":     0,
						"maximum":     10000,
					},
				},
				"required": []string{"pattern"},
			},
		},
	}
}
