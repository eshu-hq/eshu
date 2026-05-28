package mcp

// supplyChainImpactAggregateTools returns the cheap-summary aggregate tools
// shipped alongside the existing list_supply_chain_impact_findings list tool.
// They give callers an O(1) answer to ecosystem-level totals questions like
// "how many critical findings across all repos?" without forcing them to
// paginate through the list endpoint.
func supplyChainImpactAggregateTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "count_supply_chain_impact_findings",
			Description: "Return reducer-owned vulnerability impact totals for one optional scope without paging through individual findings. Provides total, affected, not_affected, by-priority-bucket counts, and a CVSS severity rollup (critical/high/medium/low/none). Use before list_supply_chain_impact_findings when the question is a count, not a list.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"cve_id": map[string]any{
						"type":        "string",
						"description": "Optional CVE or advisory identifier to scope the totals.",
					},
					"package_id": map[string]any{
						"type":        "string",
						"description": "Optional normalized package identity such as pkg:npm/example.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional canonical repository id or human repository selector: name, repo slug, indexed path, local path, or remote URL.",
					},
					"subject_digest": map[string]any{
						"type":        "string",
						"description": "Optional image or artifact digest from SBOM or runtime evidence.",
					},
					"impact_status": map[string]any{
						"type":        "string",
						"description": "Optional reducer impact status filter.",
						"enum":        []string{"affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"},
					},
				},
			},
		},
		{
			Name:        "get_supply_chain_impact_inventory",
			Description: "Return a paginated grouped count of reducer-owned vulnerability impact findings along one dimension (impact_status, priority_bucket, severity, repository_id). Replaces the page-and-iterate caller pattern for ecosystem-level inventory questions.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group_by": map[string]any{
						"type":        "string",
						"description": "Grouping dimension. impact_status (default) groups by reducer status; priority_bucket groups by triage bucket; severity groups by CVSS severity bucket; repository_id groups by repository.",
						"enum":        []string{"impact_status", "priority_bucket", "severity", "repository_id"},
						"default":     "impact_status",
					},
					"cve_id": map[string]any{
						"type":        "string",
						"description": "Optional CVE or advisory identifier to scope the inventory.",
					},
					"package_id": map[string]any{
						"type":        "string",
						"description": "Optional normalized package identity.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional canonical repository id or human repository selector: name, repo slug, indexed path, local path, or remote URL.",
					},
					"subject_digest": map[string]any{
						"type":        "string",
						"description": "Optional image or artifact digest.",
					},
					"impact_status": map[string]any{
						"type":        "string",
						"description": "Optional reducer impact status filter applied before grouping.",
						"enum":        []string{"affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum buckets to return per page.",
						"default":     100,
						"minimum":     1,
						"maximum":     500,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Zero-based result offset for paging.",
						"default":     0,
						"minimum":     0,
						"maximum":     10000,
					},
				},
			},
		},
	}
}
