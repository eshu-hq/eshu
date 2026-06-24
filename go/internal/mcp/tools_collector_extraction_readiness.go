// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func collectorExtractionReadinessTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_collector_extraction_readiness",
			Description: "Return the advisory collector extraction readiness catalog: each tracked collector family's classification (keep_in_tree, extraction_candidate, blocked, or external_ready), per-criterion checklist, and blockers. Advisory static policy data; it does not move code.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "number",
						"description": "Maximum number of collector family rows to return",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "get_collector_extraction_readiness",
			Description: "Return the advisory extraction readiness drilldown for one collector family, including the per-criterion checklist and rationale. Advisory only; it does not move code.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"family": map[string]any{
						"type":        "string",
						"description": "Canonical collector family kind, such as git, pagerduty, or jira",
					},
				},
				"required": []string{"family"},
			},
		},
	}
}
