// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func investigationWorkflowTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_investigation_workflows",
			Description: "List guided investigation workflows with input shape, required and optional evidence, expected output packet, grouped atomic tools, starter prompts, and missing-evidence routing.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "resolve_investigation_workflow",
			Description: "Resolve one guided investigation workflow and observed missing-evidence state into bounded recommended next calls without executing the calls.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workflow_id": map[string]any{
						"type":        "string",
						"description": "Catalog workflow ID to resolve.",
					},
					"inputs": map[string]any{
						"type":                 "object",
						"description":          "Declared workflow inputs as string key/value pairs.",
						"additionalProperties": map[string]any{"type": "string"},
					},
					"missing_evidence": map[string]any{
						"type":        "array",
						"description": "Observed missing-evidence keys from a prior workflow packet or tool response.",
						"items":       map[string]any{"type": "string"},
					},
				},
				"required": []string{"workflow_id"},
			},
		},
	}
}
