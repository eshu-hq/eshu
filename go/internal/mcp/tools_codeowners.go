// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func codeownersTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_codeowners_ownership",
			Description: "List a repository's CODEOWNERS rule-to-owner declarations (issue #5419 Phase 4) from the Phase 3 DECLARES_CODEOWNER graph edges, plus an effective_owner resolved by manifest-vs-codeowners precedence: a service-catalog manifest declaration with an exact or derived reducer outcome wins; otherwise the CODEOWNERS last-match-wins rule (the highest order_index rule) applies; otherwise effective_owner is empty.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"repository_id"},
				"properties": map[string]any{
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Repository to anchor the read on.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum ownership rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
					"after_order_index": map[string]any{
						"type":        "integer",
						"description": "Keyset cursor order_index component from a prior next_cursor. Must be sent with after_pattern and after_ref.",
					},
					"after_pattern": map[string]any{
						"type":        "string",
						"description": "Keyset cursor pattern component from a prior next_cursor. Must be sent with after_order_index and after_ref.",
					},
					"after_ref": map[string]any{
						"type":        "string",
						"description": "Keyset cursor owner_ref component from a prior next_cursor. Must be sent with after_order_index and after_pattern.",
					},
				},
			},
		},
	}
}
