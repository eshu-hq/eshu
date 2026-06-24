// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func preChangeImpactTool() ToolDefinition {
	return ToolDefinition{
		Name:        "analyze_pre_change_impact",
		Description: "Analyze a base/head diff or explicit changed files before editing or landing a change, returning bounded affected code, graph impact, missing evidence, and next calls.",
		InputSchema: preChangeImpactInputSchema(false),
	}
}

func developerChangePlanTool() ToolDefinition {
	return ToolDefinition{
		Name:        "plan_developer_change",
		Description: "Build a read-only developer_change_plan.v1 artifact from a base/head diff or explicit changed files, returning ordered actions, risk, tests, missing evidence, and bounded next calls.",
		InputSchema: preChangeImpactInputSchema(true),
	}
}

func preChangeImpactInputSchema(includeIntent bool) map[string]any {
	properties := map[string]any{
		"repo_id": map[string]any{
			"type":        "string",
			"description": "Repository selector for changed-path lookup.",
		},
	}
	if includeIntent {
		properties["developer_intent"] = map[string]any{
			"type":        "string",
			"description": "Optional developer intent used to rank and explain plan actions.",
		}
	}
	for key, value := range map[string]any{
		"base_ref": map[string]any{
			"type":        "string",
			"description": "Git base ref used to derive the diff.",
		},
		"head_ref": map[string]any{
			"type":        "string",
			"description": "Git head ref used to derive the diff.",
		},
		"changed_paths": map[string]any{
			"type":        "array",
			"description": "Repo-relative changed file paths treated as modified files.",
			"items":       map[string]any{"type": "string"},
		},
		"changes": map[string]any{
			"type":        "array",
			"description": "Changed files with status and optional rename source.",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Current repo-relative file path.",
					},
					"old_path": map[string]any{
						"type":        "string",
						"description": "Previous repo-relative file path for renamed or copied files.",
					},
					"status": map[string]any{
						"type":        "string",
						"description": "Change status.",
						"enum":        []string{"added", "modified", "deleted", "renamed", "copied"},
					},
				},
			},
		},
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
			"description": "Natural-language code topic to scope pre-change investigation.",
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
	} {
		properties[key] = value
	}
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   []string{},
	}
}
