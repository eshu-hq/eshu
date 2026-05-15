package mcp

func structuralInventoryTool() ToolDefinition {
	return ToolDefinition{
		Name:        "inspect_code_inventory",
		Description: "Inspect bounded structural code inventory such as functions, classes, top-level file elements, dataclasses, documented functions, decorated methods, classes with a method, and super calls. Provide at least one scope filter: repo_id, file_path, language, entity_kind, or symbol.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional canonical repository identifier to scope the inventory",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional language filter",
				},
				"inventory_kind": map[string]any{
					"type":        "string",
					"description": "Structural inventory filter",
					"enum":        []string{"entity", "top_level", "dataclass", "documented", "documented_function", "decorated", "class_with_method", "super_call", "function_count_by_file"},
					"default":     "entity",
				},
				"entity_kind": map[string]any{
					"type":        "string",
					"description": "Optional entity kind such as function, class, module, variable, component, type_alias, or sql_function. Must be function for function_count_by_file inventory.",
				},
				"file_path": map[string]any{
					"type":        "string",
					"description": "Optional repo-relative file path anchor",
				},
				"symbol": map[string]any{
					"type":        "string",
					"description": "Optional exact entity name filter",
				},
				"decorator": map[string]any{
					"type":        "string",
					"description": "Optional decorator filter for decorated inventory",
				},
				"method_name": map[string]any{
					"type":        "string",
					"description": "Method name required for class_with_method inventory",
				},
				"class_name": map[string]any{
					"type":        "string",
					"description": "Optional class or implementation context filter",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum inventory rows to return",
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
