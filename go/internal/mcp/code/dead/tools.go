// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcodetools

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// Tools returns the three MCP dead-code tool definitions: the unused-function
// finder, the coverage-aware investigation, and the cross-repository
// classifier.
func Tools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		findDeadCodeTool(),
		deadCodeInvestigationTool(),
		crossRepoDeadCodeTool(),
	}
}

func findDeadCodeTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "find_dead_code",
		Description: "Find potentially unused functions (dead code) across the indexed codebase, optionally scoped to a canonical repository identifier and excluding functions with specific decorators. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected. A candidate whose only incoming edges come from repositories outside a scoped token's grant is kept and marked ambiguous with the permission_hidden_consumer reason, never reported as unused and never silently dropped.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"exclude_decorated_with": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "List of decorator names to exclude from dead code analysis",
					"default":     []any{},
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional canonical repository identifier",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum dead-code candidates to return",
					"default":     defaultLimit,
				},
				"scope": map[string]any{
					"type":        "string",
					"description": "Search scope",
					"default":     "auto",
				},
			},
			"required": []string{},
		},
	}
}

func deadCodeInvestigationTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "investigate_dead_code",
		Description: "Investigate dead-code candidates with coverage, language maturity, exactness blockers, candidate buckets, source handles, and conservative ambiguity for JavaScript/TypeScript precision risk. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected. A candidate whose only incoming edges come from repositories outside a scoped token's grant is kept and marked ambiguous with the permission_hidden_consumer reason, never reported as unused and never silently dropped.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional repository selector: canonical ID, repository name, repo slug, or indexed path",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional parser language filter such as go, python, typescript, tsx, javascript, java, rust, c, cpp, csharp, or sql",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum active dead-code candidates to return after policy filtering",
					"default":     defaultLimit,
					"maximum":     500,
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Zero-based offset across active candidates for paging",
					"default":     0,
					"maximum":     2000,
				},
				"exclude_decorated_with": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Decorator names to suppress from returned active candidates",
					"default":     []any{},
				},
			},
			"required": []string{},
		},
	}
}

func crossRepoDeadCodeTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "find_cross_repo_dead_code",
		Description: "Find dead-code candidates across an explicit producer repository and classify symbols kept live by deterministic consumer repository evidence. Ambiguous ownership or missing evidence is returned as unknown instead of dead. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected.",
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
					"default":     defaultLimit,
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
