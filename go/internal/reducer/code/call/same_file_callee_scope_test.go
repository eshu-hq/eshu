// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractRowsDropsForeignSameFileCallee is the row-level #7642
// regression: an in-function call whose callee name is defined only in a
// same-named file of another repository must not produce a CALLS row, while
// an in-function call to an own-file callee still yields its row with
// resolution_method same_file.
func TestExtractRowsDropsForeignSameFileCallee(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		pathA string
		pathB string
	}{
		{name: "same bare name, different relative path", pathA: "lib/util.py", pathB: "app/util.py"},
		{name: "same relative path", pathA: "src/util.py", pathB: "src/util.py"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			envelopes := []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
				{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
				scopeFileEnvelope("repo-a", "/repos/repo-a/"+tt.pathA, tt.pathA,
					[]any{
						scopeFunction("run", "repo-a:run", 1, 20),
						scopeFunction("other", "repo-a:other", 30, 32),
						scopeFunction("helper", "repo-a:helper", 40, 42),
					},
					[]any{map[string]any{"name": "other", "full_name": "other", "lang": "python", "line_number": 5}},
				),
				scopeFileEnvelope("repo-b", "/repos/repo-b/"+tt.pathB, tt.pathB,
					[]any{
						scopeFunction("main", "repo-b:main", 1, 20),
					},
					[]any{map[string]any{"name": "helper", "full_name": "helper", "lang": "python", "line_number": 5}},
				),
			}

			_, rows := ExtractRows(envelopes)

			foundOwn := false
			for _, row := range rows {
				caller := row["caller_entity_id"]
				callee := row["callee_entity_id"]
				if caller == "repo-a:run" && callee == "repo-a:other" {
					foundOwn = true
					if method := row["resolution_method"]; method != "same_file" {
						t.Errorf("own-file row resolution_method = %v, want same_file", method)
					}
				}
				if callee == "repo-a:helper" || caller == "repo-b:main" {
					t.Fatalf("repo-b call to foreign-only callee produced row %v; same_file callee must stay in the call file", row)
				}
			}
			if !foundOwn {
				t.Fatalf("missing repo-a own-file row repo-a:run -> repo-a:other in %v", rows)
			}
		})
	}
}

// TestExtractRowsDropsSCIPCalleeFromAnotherRepositorysSameNamedFile is the
// SCIP row-level #7642 regression: a callee path/line with no declaration in
// its own file must not resolve to a same-named file's declaration at that
// line in another repository.
func TestExtractRowsDropsSCIPCalleeFromAnotherRepositorysSameNamedFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		pathA string
		pathB string
	}{
		{name: "same bare name, different relative path", pathA: "lib/util.js", pathB: "app/util.js"},
		{name: "same relative path", pathA: "src/util.js", pathB: "src/util.js"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rawA := "/repos/repo-a/" + tt.pathA
			rawB := "/repos/repo-b/" + tt.pathB
			envelopes := []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
				{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
				{
					FactKind: "file",
					Payload: map[string]any{
						"repo_id":       "repo-a",
						"relative_path": tt.pathA,
						"parsed_file_data": map[string]any{
							"path":      rawA,
							"functions": []any{scopeFunction("helper", "repo-a:helper", 10, 12)},
						},
					},
				},
				{
					FactKind: "file",
					Payload: map[string]any{
						"repo_id":       "repo-b",
						"relative_path": tt.pathB,
						"parsed_file_data": map[string]any{
							"path":      rawB,
							"functions": []any{scopeFunction("main", "repo-b:main", 1, 20)},
							"function_calls_scip": []any{
								map[string]any{
									"caller_file": rawB,
									"caller_line": 1,
									"callee_file": rawB,
									"callee_line": 10,
									"ref_line":    5,
								},
							},
						},
					},
				},
			}

			_, rows := ExtractRows(envelopes)

			for _, row := range rows {
				if row["callee_entity_id"] == "repo-a:helper" {
					t.Fatalf("SCIP callee at an undeclared line produced row %v; path/line callee must stay in the indexed file", row)
				}
			}
			if len(rows) != 0 {
				t.Fatalf("len(rows) = %d, want 0: %v", len(rows), rows)
			}
		})
	}
}

// TestExtractRowsKeepsSCIPCalleeInItsOwnFile is the positive control for the
// SCIP #7642 regression: an exact path/line hit in the edge's own file still
// resolves.
func TestExtractRowsKeepsSCIPCalleeInItsOwnFile(t *testing.T) {
	t.Parallel()

	rawB := "/repos/repo-b/app/util.js"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"relative_path": "lib/util.js",
				"parsed_file_data": map[string]any{
					"path":      "/repos/repo-a/lib/util.js",
					"functions": []any{scopeFunction("helper", "repo-a:helper", 10, 12)},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-b",
				"relative_path": "app/util.js",
				"parsed_file_data": map[string]any{
					"path": rawB,
					"functions": []any{
						scopeFunction("main", "repo-b:main", 1, 20),
						scopeFunction("other", "repo-b:other", 10, 12),
					},
					"function_calls_scip": []any{
						map[string]any{
							"caller_file": rawB,
							"caller_line": 1,
							"callee_file": rawB,
							"callee_line": 10,
							"ref_line":    5,
						},
					},
				},
			},
		},
	}

	_, rows := ExtractRows(envelopes)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %v", len(rows), rows)
	}
	if got := rows[0]["callee_entity_id"]; got != "repo-b:other" {
		t.Fatalf("callee_entity_id = %v, want repo-b:other", got)
	}
}

// TestExtractRelationshipsCountsUnresolvedCallees proves the operator counter
// for #7642: a call whose callee has no declaration in the call file's own
// repository and path is counted, and an in-function call to an own-file
// callee is not.
func TestExtractRelationshipsCountsUnresolvedCallees(t *testing.T) {
	t.Parallel()

	result := ExtractRelationships([]facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
		scopeFileEnvelope("repo-a", "/repos/repo-a/lib/util.py", "lib/util.py",
			[]any{
				scopeFunction("run", "repo-a:run", 1, 20),
				scopeFunction("other", "repo-a:other", 30, 32),
				scopeFunction("helper", "repo-a:helper", 40, 42),
			},
			[]any{map[string]any{"name": "other", "full_name": "other", "lang": "python", "line_number": 5}},
		),
		scopeFileEnvelope("repo-b", "/repos/repo-b/app/util.py", "app/util.py",
			[]any{
				scopeFunction("main", "repo-b:main", 1, 20),
			},
			[]any{map[string]any{"name": "helper", "full_name": "helper", "lang": "python", "line_number": 5}},
		),
	})

	if got, want := result.UnresolvedCalleeCount, 1; got != want {
		t.Fatalf("UnresolvedCalleeCount = %d, want %d", got, want)
	}
	if got, want := result.UnresolvedCallerCount, 0; got != want {
		t.Fatalf("UnresolvedCallerCount = %d, want %d", got, want)
	}
	if got, want := len(result.CodeCallRows), 1; got != want {
		t.Fatalf("len(CodeCallRows) = %d, want %d: %v", got, want, result.CodeCallRows)
	}
}
