// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// containmentFileEnvelope builds one "file" fact whose parsed_file_data
// carries the given function declarations. rawPath is the parser's checkout
// path (absolute in production) and relativePath is the repo-relative path.
func containmentFileEnvelope(repoID, rawPath, relativePath string, functions ...map[string]any) facts.Envelope {
	items := make([]any, 0, len(functions))
	for _, function := range functions {
		items = append(items, function)
	}
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path":      rawPath,
				"functions": items,
			},
		},
	}
}

func containmentFunction(name, uid string, startLine, endLine int) map[string]any {
	return map[string]any{"name": name, "uid": uid, "line_number": startLine, "end_line": endLine}
}

// resolveContainingForTest is the single seam the table below calls, so the
// regression ran unchanged against the pre-fix signature.
func resolveContainingForTest(index EntityIndex, repoID, rawPath, relativePath string, line int) string {
	return ResolveContainingEntityID(index, repoID, rawPath, relativePath, line)
}

// TestResolveContainingEntityIDStaysInsideTheCallFile is the #7640 regression
// table: a call's containing span must come only from the call file's own
// (repository, full path) or (repository, relative path) identity, never from
// a same-named file in another directory or another repository.
func TestResolveContainingEntityIDStaysInsideTheCallFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		envelopes    []facts.Envelope
		repoID       string
		rawPath      string
		relativePath string
		line         int
		want         string
	}{
		{
			name: "same bare name in another repository with a different relative path",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/lib/util.ts", "lib/util.ts",
					containmentFunction("outer", "repo-a:outer", 1, 20)),
				containmentFileEnvelope("repo-b", "/repos/repo-b/app/util.ts", "app/util.ts",
					containmentFunction("local", "repo-b:local", 30, 35)),
			},
			repoID:       "repo-b",
			rawPath:      "/repos/repo-b/app/util.ts",
			relativePath: "app/util.ts",
			line:         5,
			want:         "",
		},
		{
			name: "same relative path in another repository",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/index.ts", "src/index.ts",
					containmentFunction("outer", "repo-a:outer", 1, 20)),
				containmentFileEnvelope("repo-b", "/repos/repo-b/src/index.ts", "src/index.ts"),
			},
			repoID:       "repo-b",
			rawPath:      "/repos/repo-b/src/index.ts",
			relativePath: "src/index.ts",
			line:         5,
			want:         "",
		},
		{
			name: "same bare name in another directory of the same repository",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/a/index.ts", "src/a/index.ts",
					containmentFunction("outer", "repo-a:a-outer", 1, 20)),
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/b/index.ts", "src/b/index.ts"),
			},
			repoID:       "repo-a",
			rawPath:      "/repos/repo-a/src/b/index.ts",
			relativePath: "src/b/index.ts",
			line:         5,
			want:         "",
		},
		{
			name: "zero-width span in a same-named file of another repository",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/lib/helper.php", "lib/helper.php",
					containmentFunction("helper", "repo-a:helper", 5, 5)),
				containmentFileEnvelope("repo-b", "/repos/repo-b/web/helper.php", "web/helper.php"),
			},
			repoID:       "repo-b",
			rawPath:      "/repos/repo-b/web/helper.php",
			relativePath: "web/helper.php",
			line:         5,
			want:         "",
		},
		{
			name: "nested call resolves to the narrowest span in its own file",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/util.ts", "src/util.ts",
					containmentFunction("outer", "repo-a:outer", 1, 20),
					containmentFunction("inner", "repo-a:inner", 5, 10)),
				containmentFileEnvelope("repo-b", "/repos/repo-b/src/util.ts", "src/util.ts",
					containmentFunction("tiny", "repo-b:tiny", 6, 8)),
			},
			repoID:       "repo-a",
			rawPath:      "/repos/repo-a/src/util.ts",
			relativePath: "src/util.ts",
			line:         7,
			want:         "repo-a:inner",
		},
		{
			name: "call outside the nested span resolves to the enclosing span",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/util.ts", "src/util.ts",
					containmentFunction("outer", "repo-a:outer", 1, 20),
					containmentFunction("inner", "repo-a:inner", 5, 10)),
			},
			repoID:       "repo-a",
			rawPath:      "/repos/repo-a/src/util.ts",
			relativePath: "src/util.ts",
			line:         15,
			want:         "repo-a:outer",
		},
		{
			name: "two same-named files in one repository resolve to the call file only",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/a/util.ts", "src/a/util.ts",
					containmentFunction("run", "repo-a:a-run", 1, 20)),
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/b/util.ts", "src/b/util.ts",
					containmentFunction("run", "repo-a:b-run", 1, 20)),
			},
			repoID:       "repo-a",
			rawPath:      "/repos/repo-a/src/b/util.ts",
			relativePath: "src/b/util.ts",
			line:         5,
			want:         "repo-a:b-run",
		},
		{
			name: "same file indexed under one checkout path resolves through its relative path",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/checkout-1/src/x.ts", "src/x.ts",
					containmentFunction("run", "repo-a:run", 1, 20)),
			},
			repoID:       "repo-a",
			rawPath:      "/checkout-2/src/x.ts",
			relativePath: "src/x.ts",
			line:         5,
			want:         "repo-a:run",
		},
		{
			name: "same relative path under another checkout path does not leak across repositories",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/checkout-1/src/x.ts", "src/x.ts",
					containmentFunction("run", "repo-a:run", 1, 20)),
			},
			repoID:       "repo-b",
			rawPath:      "/checkout-2/src/x.ts",
			relativePath: "src/x.ts",
			line:         5,
			want:         "",
		},
		{
			name: "relative-only path still resolves within its repository",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "src/util.ts", "src/util.ts",
					containmentFunction("run", "repo-a:run", 1, 20)),
			},
			repoID:       "repo-a",
			rawPath:      "",
			relativePath: "src/util.ts",
			line:         5,
			want:         "repo-a:run",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			index := BuildEntityIndex(tt.envelopes)
			got := resolveContainingForTest(index, tt.repoID, tt.rawPath, tt.relativePath, tt.line)
			if got != tt.want {
				t.Fatalf("ResolveContainingEntityID(%q, %q, %q, %d) = %q, want %q",
					tt.repoID, tt.rawPath, tt.relativePath, tt.line, got, tt.want)
			}
		})
	}
}
