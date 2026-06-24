// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// securityAlertReconciliationAggregateTools returns the cheap-summary
// aggregate tools shipped alongside the existing
// list_security_alert_reconciliations list tool. They give callers an O(1)
// answer to ecosystem-level questions like "how many alerts per provider?"
// without paging through the list endpoint.
func securityAlertReconciliationAggregateTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "count_security_alert_reconciliations",
			Description: "Return reducer-owned provider security alert reconciliation totals for one optional scope without paging through individual reconciliation rows. Provides total reconciliations, rollups by reconciliation_status, provider, provider_state, source_freshness, and provider-source coverage. Use before list_security_alert_reconciliations when the question is a count, not a list.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional canonical repository id or human repository selector: name, repo slug, indexed path, local path, or remote URL.",
					},
					"provider": map[string]any{
						"type":        "string",
						"description": "Optional provider identifier (such as `github_security_advisories`) to scope the totals.",
					},
					"package_id": map[string]any{
						"type":        "string",
						"description": "Optional normalized package identity (such as `pkg:npm/example`) to scope the totals.",
					},
					"cve_id": map[string]any{
						"type":        "string",
						"description": "Optional CVE identifier to scope the totals; matches any reconciliation whose cve_ids contain the value.",
					},
					"ghsa_id": map[string]any{
						"type":        "string",
						"description": "Optional GHSA identifier to scope the totals; matches any reconciliation whose ghsa_ids contain the value.",
					},
					"provider_state": map[string]any{
						"type":        "string",
						"description": "Optional provider-reported alert state filter applied before counting.",
					},
					"reconciliation_status": map[string]any{
						"type":        "string",
						"description": "Optional reducer reconciliation status filter applied before counting.",
					},
				},
			},
		},
		{
			Name:        "get_security_alert_reconciliation_inventory",
			Description: "Return a paginated grouped count of reducer-owned provider alert reconciliations along one dimension (reconciliation_status, provider, provider_state, repository_id, package_id). Replaces the page-and-iterate caller pattern for ecosystem-level inventory questions.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group_by": map[string]any{
						"type":        "string",
						"description": "Grouping dimension. reconciliation_status (default) groups by reducer status; provider groups by provider; provider_state groups by provider-reported state; repository_id groups by repository; package_id groups by package.",
						"enum":        []string{"reconciliation_status", "provider", "provider_state", "repository_id", "package_id"},
						"default":     "reconciliation_status",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional canonical repository id or human repository selector: name, repo slug, indexed path, local path, or remote URL.",
					},
					"provider": map[string]any{
						"type":        "string",
						"description": "Optional provider identifier to scope the inventory.",
					},
					"package_id": map[string]any{
						"type":        "string",
						"description": "Optional normalized package identity.",
					},
					"cve_id": map[string]any{
						"type":        "string",
						"description": "Optional CVE identifier to scope the inventory; matches any reconciliation whose cve_ids contain the value.",
					},
					"ghsa_id": map[string]any{
						"type":        "string",
						"description": "Optional GHSA identifier to scope the inventory.",
					},
					"provider_state": map[string]any{
						"type":        "string",
						"description": "Optional provider-reported state filter applied before grouping.",
					},
					"reconciliation_status": map[string]any{
						"type":        "string",
						"description": "Optional reducer reconciliation status filter applied before grouping.",
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
