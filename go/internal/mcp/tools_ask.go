package mcp

// askTools returns the MCP tool definition for the Ask Eshu natural-language
// answer tool. The tool is default-off: when no agent_reasoning provider
// profile is configured on the server, every call returns an unavailable
// response with state "unavailable". When enabled, it wires the bounded
// Tier-1 engine and returns the answer with evidence-backed truth metadata.
func askTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "ask",
			Description: "Ask Eshu a free-form natural-language question about your repositories, services, dependencies, infrastructure, or runtime environment. The engine plans the most efficient retrieval path, assembles evidence-backed AnswerPackets, and returns the answer with truth metadata. This tool is default-off: it requires ESHU_ASK_ENABLED=true and a configured agent_reasoning provider profile. When unavailable, it returns state='unavailable'.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"question"},
				"properties": map[string]any{
					"question": map[string]any{
						"type":        "string",
						"description": "The natural-language question to answer about your stack.",
					},
					"format": map[string]any{
						"type":        "string",
						"enum":        []string{"auto", "markdown", "mermaid", "json", "yaml", "csv"},
						"default":     "auto",
						"description": "Requested output format. 'auto' infers from the question.",
					},
				},
			},
		},
	}
}
