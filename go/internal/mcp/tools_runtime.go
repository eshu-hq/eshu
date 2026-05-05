package mcp

func runtimeTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_ingesters",
			Description: "Return the current status for the configured ingesters.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
		{
			Name:        "get_ingester_status",
			Description: "Return the current status for one ingester.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ingester": map[string]any{
						"type":        "string",
						"description": "Ingester identifier",
						"default":     "repository",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "get_index_status",
			Description: "Return the latest checkpointed index status.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
	}
}
