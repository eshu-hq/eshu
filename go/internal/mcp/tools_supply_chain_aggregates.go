// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
					"advisory_id": map[string]any{
						"type":        "string",
						"description": "Optional exact source advisory identifier such as GHSA or OSV.",
					},
					"ghsa_id": map[string]any{
						"type":        "string",
						"description": "Optional GHSA advisory identifier alias for advisory_id.",
					},
					"osv_id": map[string]any{
						"type":        "string",
						"description": "Optional OSV advisory identifier alias for advisory_id.",
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
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Optional exact image reference stored on reducer-owned impact findings.",
					},
					"impact_status": map[string]any{
						"type":        "string",
						"description": "Optional reducer impact status filter.",
						"enum":        []string{"affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"},
					},
					"ecosystem": map[string]any{
						"type":        "string",
						"description": "Optional package ecosystem filter.",
					},
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Optional reducer-admitted workload anchor.",
					},
					"service_id": map[string]any{
						"type":        "string",
						"description": "Optional reducer-admitted service anchor.",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional reducer-admitted environment anchor.",
					},
					"severity": map[string]any{
						"type":        "string",
						"description": "Optional CVSS-derived severity bucket.",
						"enum":        []string{"critical", "high", "medium", "low", "none"},
					},
					"profile": map[string]any{
						"type":        "string",
						"description": "Detection profile selector. precise (default) counts only exact installed-version anchors resolved by supported matchers; comprehensive also counts range-only, SBOM/CPE-derived, malformed, and missing-version rows. Unsupported ecosystems remain readiness gaps.",
						"enum":        []string{"precise", "comprehensive"},
						"default":     "precise",
					},
					"priority_bucket": map[string]any{
						"type":        "string",
						"description": "Optional reducer triage priority filter. Priority explains urgency and does not change impact truth.",
						"enum":        []string{"critical", "high", "medium", "low", "informational"},
					},
					"min_priority_score": map[string]any{
						"type":        "integer",
						"description": "Minimum reducer priority score from 0 through 100. Zero is the default no-op value.",
						"default":     0,
						"minimum":     0,
						"maximum":     100,
					},
					"suppression_state": map[string]any{
						"type":        "string",
						"description": "Optional reducer suppression-state filter. Hidden states (operator suppressions and advisory_retracted) require include_suppressed=true to be counted.",
						"enum":        []string{"active", "not_affected", "accepted_risk", "false_positive", "ignored", "expired", "provider_dismissed", "scope_mismatch", "advisory_retracted"},
					},
					"include_suppressed": map[string]any{
						"type":        "boolean",
						"description": "Include findings hidden by operator-asserted VEX or policy suppression, or by a source retraction of the advisory (advisory_retracted).",
						"default":     false,
					},
				},
			},
		},
		{
			Name:        "get_supply_chain_impact_inventory",
			Description: "Return a paginated grouped count of reducer-owned vulnerability impact findings along one dimension (impact_status, priority_bucket, severity, repository_id, ecosystem, service_id, workload_id, environment). Replaces the page-and-iterate caller pattern for ecosystem-level inventory questions. The service_id, workload_id, and environment dimensions resolve at read time, count a multi-service finding in every group, and add per-bucket completeness plus distinct_findings, the distinct finding count over the whole filtered set (normally equal to the count tool total_findings; it can differ under a scope-id grant, while the winners read model is stale, or when copies of one finding differ on image_ref, detection_profile, or priority_bucket), ordered by max_priority_score, count, then value; compose them with a filter such as group_by=environment with service_id.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group_by": map[string]any{
						"type":        "string",
						"description": "Grouping dimension. impact_status (default) groups by reducer status; priority_bucket groups by triage bucket; severity groups by CVSS severity bucket; repository_id groups by repository; ecosystem groups by package ecosystem; service_id, workload_id, and environment group by the runtime service, workload, or deployment environment each finding resolves to. A finding with two services counts in both service buckets, so bucket counts can sum above distinct_findings. Those three dimensions return per-bucket completeness (complete, partial, unattributed, read_model_unavailable) and an unattributed bucket (value empty, unattributed true) for findings with no resolved group; the unattributed bucket is never complete. They answer 503 when the grouped read model is not available (ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ is off by default).",
						"enum":        []string{"impact_status", "priority_bucket", "severity", "repository_id", "ecosystem", "service_id", "workload_id", "environment"},
						"default":     "impact_status",
					},
					"cve_id": map[string]any{
						"type":        "string",
						"description": "Optional CVE or advisory identifier to scope the inventory.",
					},
					"advisory_id": map[string]any{
						"type":        "string",
						"description": "Optional exact source advisory identifier such as GHSA or OSV.",
					},
					"ghsa_id": map[string]any{
						"type":        "string",
						"description": "Optional GHSA advisory identifier alias for advisory_id.",
					},
					"osv_id": map[string]any{
						"type":        "string",
						"description": "Optional OSV advisory identifier alias for advisory_id.",
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
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Optional exact image reference stored on reducer-owned impact findings.",
					},
					"impact_status": map[string]any{
						"type":        "string",
						"description": "Optional reducer impact status filter applied before grouping.",
						"enum":        []string{"affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"},
					},
					"ecosystem": map[string]any{
						"type":        "string",
						"description": "Optional package ecosystem filter.",
					},
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Optional reducer-admitted workload anchor.",
					},
					"service_id": map[string]any{
						"type":        "string",
						"description": "Optional reducer-admitted service anchor.",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional reducer-admitted environment anchor.",
					},
					"severity": map[string]any{
						"type":        "string",
						"description": "Optional CVSS-derived severity bucket.",
						"enum":        []string{"critical", "high", "medium", "low", "none"},
					},
					"profile": map[string]any{
						"type":        "string",
						"description": "Detection profile selector. precise (default) includes only exact installed-version anchors; comprehensive includes range-only, SBOM/CPE-derived, malformed, and missing-version rows.",
						"enum":        []string{"precise", "comprehensive"},
						"default":     "precise",
					},
					"priority_bucket": map[string]any{
						"type":        "string",
						"description": "Optional reducer triage priority filter applied before grouping.",
						"enum":        []string{"critical", "high", "medium", "low", "informational"},
					},
					"min_priority_score": map[string]any{
						"type":        "integer",
						"description": "Minimum reducer priority score from 0 through 100. Zero is the default no-op value.",
						"default":     0,
						"minimum":     0,
						"maximum":     100,
					},
					"suppression_state": map[string]any{
						"type":        "string",
						"description": "Optional reducer suppression-state filter applied before grouping. Hidden states (operator suppressions and advisory_retracted) require include_suppressed=true to be included.",
						"enum":        []string{"active", "not_affected", "accepted_risk", "false_positive", "ignored", "expired", "provider_dismissed", "scope_mismatch", "advisory_retracted"},
					},
					"include_suppressed": map[string]any{
						"type":        "boolean",
						"description": "Include findings hidden by operator-asserted VEX or policy suppression, or by a source retraction of the advisory (advisory_retracted), before grouping.",
						"default":     false,
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
