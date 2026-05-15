package mcp

func packageRegistryTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_package_registry_dependencies",
			Description: "List package-native dependency edges by Package.uid or PackageVersion.uid without inferring repository ownership or runtime consumption.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package_id": map[string]any{
						"type":        "string",
						"description": "Package.uid to anchor dependency lookup when version_id is absent.",
					},
					"version_id": map[string]any{
						"type":        "string",
						"description": "PackageVersion.uid for an exact version-scoped dependency lookup.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum dependency edges to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
				"required": []string{"limit"},
			},
		},
	}
}
