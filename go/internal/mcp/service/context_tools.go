// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicetools //nolint:dirgate // Root owns service tool registration aggregation (Catalog/Context/Intelligence); route selection lives in the context child.

import "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"

// ContextTools returns the service context, story, and investigation
// registrations in their canonical local order.
func ContextTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "get_service_context",
			Description: "Alias for workload context that accepts service workload selectors through workload_id.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Service workload identifier, or a service name passed through the workload_id field",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
					"evidence_detail": map[string]any{
						"type":        "string",
						"enum":        []string{"full", "handles"},
						"description": "Row detail for deployment evidence artifacts and API surface endpoints. handles (the default for this tool) returns identity rows only (id, relationship_type, resolved_id for artifacts; id, path, methods for endpoints), drops the evidence_index and the content-derived evidence lists (deployment_artifacts, delivery_paths, topology_story and similar), keeps every count, and names each reduced family with its total in truth.omissions; repeat the call with full for the rows. full returns every row in full and can exceed the response budget on a populated service.",
					},
				},
				"required": []string{"workload_id"},
			},
		},
		{
			Name:        "get_service_story",
			Description: "Get the one-call service dossier for a service: identity, API surface, deployment lanes, dependencies, consumers, and evidence graph. Select the service with one of service_id, workload_id, or service_name; the first one given wins in that order. repo separates only services that live in different repositories, so two same-named services in one repository stay ambiguous. When the answer is ambiguous, call again with service_id set to a candidate's next_selector value.",
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
						"description": "Optional service name selector when the caller starts from repository-scoped service story context",
					},
					"repo": map[string]any{
						"type":        "string",
						"description": "Optional repository selector used with service_name. It separates only services in different repositories, not two services in the same repository; use service_id for those",
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
		{
			Name:        "investigate_service",
			Description: "Plan a service investigation across related repositories, deployment sources, indexed docs, and evidence drilldowns. Select the service with service_id or service_name; service_id wins. repo separates only services that live in different repositories. When the answer is ambiguous, call again with service_id set to a candidate's next_selector value.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service_id": map[string]any{
						"type":        "string",
						"description": "Exact service identifier: a full scoped id (workload:repository:<repo_id>:<name>) or a workload:<name> handle, as listed in the candidates of an ambiguous answer. Selects one service on its own and wins over service_name",
					},
					"service_name": map[string]any{
						"type":        "string",
						"description": "Service name or canonical workload identifier to investigate. Optional when service_id is given",
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
					"intent": map[string]any{
						"type":        "string",
						"description": "Optional investigation intent such as runbook, onboarding, or incident",
					},
					"question": map[string]any{
						"type":        "string",
						"description": "Optional user question to preserve in the investigation packet",
					},
				},
			},
		},
	}
}
