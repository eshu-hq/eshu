package mcp

func securityInvestigationTool() ToolDefinition {
	return ToolDefinition{
		Name:        "investigate_hardcoded_secrets",
		Description: "Investigate potential hardcoded passwords, API keys, tokens, private keys, and risky literals from indexed content with redacted findings, suppression metadata, paging, and coverage.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional canonical repository identifier to scope the investigation",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional language filter",
				},
				"finding_kinds": map[string]any{
					"type":        "array",
					"description": "Optional finding kinds to include",
					"items": map[string]any{
						"type": "string",
						"enum": []string{"api_token", "aws_access_key", "password_literal", "private_key", "secret_literal", "slack_token"},
					},
				},
				"include_suppressed": map[string]any{
					"type":        "boolean",
					"description": "Include test, fixture, example, and placeholder findings with suppression notes",
					"default":     false,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum redacted findings to return",
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
		},
	}
}
