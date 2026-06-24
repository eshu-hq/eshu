// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// serviceIntelligenceTools returns the MCP tool for the composed service
// intelligence report. The tool maps to GET
// /api/v0/services/{service_name}/intelligence-report and returns the
// service_intelligence_report.v1 schema: identity, code-to-runtime, deployment,
// supply-chain, and incident sections with truth labels, evidence handles,
// limitations, bounded next calls, and suggested investigations.
func serviceIntelligenceTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "get_service_intelligence_report",
			Description: "Compose the one-call service intelligence report for a service: identity, code-to-runtime trace, deployment/config influence, supply-chain, and incidents, each with preserved truth labels, evidence handles, limitations, and bounded next calls, plus deterministic suggested investigations. Returns schema service_intelligence_report.v1.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Service workload identifier, or a service name passed through the workload_id field",
					},
					"service_name": map[string]any{
						"type":        "string",
						"description": "Optional service name selector when the caller starts from repository-scoped service context",
					},
					"repo": map[string]any{
						"type":        "string",
						"description": "Optional repository selector used with service_name to disambiguate the service",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector alias used with service_name to disambiguate the service",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector alias used with service_name to disambiguate the service",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
				},
			},
		},
	}
}
