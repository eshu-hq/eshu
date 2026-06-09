package mcp

func freshnessTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "get_generation_lifecycle",
			Description: "Inspect bounded scope generation lifecycle history (active, pending, superseded, completed, failed) for a scope, repository, collector, source system, generation, or status. Each row carries the current active generation, trigger kind, freshness hint, observed/activated/superseded timestamps, the per-generation queue status, and the latest failure when present. Unknown scope/repository/generation selectors return an explicit not-found, never a confident empty list.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Optional exact ingestion scope id, for example git-repository-scope:owner/repo.",
					},
					"repository": map[string]any{
						"type":        "string",
						"description": "Optional canonical repository id (matches repository-kind scopes by source_key).",
					},
					"collector_kind": map[string]any{
						"type":        "string",
						"description": "Optional collector kind filter, for example git, aws, or terraform_state.",
					},
					"source_system": map[string]any{
						"type":        "string",
						"description": "Optional source system filter, for example github.",
					},
					"generation_id": map[string]any{
						"type":        "string",
						"description": "Optional exact generation id to drill into a single lifecycle row.",
					},
					"status": map[string]any{
						"type":        "string",
						"description": "Optional generation status filter.",
						"enum":        []string{"pending", "active", "superseded", "completed", "failed"},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum generation lifecycle rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     500,
					},
				},
			},
		},
	}
}
