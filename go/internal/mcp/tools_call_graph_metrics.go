package mcp

func callGraphMetricsTool() ToolDefinition {
	return ToolDefinition{
		Name:        "inspect_call_graph_metrics",
		Description: "Inspect bounded call-graph metrics for recursive functions and highly connected hub functions within one repository. Requires repo_id and returns source handles, truncation, truth metadata, hub call-degree counts, and recursion evidence.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"repo_id"},
			"properties": map[string]any{
				"metric_type": map[string]any{
					"type":        "string",
					"description": "Call-graph metric to inspect",
					"enum":        []string{"hub_functions", "recursive_functions"},
					"default":     "hub_functions",
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Canonical repository identifier",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional language filter",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum function rows to return",
					"default":     25,
					"minimum":     1,
					"maximum":     200,
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Zero-based result offset for paging",
					"default":     0,
					"minimum":     0,
					"maximum":     10000,
				},
			},
		},
	}
}
