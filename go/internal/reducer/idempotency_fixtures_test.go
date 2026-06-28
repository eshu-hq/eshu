// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/facts"

// Fact fixtures for the B-6 (#3799) idempotency replay suite. Each builder
// returns a static fact set that drives its handler's emit path to exactly one
// canonical projection unit, so the replay assertion has a non-empty,
// deterministic projection to deduplicate. Builders live here, separate from the
// case constructors in idempotency_cases_test.go, to keep both files under the
// repo's 500-line cap.

// codeCallReplayFacts builds a repository plus one caller/callee file pair that
// extracts to exactly one resolved code-call edge.
func codeCallReplayFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: "repository",
			ScopeID:  "scope-cc",
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"source_run_id": "run-a",
				"graph_id":      "repo-a",
				"graph_kind":    "repository",
				"name":          "repo-a",
			},
		},
		{
			FactKind: "file",
			ScopeID:  "scope-cc",
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"relative_path": "caller.py",
				"parsed_file_data": map[string]any{
					"path": "caller.py",
					"functions": []any{
						map[string]any{"name": "handle", "line_number": 3, "uid": "entity:handle"},
					},
					"function_calls_scip": []any{
						map[string]any{
							"caller_file":   "caller.py",
							"caller_line":   3,
							"caller_symbol": "pkg/caller#handle().",
							"callee_file":   "callee.py",
							"callee_line":   1,
							"callee_symbol": "pkg/callee#callee().",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			ScopeID:  "scope-cc",
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"relative_path": "callee.py",
				"parsed_file_data": map[string]any{
					"path": "callee.py",
					"functions": []any{
						map[string]any{"name": "callee", "line_number": 1, "uid": "entity:callee"},
					},
				},
			},
		},
	}
}

// shellExecReplayFacts builds a repository plus one file with one embedded shell
// command that extracts to exactly one EXECUTES_SHELL edge.
func shellExecReplayFacts() []facts.Envelope {
	return []facts.Envelope{
		sqlRelationshipRepositoryEnvelope(false, nil),
		{
			FactKind: factKindFile,
			ScopeID:  "scope-db",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"relative_path": "cmd/archive/main.go",
				"path":          "/repo/cmd/archive/main.go",
				"parsed_file_data": map[string]any{
					"functions": []any{
						map[string]any{"name": "runArchive", "line_number": 7, "uid": "function:runArchive"},
					},
					"embedded_shell_commands": []any{
						map[string]any{
							"function_name":        "runArchive",
							"function_line_number": 7,
							"line_number":          8,
							"api":                  "os/exec.CommandContext",
							"language":             "go",
						},
					},
				},
			},
		},
	}
}

// semanticEntityReplayFacts builds a repository plus one annotation content
// entity that extracts to exactly one canonical semantic node.
func semanticEntityReplayFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: "repository",
			ScopeID:  "scope-se",
			Payload:  map[string]any{"repo_id": "repo-1"},
		},
		{
			FactKind:  "content_entity",
			ScopeID:   "scope-se",
			SourceRef: facts.Ref{SourceURI: "/repo/src/Logged.java", SourceSystem: "git"},
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "annotation-1",
				"relative_path": "src/Logged.java",
				"entity_type":   "Annotation",
				"entity_name":   "Logged",
				"language":      "java",
				"start_line":    12,
				"end_line":      12,
				"entity_metadata": map[string]any{
					"kind":        "applied",
					"target_kind": "method_declaration",
				},
			},
		},
	}
}
