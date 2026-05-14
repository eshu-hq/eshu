package mcp

func codeTopicInvestigationTool() ToolDefinition {
	return ToolDefinition{
		Name:        "investigate_code_topic",
		Description: "Investigate a broad code topic or behavior with ranked files, symbols, coverage metadata, truncation, and exact next-call handles for source reads and relationship stories.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"topic": map[string]any{
					"type":        "string",
					"description": "Natural-language topic or behavior to investigate, for example repo sync authentication or workspace locking",
				},
				"intent": map[string]any{
					"type":        "string",
					"description": "Optional intent such as explain_flow, find_owners, or debug_issue",
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional canonical repository identifier to scope the investigation",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional language filter",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum evidence groups to return",
					"default":     25,
					"maximum":     200,
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Zero-based result offset for paging",
					"default":     0,
					"maximum":     10000,
				},
			},
			"required": []string{"topic"},
		},
	}
}
