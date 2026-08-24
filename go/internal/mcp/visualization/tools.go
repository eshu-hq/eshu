// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualizationtools

import "github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"

// Tools returns the canonical visualization-packet registration definition.
// The tool derives a bounded packet from a response the caller has already
// received through an authorized API or MCP read; it does not run a new query.
func Tools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "derive_visualization_packet",
			Description: "Derive a bounded visualization packet from an already-authorized answer response. Supports service_story, evidence_citation, and incident_context source responses without running a new query.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"view": map[string]any{
						"type":        "string",
						"description": "Visualization view to derive.",
						"enum":        []string{"service_story", "evidence_citation", "incident_context"},
					},
					"source_response": map[string]any{
						"type":        "object",
						"description": "Source answer response payload the caller already received from an authorized API or MCP tool.",
					},
					"source_truth": map[string]any{
						"type":        "object",
						"description": "Optional source TruthEnvelope copied into the derived visualization packet; the route envelope reports visualization.packet_derivation.",
					},
				},
				"required": []string{"view", "source_response"},
			},
		},
	}
}
