// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ecosystemtools

import "github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"

func findChangeSurfaceTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "find_change_surface",
		Description: "Find the blast or change surface for a workload, cloud resource, or terraform module.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target": map[string]any{
					"type":        "string",
					"description": "Target entity identifier",
				},
				"environment": map[string]any{
					"type":        "string",
					"description": "Optional environment context",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum impacted rows to return",
					"default":     50,
					"minimum":     1,
					"maximum":     200,
				},
			},
			"required": []string{"target"},
		},
	}
}

func investigateChangeSurfaceTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "investigate_change_surface",
		Description: "Investigate the code, repository, workload, infrastructure, and transitive impact surface for a service, module, resource, code topic, or changed path set in one bounded call.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target": map[string]any{
					"type":        "string",
					"description": "Optional canonical entity id or exact entity name to resolve before impact traversal.",
				},
				"target_type": map[string]any{
					"type":        "string",
					"description": "Optional target kind used to choose the exact resolver shape.",
					"enum":        []string{"service", "workload", "workload_instance", "repository", "resource", "cloud_resource", "terraform_module", "module"},
				},
				"service_name": map[string]any{
					"type":        "string",
					"description": "Service or workload name to resolve as the graph impact anchor.",
				},
				"workload_id": map[string]any{
					"type":        "string",
					"description": "Canonical workload id to resolve as the graph impact anchor.",
				},
				"resource_id": map[string]any{
					"type":        "string",
					"description": "Canonical cloud resource id to resolve as the graph impact anchor.",
				},
				"module_id": map[string]any{
					"type":        "string",
					"description": "Terraform module uid or name to resolve as the graph impact anchor.",
				},
				"topic": map[string]any{
					"type":        "string",
					"description": "Natural-language code topic such as repo-sync auth behavior.",
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Repository selector for code-topic and changed-path scoping.",
				},
				"changed_paths": map[string]any{
					"type":        "array",
					"description": "Changed file paths to map to touched code symbols.",
					"items":       map[string]any{"type": "string"},
				},
				"environment": map[string]any{
					"type":        "string",
					"description": "Optional environment filter for graph impact rows.",
				},
				"max_depth": map[string]any{
					"type":        "integer",
					"description": "Maximum graph traversal depth.",
					"default":     4,
					"minimum":     1,
					"maximum":     8,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum rows to return per surface.",
					"default":     25,
					"minimum":     1,
					"maximum":     100,
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Result offset for content-backed code investigation.",
					"default":     0,
					"minimum":     0,
					"maximum":     10000,
				},
			},
			"required": []string{},
		},
	}
}
