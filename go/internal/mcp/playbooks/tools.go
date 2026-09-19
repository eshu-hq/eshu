// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package playbooktools

import "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"

// Tools returns fresh MCP definitions for the query-playbook catalog.
func Tools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "list_query_playbooks",
			Description: "List deterministic query playbooks. Default (compact) view returns id/name/version/prompt_family/description with bounded paging; view=full returns the complete shape (required inputs, ordered steps, expected truth, evidence, and failure modes).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of playbooks to return (1-200).",
						"default":     20,
						"minimum":     1,
						"maximum":     200,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Number of playbooks to skip for paging.",
						"default":     0,
						"minimum":     0,
					},
					"view": map[string]any{
						"type":        "string",
						"description": "compact (default: id/name/version/prompt_family/description) or full (complete playbook detail).",
						"enum":        []string{"compact", "full"},
					},
				},
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
