package mcp

func codeQualityInspectionTool() ToolDefinition {
	return ToolDefinition{
		Name:        "inspect_code_quality",
		Description: "Inspect bounded code-quality and refactoring metrics for functions: complexity, function length, argument count, or combined refactoring candidates.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"check": map[string]any{
					"type":        "string",
					"description": "Metric family to inspect",
					"enum":        []string{"complexity", "function_length", "argument_count", "refactoring_candidates"},
					"default":     "refactoring_candidates",
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional canonical repository identifier to scope the inspection",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional language filter",
				},
				"entity_id": map[string]any{
					"type":        "string",
					"description": "Optional exact function entity identifier",
				},
				"function_name": map[string]any{
					"type":        "string",
					"description": "Optional exact function name",
				},
				"min_complexity": map[string]any{
					"type":        "integer",
					"description": "Minimum cyclomatic complexity for complexity/refactoring checks",
					"default":     10,
				},
				"min_lines": map[string]any{
					"type":        "integer",
					"description": "Minimum function line count for length/refactoring checks",
					"default":     20,
				},
				"min_arguments": map[string]any{
					"type":        "integer",
					"description": "Minimum function argument count for argument/refactoring checks",
					"default":     5,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum functions to return",
					"default":     10,
					"minimum":     1,
					"maximum":     100,
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Zero-based result offset for paging",
					"default":     0,
					"minimum":     0,
					"maximum":     10000,
				},
			},
			"required": []string{},
		},
	}
}
