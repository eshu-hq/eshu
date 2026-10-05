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
					"default":     DefaultLimit,
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
					"default":     DefaultLimit,
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
		Description: "Find dead-code candidates across an explicit producer repository and classify symbols kept live by deterministic consumer repository evidence. Ambiguous ownership or missing evidence, including a consumer repository with no reachability watermark, a truncated one, or an older-epoch one (consumer_coverage_incomplete), is returned as unknown instead of dead. consumer_coverage.incomplete names each such repository with a state (no_snapshot_yet and older_epoch: a snapshot is expected for this generation, not guaranteed, and it can stay missing for a long time on a delta generation or a full generation whose reducer work did not complete; truncated and no_active_scope: not expected), the generation_id, a retryable hint, and a plain-language reason and next_step; consumer_coverage.coverage_summary is one sentence over the listed gaps (it counts only what is listed, so a cut list says at least N); consumer_coverage.retryable is true only when every gap is retryable and the list was not cut. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected. A live_by_consumer row carries test_only_consumers: true when every consumer root is in a test file (liveness is unchanged, a test caller is a caller; the flag is absent otherwise, and it can fire only for C#, Java, Kotlin, Scala, Rust and Swift, where a test method is a reachability root).",
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
					"default":     DefaultLimit,
					"maximum":     500,
				},
				"exclude_decorated_with": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Decorator names to suppress from active candidates",
					"default":     []any{},
				},
				"evidence_detail": map[string]any{
					"type": "string",
					"enum": []string{"full", "handles"},
					// The default depends on the transport (handles on MCP, full on HTTP),
					// which one JSON-Schema default cannot say, so none is advertised.
					"description": "Row detail for consumer evidence. handles (the default for this tool) replaces each row's consumer_evidence items with at most 5 groups {consumer_repo_id, relationship_type, evidence_family, confidence_label, item_count}, strongest first so the group that decided the bucket leads, and caps the shared boundary_consumer_evidence list at 25; consumer_evidence_count, consumer_evidence_group_count, boundary_consumer_evidence_count and the *_truncated markers keep the totals, and truth.omissions names each reduced section. Buckets, reasons and analysis are identical in both modes. Evidence under handles is bounded to at most 17.5% of the response budget, but the row base is not: each docstring is clipped to 512 bytes yet echoed about six times per row, so on a repository with long docstrings the reply can still be delivered as the full resource only (structuredContent omitted) or exceed the budget. Repeat the call with full for every evidence item (citation, entity id, generation); full can exceed the response budget on a populated repository, so narrow with consumer_repo_ids and limit.",
				},
			},
			"required": []string{"repo_id"},
		},
	}
}
