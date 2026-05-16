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
					"after_version_id": map[string]any{
						"type":        "string",
						"description": "Source PackageVersion.uid from next_cursor when continuing a truncated dependency page.",
					},
					"after_dependency_id": map[string]any{
						"type":        "string",
						"description": "PackageDependency.uid from next_cursor when continuing a truncated dependency page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum dependency edges to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
		{
			Name:        "list_package_registry_correlations",
			Description: "List reducer-owned package ownership candidates, publication evidence, and manifest-backed consumption correlations by package_id or repository_id.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package_id": map[string]any{
						"type":        "string",
						"description": "Package.uid to anchor package ownership, publication, or consumption correlation lookup.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Repository.id to anchor package ownership, publication, or consumption correlation lookup.",
					},
					"relationship_kind": map[string]any{
						"type":        "string",
						"description": "Optional relationship kind filter.",
						"enum":        []string{"ownership", "publication", "consumption"},
					},
					"after_correlation_id": map[string]any{
						"type":        "string",
						"description": "Correlation ID from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum correlation rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
	}
}
