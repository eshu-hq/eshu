package mcp

func factSchemaVersionTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_fact_schema_versions",
			Description: "Return the core fact-kind to supported schema-version registry: the schema version a core reducer or query consumer currently supports for each core fact kind. Static in-binary registry data; it does not move code.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "number",
						"description": "Maximum number of fact-kind rows to return",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "get_fact_schema_version",
			Description: "Return the supported schema version for one core fact kind. When candidate is set, classify that collector version as supported, unsupported_major, unsupported_minor, or unknown_kind so an incompatible collector fact version is detected safely. Advisory only; it does not move code.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"fact_kind": map[string]any{
						"type":        "string",
						"description": "Core fact kind, such as terraform_state_resource or documentation_section",
					},
					"candidate": map[string]any{
						"type":        "string",
						"description": "Optional candidate schema version to classify against the supported version",
					},
				},
				"required": []string{"fact_kind"},
			},
		},
	}
}
