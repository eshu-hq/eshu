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
			Name:        "get_operator_control_plane",
			Description: "Return the unified operator read model in one call: queue depth with claim-latency and stuck-work signals, reducer-domain backlogs, collector-family promotion verdicts with the newest proof artifact, and dead-letter state classed by reducer domain and collector-generation commit. Scoped tokens receive the same aggregate counts with raw correlation IDs and instance labels redacted.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
		{
			Name:        "get_freshness_causality",
			Description: "Return the freshness causality read model: why answers are stale by closed cause (pending generation, reducer backlog, dead-lettered domain, missing collector completion, plus per-answer content-coverage, unsupported-profile, and retention-expired classes), the generation lifecycle including retired generations, and pending projection work. Scoped tokens receive the same aggregate counts with raw scope/generation identifiers withheld.",
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
		{
			Name:        "get_capability_catalog",
			Description: "Return the reconciled Eshu capability catalog: per-capability maturity, public surfaces, proof signals, owner package, known gaps, and linked issues, with optional maturity and owner filters and bounded paging.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"maturity": map[string]any{
						"type":        "string",
						"description": "Optional maturity filter (general_availability, experimental, preview, gated, degraded, not_implemented).",
					},
					"owner": map[string]any{
						"type":        "string",
						"description": "Optional owner_package filter (exact match).",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of capabilities to return (1-500).",
						"default":     200,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Number of capabilities to skip for paging.",
						"default":     0,
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "get_surface_inventory",
			Description: "Return the generated Eshu surface inventory: every platform surface across six categories (command, collector, reducer_domain, api_route, mcp_tool, console_page) with its readiness lane (implemented, partial, gated, foundation_only, fixture_only, research_only, not_implemented, unsupported), owner, promotion proof, docs, and notes. Use it to summarize what surfaces exist and how production-ready each is. Supports optional category and readiness filters with bounded paging.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"category": map[string]any{
						"type":        "string",
						"description": "Optional surface category filter (command, collector, reducer_domain, api_route, mcp_tool, console_page).",
					},
					"readiness": map[string]any{
						"type":        "string",
						"description": "Optional readiness lane filter (implemented, partial, gated, foundation_only, fixture_only, research_only, not_implemented, unsupported).",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of surfaces to return (1-1000).",
						"default":     200,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Number of surfaces to skip for paging.",
						"default":     0,
					},
				},
				"required": []string{},
			},
		},
	}
}
