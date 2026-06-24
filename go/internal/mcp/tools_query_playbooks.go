// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func queryPlaybookTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_query_playbooks",
			Description: "List deterministic query playbooks with versions, required inputs, ordered steps, expected truth, evidence, and failure modes.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "resolve_query_playbook",
			Description: "Resolve one query playbook and declared inputs into an ordered, bounded call sequence without executing the calls.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"playbook_id": map[string]any{
						"type":        "string",
						"description": "Catalog playbook ID to resolve",
					},
					"inputs": map[string]any{
						"type":                 "object",
						"description":          "Declared playbook inputs as string key/value pairs",
						"additionalProperties": map[string]any{"type": "string"},
					},
				},
				"required": []string{"playbook_id"},
			},
		},
	}
}
