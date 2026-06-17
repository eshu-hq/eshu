package mcp

func runtimeTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_collectors",
			Description: "Return collector runtime status including coordinator-managed, direct-mode, disabled, and unregistered evidence categories.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
		{
			Name:        "list_ingesters",
			Description: "Return the current status for the configured ingesters.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
		{
			Name:        "get_ingester_status",
			Description: "Return the current status for one ingester.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ingester": map[string]any{
						"type":        "string",
						"description": "Ingester identifier",
						"default":     "repository",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "get_index_status",
			Description: "Return the latest checkpointed index status.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
		{
			Name:        "get_hosted_readiness",
			Description: "Return the hosted operator readiness report across status snapshot loading, queue drain, collector completion, shared projection backlog, and API/MCP query readback.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
		{
			Name:        "get_collector_readiness",
			Description: "Return per-collector-family promotion readiness: promotion state (implemented, partial, failed, stale, gated, disabled, permission_hidden, unsupported), reducer readback status, evidence counts, last proof time, blockers, and a recommended next action. Redacts credentials and raw provider payloads.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
		{
			Name:        "get_hosted_governance_status",
			Description: "Return redacted hosted governance status across policy mode, shared-token posture, tenancy, egress, semantic, extension, redaction, retention, audit, and aggregate decision readbacks.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
		{
			Name:        "get_semantic_capability_status",
			Description: "Return semantic extraction capability status, including no-provider unavailable state, redacted provider profiles, queue, budget, audit readbacks, and whether deterministic paths are affected.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
		{
			Name:        "get_answer_narration_status",
			Description: "Return optional governed answer narration status, including disabled-by-default posture, deterministic fallback availability, retention posture, and validator reason codes.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
	}
}
