package mcp

func incidentContextTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "get_incident_context",
			Description: "Get a bounded incident context packet from collected incident evidence, including explicit missing Jira, pull-request, runtime artifact, image, build/deploy, commit, and deployable slots.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"provider_incident_id": map[string]any{
						"type":        "string",
						"description": "Provider incident identifier, such as a PagerDuty incident id.",
					},
					"provider": map[string]any{
						"type":        "string",
						"description": "Incident provider. Defaults to pagerduty.",
						"default":     "pagerduty",
					},
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Optional provider scope id to disambiguate duplicate provider incident ids.",
					},
					"service_id": map[string]any{
						"type":        "string",
						"description": "Optional provider service id used to bound fallback change candidates.",
					},
					"since": map[string]any{
						"type":        "string",
						"description": "Optional RFC3339 lower bound for fallback change candidates.",
					},
					"until": map[string]any{
						"type":        "string",
						"description": "Optional RFC3339 upper bound for fallback change candidates.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum timeline and change-candidate rows to return.",
						"default":     25,
						"minimum":     1,
						"maximum":     100,
					},
				},
				"required": []string{"provider_incident_id"},
			},
		},
	}
}
