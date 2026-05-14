package mcp

func contextTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "resolve_entity",
			Description: "Resolve a fuzzy or user-supplied identifier into ranked canonical graph entities.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Query string to resolve",
					},
					"types": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by entity types",
					},
					"kinds": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by entity kinds",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector to scope the search: canonical ID, name, repo slug, or indexed path",
					},
					"exact": map[string]any{
						"type":        "boolean",
						"description": "Whether to perform exact matching",
						"default":     false,
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results",
						"default":     10,
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "get_entity_context",
			Description: "Get context for a canonical entity ID.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{
						"type":        "string",
						"description": "Canonical entity identifier",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name:        "get_workload_context",
			Description: "Get logical or environment-specific context for a canonical workload identifier.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Canonical workload identifier",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
				},
				"required": []string{"workload_id"},
			},
		},
		{
			Name:        "get_workload_story",
			Description: "Get a structured story for a workload.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Canonical workload identifier",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
				},
				"required": []string{"workload_id"},
			},
		},
		{
			Name:        "get_service_context",
			Description: "Alias for workload context that only accepts canonical workload identifiers for service workloads.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Canonical workload identifier for a service",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
				},
				"required": []string{"workload_id"},
			},
		},
		{
			Name:        "get_service_story",
			Description: "Get the one-call service dossier for a service: identity, API surface, deployment lanes, dependencies, consumers, and evidence graph.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Service workload identifier or service name",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
				},
				"required": []string{"workload_id"},
			},
		},
		{
			Name:        "investigate_service",
			Description: "Plan a service investigation across related repositories, deployment sources, indexed docs, and evidence drilldowns.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service_name": map[string]any{
						"type":        "string",
						"description": "Service name or canonical workload identifier to investigate",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
					"intent": map[string]any{
						"type":        "string",
						"description": "Optional investigation intent such as runbook, onboarding, or incident",
					},
					"question": map[string]any{
						"type":        "string",
						"description": "Optional user question to preserve in the investigation packet",
					},
				},
				"required": []string{"service_name"},
			},
		},
	}
}
