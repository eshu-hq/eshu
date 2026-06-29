// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func crossRepoDeadCodeTool() ToolDefinition {
	return ToolDefinition{
		Name:        "find_cross_repo_dead_code",
		Description: "Find dead-code candidates across an explicit producer repository and classify symbols kept live by deterministic consumer repository evidence. Ambiguous ownership or missing evidence is returned as unknown instead of dead.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Required producer repository selector: canonical ID, repository name, repo slug, or indexed path",
				},
				"consumer_repo_ids": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional consumer repository selectors that bound cross-repo liveness evidence",
					"default":     []any{},
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional parser language filter such as go, python, typescript, javascript, java, rust, c, cpp, csharp, or sql",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum active producer candidates to classify",
					"default":     100,
					"maximum":     500,
				},
				"exclude_decorated_with": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Decorator names to suppress from active candidates",
					"default":     []any{},
				},
			},
			"required": []string{"repo_id"},
		},
	}
}
