// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func observabilityCoverageTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_observability_coverage_correlations",
			Description: "List reducer-owned observability coverage correlations: whether a monitored cloud resource or service has alarm, dashboard, log, or trace coverage, and which coverage gaps remain, by scope, provider, coverage signal, observability object, target resource, or target service.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Optional ingestion scope ID for an observability coverage generation.",
					},
					"provider": map[string]any{
						"type":        "string",
						"description": "Observability provider such as aws.",
					},
					"coverage_signal": map[string]any{
						"type":        "string",
						"description": "Coverage signal class such as alarm, dashboard, log_group, or trace_sampling.",
					},
					"observability_object_ref": map[string]any{
						"type":        "string",
						"description": "Provider-native observability object reference such as a CloudWatch alarm ARN.",
					},
					"target_uid": map[string]any{
						"type":        "string",
						"description": "Monitored cloud resource UID (ARN or bare resource id) to anchor coverage lookup.",
					},
					"target_service_ref": map[string]any{
						"type":        "string",
						"description": "Target service reference such as an X-Ray service name to anchor coverage lookup.",
					},
					"source_class": map[string]any{
						"type":        "string",
						"description": "Optional evidence class filter: declared, applied, observed, or mixed.",
						"enum":        []string{"declared", "applied", "observed", "mixed"},
					},
					"resource_class": map[string]any{
						"type":        "string",
						"description": "Optional provider resource class filter such as dashboard, scrape_config, log_signal, or trace_signal.",
					},
					"outcome": map[string]any{
						"type":        "string",
						"description": "Optional reducer outcome filter.",
						"enum":        []string{"exact", "derived", "ambiguous", "unresolved", "stale", "rejected", "drifted", "permission_hidden"},
					},
					"coverage_status": map[string]any{
						"type":        "string",
						"description": "Optional coverage status filter such as covered or gap.",
					},
					"after_correlation_id": map[string]any{
						"type":        "string",
						"description": "Correlation ID from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum correlation rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
	}
}
