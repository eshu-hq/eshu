// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicetools

import "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"

// IntelligenceTools returns the MCP tool for the composed service
// intelligence report. The tool maps to GET
// /api/v0/services/{service_name}/intelligence-report and returns the
// service_intelligence_report.v1 schema: identity, code-to-runtime, deployment,
// supply-chain, and incident sections with truth labels, evidence handles,
// limitations, bounded next calls, and suggested investigations.
func IntelligenceTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "get_service_intelligence_report",
			Description: "Compose the one-call service intelligence report for a service: identity, code-to-runtime trace, deployment/config influence, supply-chain, and incidents, each with preserved truth labels, evidence handles, limitations, and bounded next calls, plus deterministic suggested investigations. Returns schema service_intelligence_report.v1. Select the service with one of service_id, workload_id, or service_name; the first one given wins in that order. repo separates only services that live in different repositories. When the answer is ambiguous, call again with service_id set to a candidate's next_selector value.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service_id": map[string]any{
						"type":        "string",
						"description": "Exact service identifier: a full scoped id (workload:repository:<repo_id>:<name>) or a workload:<name> handle, as listed in the candidates of an ambiguous answer. Selects one service on its own and wins over workload_id and service_name",
					},
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
						"description": "Optional repository selector used with service_name. It separates only services in different repositories; use service_id for the rest",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector alias used with service_name. It separates only services in different repositories; use service_id for the rest",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector alias used with service_name. It separates only services in different repositories; use service_id for the rest",
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
