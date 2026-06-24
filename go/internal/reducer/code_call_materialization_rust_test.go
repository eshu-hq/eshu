// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesRustImplScopedCallsUsingImplContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "main.rs")
	calleePath := filepath.Join(repoRoot, "impl_blocks.rs")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-rust",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-rust",
				"relative_path": "main.rs",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "main",
							"line_number": 1,
							"end_line":    5,
							"uid":         "content-entity:rust-main",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "new",
							"full_name":   "Point::new",
							"line_number": 3,
							"lang":        "rust",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-rust",
				"relative_path": "impl_blocks.rs",
				"parsed_file_data": map[string]any{
					"path": calleePath,
					"functions": []any{
						map[string]any{
							"name":         "new",
							"impl_context": "Point",
							"line_number":  2,
							"end_line":     4,
							"uid":          "content-entity:rust-point-new",
						},
					},
				},
			},
		},
	}

	repoIDs, rows := ExtractCodeCallRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-rust" {
		t.Fatalf("repoIDs = %v, want [repo-rust]", repoIDs)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["caller_entity_id"], "content-entity:rust-main"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:rust-point-new"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_file"], "impl_blocks.rs"; got != want {
		t.Fatalf("callee_file = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesRustTraitBoundReceiverCalls(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-rust"},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-rust",
				"relative_path": "src/lib.rs",
				"parsed_file_data": map[string]any{
					"path": "src/lib.rs",
					"functions": []any{
						map[string]any{"uid": "fn-area", "name": "area", "line_number": 2, "end_line": 2, "lang": "rust", "trait_context": "Area"},
						map[string]any{"uid": "fn-draw-area", "name": "area", "line_number": 6, "end_line": 6, "lang": "rust", "trait_context": "Draw"},
						map[string]any{"uid": "fn-compare", "name": "compare", "line_number": 10, "end_line": 14, "lang": "rust", "where_predicates": []any{"T: Area"}},
					},
					"function_calls": []any{
						map[string]any{"name": "area", "full_name": "shape.area", "line_number": 13, "lang": "rust", "inferred_obj_type": "T"},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertCodeCallRow(t, rows, "fn-compare", "fn-area")
	assertNoRustCodeCallRow(t, rows, "fn-compare", "fn-draw-area")
	if got := resolutionMethodForCallee(t, rows, "fn-area"); got != codeprovenance.MethodTypeInferred {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodTypeInferred)
	}
}

func TestExtractCodeCallRowsLeavesRustAmbiguousTraitBoundReceiverCallsUnresolved(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-rust"},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-rust",
				"relative_path": "src/lib.rs",
				"parsed_file_data": map[string]any{
					"path": "src/lib.rs",
					"functions": []any{
						map[string]any{"uid": "fn-area", "name": "area", "line_number": 2, "end_line": 2, "lang": "rust", "trait_context": "Area"},
						map[string]any{"uid": "fn-surface-area", "name": "area", "line_number": 6, "end_line": 6, "lang": "rust", "trait_context": "Surface"},
						map[string]any{"uid": "fn-compare", "name": "compare", "line_number": 10, "end_line": 14, "lang": "rust", "where_predicates": []any{"T: Area + Surface"}},
					},
					"function_calls": []any{
						map[string]any{"name": "area", "full_name": "shape.area", "line_number": 13, "lang": "rust", "inferred_obj_type": "T"},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertNoRustCodeCallRow(t, rows, "fn-compare", "fn-area")
	assertNoRustCodeCallRow(t, rows, "fn-compare", "fn-surface-area")
}

func TestExtractCodeCallRowsLeavesRustAssociatedTypeBoundsUnresolved(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-rust"},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-rust",
				"relative_path": "src/lib.rs",
				"parsed_file_data": map[string]any{
					"path": "src/lib.rs",
					"functions": []any{
						map[string]any{"uid": "fn-area", "name": "area", "line_number": 2, "end_line": 2, "lang": "rust", "trait_context": "Area"},
						map[string]any{"uid": "fn-compare", "name": "compare", "line_number": 10, "end_line": 14, "lang": "rust", "where_predicates": []any{"T::Item: Area"}},
					},
					"function_calls": []any{
						map[string]any{"name": "area", "full_name": "shape.area", "line_number": 13, "lang": "rust", "inferred_obj_type": "T"},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertNoRustCodeCallRow(t, rows, "fn-compare", "fn-area")
}

func assertNoRustCodeCallRow(t *testing.T, rows []map[string]any, callerID string, calleeID string) {
	t.Helper()
	for _, row := range rows {
		if anyToString(row["caller_entity_id"]) == callerID && anyToString(row["callee_entity_id"]) == calleeID {
			t.Fatalf("unexpected code-call row %s -> %s in %#v", callerID, calleeID, rows)
		}
	}
}
