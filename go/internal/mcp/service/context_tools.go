// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicetools

import "github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"

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
				},
				"required": []string{"workload_id"},
			},
		},
		{
			Name:        "get_service_story",
			Description: "Get the one-call service dossier for a service: identity, API surface, deployment lanes, dependencies, consumers, and evidence graph.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
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
						"description": "Optional repository selector used with service_name to disambiguate service story readback",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector alias used with service_name to disambiguate service story readback",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector alias used with service_name to disambiguate service story readback",
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
			Description: "Plan a service investigation across related repositories, deployment sources, indexed docs, and evidence drilldowns.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service_name": map[string]any{
						"type":        "string",
						"description": "Service name or canonical workload identifier to investigate",
					},
					"repo": map[string]any{
						"type":        "string",
						"description": "Optional repository selector used with service_name to disambiguate service investigation readback",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector alias used with service_name to disambiguate service investigation readback",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector alias used with service_name to disambiguate service investigation readback",
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
				"required": []string{"service_name"},
			},
		},
	}
}
