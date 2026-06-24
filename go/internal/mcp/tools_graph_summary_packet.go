// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// graphSummaryPacketTool defines the bounded, summary-first graph packet tool.
// It mirrors get_ecosystem_overview but adds a repo-scoped packet (hot entities
// by call degree, key relationship type counts, and a repo ecosystem map) with a
// bounded hot-entity limit. Without repo_id the handler returns only the bounded
// ecosystem-wide label counts plus a note; the tool never triggers a whole-graph
// hot-entity scan.
func graphSummaryPacketTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_graph_summary_packet",
		Description: "Get a bounded, summary-first graph packet: hot entities (most-connected code functions by call degree), key relationship type counts, and a per-scope ecosystem map. Provide repo_id for the repo-scoped packet (hot entities, relationship counts, repo ecosystem map). Without repo_id only bounded ecosystem-wide label counts plus a note are returned; hot-entity ranking requires repo_id. Deterministic, truth-labeled, and never runs a whole-graph hot-entity scan.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Repository scope for hot entities, relationship counts, and the repo ecosystem map. Omit for bounded ecosystem-wide label counts only.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum hot entities to return (repo-scoped only).",
					"default":     10,
					"minimum":     1,
					"maximum":     100,
				},
			},
			"required": []string{},
		},
	}
}
