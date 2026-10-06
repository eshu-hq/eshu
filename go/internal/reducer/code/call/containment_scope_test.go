// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
)

// scopeFileEnvelope builds one "file" fact for the #7640 containment tests.
// rawPath is the parser's checkout path and relativePath the repo-relative
// path; functions and calls ride in parsed_file_data verbatim.
func scopeFileEnvelope(repoID, rawPath, relativePath string, functions []any, calls []any) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path":           rawPath,
				"functions":      functions,
				"function_calls": calls,
			},
		},
	}
}

func scopeFunction(name, uid string, startLine, endLine int) map[string]any {
	return map[string]any{"name": name, "uid": uid, "line_number": startLine, "end_line": endLine}
}

// resolveScopedForTest is the single seam into the same-file scoped callee
// lookup, so the regression ran unchanged against the pre-fix signature.
func resolveScopedForTest(index shared.EntityIndex, repoID, rawPath, relativePath string, call map[string]any, line int) string {
	return resolveSameFileScopedCalleeEntityID(index, repoID, rawPath, relativePath, call, line)
}

// TestSameFileScopedCalleeStaysInsideTheCallFile proves the scoped callee
// lookup finds the CALLER span only in the call file's own repository and
// path (#7640). A top-level call in one file must not borrow the caller span
// of a same-named file elsewhere and then pick a callee nested inside it.
func TestSameFileScopedCalleeStaysInsideTheCallFile(t *testing.T) {
	t.Parallel()

	foreignFunctions := []any{
		scopeFunction("outer", "repo-a:outer", 1, 20),
		scopeFunction("helper", "repo-a:helper", 5, 10),
	}
	call := map[string]any{"name": "helper", "full_name": "helper", "lang": "typescript", "line_number": 15}

	tests := []struct {
		name         string
		envelopes    []facts.Envelope
		repoID       string
		rawPath      string
		relativePath string
		want         string
	}{
		{
			name: "same bare name in another repository",
			envelopes: []facts.Envelope{
				scopeFileEnvelope("repo-a", "/repos/repo-a/lib/util.ts", "lib/util.ts", foreignFunctions, nil),
				scopeFileEnvelope("repo-b", "/repos/repo-b/app/util.ts", "app/util.ts", nil, []any{call}),
			},
			repoID:       "repo-b",
			rawPath:      "/repos/repo-b/app/util.ts",
			relativePath: "app/util.ts",
			want:         "",
		},
		{
			name: "same relative path in another repository",
			envelopes: []facts.Envelope{
				scopeFileEnvelope("repo-a", "/repos/repo-a/src/util.ts", "src/util.ts", foreignFunctions, nil),
				scopeFileEnvelope("repo-b", "/repos/repo-b/src/util.ts", "src/util.ts", nil, []any{call}),
			},
			repoID:       "repo-b",
			rawPath:      "/repos/repo-b/src/util.ts",
			relativePath: "src/util.ts",
			want:         "",
		},
		{
			name: "own file nested callee still resolves",
			envelopes: []facts.Envelope{
				scopeFileEnvelope("repo-a", "/repos/repo-a/src/util.ts", "src/util.ts", foreignFunctions, []any{call}),
			},
			repoID:       "repo-a",
			rawPath:      "/repos/repo-a/src/util.ts",
			relativePath: "src/util.ts",
			want:         "repo-a:helper",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			index := shared.BuildEntityIndex(tt.envelopes)
			got := resolveScopedForTest(index, tt.repoID, tt.rawPath, tt.relativePath, call, 15)
			if got != tt.want {
				t.Fatalf("scoped callee = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestExtractRowsDropsCallerFromAnotherRepositorysSameNamedFile is the
// row-level #7640 regression: a top-level (non-JavaScript) call must not
// produce a CALLS row whose caller is a function in a same-named file of
// another repository, while an in-function call in that other file still
// yields its own row.
func TestExtractRowsDropsCallerFromAnotherRepositorysSameNamedFile(t *testing.T) {
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
					},
					[]any{map[string]any{"name": "other", "full_name": "other", "lang": "python", "line_number": 5}},
				),
				scopeFileEnvelope("repo-b", "/repos/repo-b/"+tt.pathB, tt.pathB,
					[]any{scopeFunction("helper", "repo-b:helper", 10, 12)},
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
				}
				if callee == "repo-b:helper" {
					t.Fatalf("top-level repo-b call produced row %v; caller must not come from another repository's file", row)
				}
			}
			if !foundOwn {
				t.Fatalf("missing repo-a in-function row repo-a:run -> repo-a:other in %v", rows)
			}
		})
	}
}

// TestExtractRelationshipsCountsUnresolvedCallers proves the operator counter
// for #7640: a call whose callee resolves but whose caller has no containing
// entity in its own file is counted, and an in-function call is not.
func TestExtractRelationshipsCountsUnresolvedCallers(t *testing.T) {
	t.Parallel()

	result := ExtractRelationships([]facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
		scopeFileEnvelope("repo-a", "/repos/repo-a/lib/util.py", "lib/util.py",
			[]any{
				scopeFunction("run", "repo-a:run", 1, 20),
				scopeFunction("other", "repo-a:other", 30, 32),
			},
			[]any{map[string]any{"name": "other", "full_name": "other", "lang": "python", "line_number": 5}},
		),
		scopeFileEnvelope("repo-b", "/repos/repo-b/app/util.py", "app/util.py",
			[]any{scopeFunction("helper", "repo-b:helper", 10, 12)},
			[]any{map[string]any{"name": "helper", "full_name": "helper", "lang": "python", "line_number": 5}},
		),
	})

	if got, want := result.UnresolvedCallerCount, 1; got != want {
		t.Fatalf("UnresolvedCallerCount = %d, want %d", got, want)
	}
	if got, want := len(result.CodeCallRows), 1; got != want {
		t.Fatalf("len(CodeCallRows) = %d, want %d: %v", got, want, result.CodeCallRows)
	}
}
