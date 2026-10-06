// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
)

func dynamicScopeFileEnvelope(repoID, rawPath, relativePath string, functions ...any) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path":      rawPath,
				"lang":      "javascript",
				"functions": functions,
			},
		},
	}
}

// aliasesForTest is the single seam into the cached alias lookup, so the
// regression ran unchanged against the pre-fix signature.
func aliasesForTest(index shared.EntityIndex, repoID, rawPath, relativePath string, line int) (shared.JavaScriptAliasSet, bool) {
	return javaScriptStaticAliasesForCall(index, repoID, rawPath, relativePath, line)
}

// TestJavaScriptStaticAliasesStayInsideTheCallFile proves the dynamic-call
// alias set comes only from a function in the call file's own repository
// and path (#7640), never from a same-named file elsewhere.
func TestJavaScriptStaticAliasesStayInsideTheCallFile(t *testing.T) {
	t.Parallel()

	foreign := map[string]any{
		"name": "run", "uid": "repo-a:run", "line_number": 1, "end_line": 20,
		"source": "function run() {\n  const handlers = { create: createOrder };\n  const { create } = handlers;\n}\n",
	}
	tests := []struct {
		name         string
		envelopes    []facts.Envelope
		repoID       string
		rawPath      string
		relativePath string
		wantOK       bool
	}{
		{
			name: "same bare name in another repository",
			envelopes: []facts.Envelope{
				dynamicScopeFileEnvelope("repo-a", "/repos/repo-a/lib/util.js", "lib/util.js", foreign),
				dynamicScopeFileEnvelope("repo-b", "/repos/repo-b/app/util.js", "app/util.js"),
			},
			repoID: "repo-b", rawPath: "/repos/repo-b/app/util.js", relativePath: "app/util.js",
			wantOK: false,
		},
		{
			name: "same relative path in another repository",
			envelopes: []facts.Envelope{
				dynamicScopeFileEnvelope("repo-a", "/repos/repo-a/src/util.js", "src/util.js", foreign),
				dynamicScopeFileEnvelope("repo-b", "/repos/repo-b/src/util.js", "src/util.js"),
			},
			repoID: "repo-b", rawPath: "/repos/repo-b/src/util.js", relativePath: "src/util.js",
			wantOK: false,
		},
		{
			name: "own file function supplies the alias set",
			envelopes: []facts.Envelope{
				dynamicScopeFileEnvelope("repo-a", "/repos/repo-a/src/util.js", "src/util.js", foreign),
			},
			repoID: "repo-a", rawPath: "/repos/repo-a/src/util.js", relativePath: "src/util.js",
			wantOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			index := shared.BuildEntityIndex(tt.envelopes)
			_, ok := aliasesForTest(index, tt.repoID, tt.rawPath, tt.relativePath, 5)
			if ok != tt.wantOK {
				t.Fatalf("alias set found = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}
