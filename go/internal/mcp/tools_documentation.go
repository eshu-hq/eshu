package mcp

func documentationTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_documentation_findings",
			Description: "List durable documentation truth findings by bounded filters.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id":      map[string]any{"type": "string"},
					"generation_id": map[string]any{"type": "string"},
					"repo":          map[string]any{"type": "string"},
					"finding_type":  map[string]any{"type": "string"},
					"source_id":     map[string]any{"type": "string"},
					"document_id":   map[string]any{"type": "string"},
					"status":        map[string]any{"type": "string"},
					"truth_level":   map[string]any{"type": "string"},
					"freshness_state": map[string]any{
						"type":        "string",
						"description": "Freshness state filter such as fresh or stale",
					},
					"updated_since": map[string]any{
						"type":        "string",
						"description": "Only return findings updated at or after this RFC3339 timestamp",
					},
					"limit": map[string]any{
						"type":    "integer",
						"default": 50,
						"minimum": 1,
						"maximum": 200,
					},
					"cursor": map[string]any{"type": "string"},
				},
				"required": []string{},
			},
		},
		{
			Name:        "get_documentation_evidence_packet",
			Description: "Return the bounded evidence packet for one documentation finding.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"finding_id": map[string]any{
						"type":        "string",
						"description": "Documentation finding identifier",
					},
				},
				"required": []string{"finding_id"},
			},
		},
		{
			Name:        "check_documentation_evidence_packet_freshness",
			Description: "Check whether a saved documentation evidence packet version is current.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"packet_id": map[string]any{
						"type":        "string",
						"description": "Documentation evidence packet identifier",
					},
					"packet_version": map[string]any{
						"type":        "string",
						"description": "Previously saved packet version",
					},
				},
				"required": []string{"packet_id"},
			},
		},
	}
}
