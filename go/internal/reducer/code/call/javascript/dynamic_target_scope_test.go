// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
)

// dynamicTargetScopeCall is a static-named call that reaches the same-file
// dynamic-target lookup with no alias rewrite.
func dynamicTargetScopeCall() map[string]any {
	return map[string]any{"name": "createOrder", "lang": "javascript", "line_number": 5}
}

// TestResolveDynamicCalleeTargetStaysInsideTheCallFile proves the same-file
// dynamic-call target comes only from the call file's own repository and
// path (#7642), never from a same-named file elsewhere.
func TestResolveDynamicCalleeTargetStaysInsideTheCallFile(t *testing.T) {
	t.Parallel()

	foreign := map[string]any{
		"name": "createOrder", "uid": "repo-a:createOrder", "line_number": 10, "end_line": 12,
	}
	local := map[string]any{
		"name": "run", "uid": "repo-b:run", "line_number": 1, "end_line": 20,
		"source": "function run() {}\n",
	}
	ownTarget := map[string]any{
		"name": "createOrder", "uid": "repo-b:createOrder", "line_number": 10, "end_line": 12,
	}

	tests := []struct {
		name         string
		envelopes    []facts.Envelope
		repoID       string
		rawPath      string
		relativePath string
		fileData     map[string]any
		want         string
	}{
		{
			name: "same bare name in another repository",
			envelopes: []facts.Envelope{
				dynamicScopeFileEnvelope("repo-a", "/repos/repo-a/lib/util.js", "lib/util.js", foreign),
				dynamicScopeFileEnvelope("repo-b", "/repos/repo-b/app/util.js", "app/util.js", local),
			},
			repoID: "repo-b", rawPath: "/repos/repo-b/app/util.js", relativePath: "app/util.js",
			fileData: map[string]any{
				"path": "/repos/repo-b/app/util.js", "lang": "javascript",
				"functions": []any{local},
			},
			want: "",
		},
		{
			name: "same relative path in another repository",
			envelopes: []facts.Envelope{
				dynamicScopeFileEnvelope("repo-a", "/repos/repo-a/src/util.js", "src/util.js", foreign),
				dynamicScopeFileEnvelope("repo-b", "/repos/repo-b/src/util.js", "src/util.js", local),
			},
			repoID: "repo-b", rawPath: "/repos/repo-b/src/util.js", relativePath: "src/util.js",
			fileData: map[string]any{
				"path": "/repos/repo-b/src/util.js", "lang": "javascript",
				"functions": []any{local},
			},
			want: "",
		},
		{
			name: "own file target still resolves",
			envelopes: []facts.Envelope{
				dynamicScopeFileEnvelope("repo-a", "/repos/repo-a/lib/util.js", "lib/util.js", foreign),
				dynamicScopeFileEnvelope("repo-b", "/repos/repo-b/app/util.js", "app/util.js", local, ownTarget),
			},
			repoID: "repo-b", rawPath: "/repos/repo-b/app/util.js", relativePath: "app/util.js",
			fileData: map[string]any{
				"path": "/repos/repo-b/app/util.js", "lang": "javascript",
				"functions": []any{local, ownTarget},
			},
			want: "repo-b:createOrder",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			index := shared.BuildEntityIndex(tt.envelopes)
			got := ResolveDynamicCallee(index, tt.repoID, tt.rawPath, tt.relativePath, tt.fileData, dynamicTargetScopeCall())
			if got != tt.want {
				t.Fatalf("ResolveDynamicCallee() = %q, want %q", got, tt.want)
			}
		})
	}
}
