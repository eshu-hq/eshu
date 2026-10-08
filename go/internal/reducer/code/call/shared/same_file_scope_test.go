// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// resolveSameFileCalleeForTest is the single seam into the same-file callee
// lookup, so the table below runs against the repository-scoped signature.
func resolveSameFileCalleeForTest(index EntityIndex, repoID, rawPath, relativePath string, call map[string]any) string {
	return ResolveSameFileCalleeEntityID(index, repoID, rawPath, relativePath, call)
}

// TestResolveSameFileCalleeStaysInsideTheCallFile is the #7642 regression
// table: a same-file callee must come only from the call file's own
// (repository, full path) or (repository, relative path) identity, never
// from a same-named file in another directory or another repository.
func TestResolveSameFileCalleeStaysInsideTheCallFile(t *testing.T) {
	t.Parallel()

	callFor := func(name string) map[string]any {
		return map[string]any{"name": name, "full_name": name, "lang": "javascript"}
	}

	tests := []struct {
		name         string
		envelopes    []facts.Envelope
		repoID       string
		rawPath      string
		relativePath string
		call         map[string]any
		want         string
	}{
		{
			name: "same bare name in another repository with a different relative path",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/lib/util.js", "lib/util.js",
					containmentFunction("helper", "repo-a:helper", 1, 10)),
				containmentFileEnvelope("repo-b", "/repos/repo-b/app/util.js", "app/util.js",
					containmentFunction("main", "repo-b:main", 1, 20)),
			},
			repoID: "repo-b", rawPath: "/repos/repo-b/app/util.js", relativePath: "app/util.js",
			call: callFor("helper"),
			want: "",
		},
		{
			name: "same relative path in another repository",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/index.js", "src/index.js",
					containmentFunction("helper", "repo-a:helper", 1, 10)),
				containmentFileEnvelope("repo-b", "/repos/repo-b/src/index.js", "src/index.js",
					containmentFunction("main", "repo-b:main", 1, 20)),
			},
			repoID: "repo-b", rawPath: "/repos/repo-b/src/index.js", relativePath: "src/index.js",
			call: callFor("helper"),
			want: "",
		},
		{
			name: "same bare name in another directory of the same repository",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/a/util.js", "src/a/util.js",
					containmentFunction("helper", "repo-a:a-helper", 1, 10)),
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/b/util.js", "src/b/util.js",
					containmentFunction("main", "repo-a:b-main", 1, 20)),
			},
			repoID: "repo-a", rawPath: "/repos/repo-a/src/b/util.js", relativePath: "src/b/util.js",
			call: callFor("helper"),
			want: "",
		},
		{
			name: "own file callee still resolves beside a same-named foreign file",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/lib/util.js", "lib/util.js",
					containmentFunction("helper", "repo-a:helper", 1, 10)),
				containmentFileEnvelope("repo-b", "/repos/repo-b/app/util.js", "app/util.js",
					containmentFunction("main", "repo-b:main", 1, 20),
					containmentFunction("helper", "repo-b:helper", 30, 35)),
			},
			repoID: "repo-b", rawPath: "/repos/repo-b/app/util.js", relativePath: "app/util.js",
			call: callFor("helper"),
			want: "repo-b:helper",
		},
		{
			name: "same file indexed under one checkout path resolves through its relative path",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/checkout-1/src/x.js", "src/x.js",
					containmentFunction("helper", "repo-a:helper", 1, 10)),
			},
			repoID: "repo-a", rawPath: "/checkout-2/src/x.js", relativePath: "src/x.js",
			call: callFor("helper"),
			want: "repo-a:helper",
		},
		{
			name: "same relative path under another checkout path does not leak across repositories",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/checkout-1/src/x.js", "src/x.js",
					containmentFunction("helper", "repo-a:helper", 1, 10)),
			},
			repoID: "repo-b", rawPath: "/checkout-2/src/x.js", relativePath: "src/x.js",
			call: callFor("helper"),
			want: "",
		},
		{
			name: "ambiguous name in the call file resolves to nothing",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/lib/util.js", "lib/util.js",
					containmentFunction("helper", "repo-a:helper", 1, 10)),
				containmentFileEnvelope("repo-b", "/repos/repo-b/app/util.js", "app/util.js",
					containmentFunction("helper", "repo-b:helper-one", 1, 10),
					containmentFunction("helper", "repo-b:helper-two", 30, 35)),
			},
			repoID: "repo-b", rawPath: "/repos/repo-b/app/util.js", relativePath: "app/util.js",
			call: callFor("helper"),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			index := BuildEntityIndex(tt.envelopes)
			got := resolveSameFileCalleeForTest(index, tt.repoID, tt.rawPath, tt.relativePath, tt.call)
			if got != tt.want {
				t.Fatalf("ResolveSameFileCalleeEntityID(%q, %q, %q) = %q, want %q",
					tt.repoID, tt.rawPath, tt.relativePath, got, tt.want)
			}
		})
	}
}

// resolveEntityForTest is the single seam into the path/line lookup, so the
// table below runs against the repository-scoped signature.
func resolveEntityForTest(index EntityIndex, repoID string, pathValue any, lineValue any) string {
	return ResolveEntityID(index, repoID, pathValue, lineValue)
}

// TestResolveEntityIDStaysInsideTheIndexedFile is the #7642 regression
// table for the path/line lookup: an entity resolves only under its own
// (repository, full path) identity, never through a bare file name shared
// with a same-named file elsewhere.
func TestResolveEntityIDStaysInsideTheIndexedFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		envelopes []facts.Envelope
		repoID    string
		path      string
		line      int
		want      string
	}{
		{
			name: "same bare name in another repository with a different relative path",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/lib/util.js", "lib/util.js",
					containmentFunction("helper", "repo-a:helper", 10, 12)),
			},
			repoID: "repo-b",
			path:   "/repos/repo-b/app/util.js",
			line:   10,
			want:   "",
		},
		{
			name: "same relative path in another repository",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/x.js", "src/x.js",
					containmentFunction("helper", "repo-a:helper", 10, 12)),
			},
			repoID: "repo-b",
			path:   "/repos/repo-b/src/x.js",
			line:   10,
			want:   "",
		},
		{
			name: "same checkout path in another repository",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/checkout/src/x.js", "src/x.js",
					containmentFunction("helper", "repo-a:helper", 10, 12)),
			},
			repoID: "repo-b",
			path:   "/checkout/src/x.js",
			line:   10,
			want:   "",
		},
		{
			name: "exact path and line still resolves",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/lib/util.js", "lib/util.js",
					containmentFunction("helper", "repo-a:helper", 10, 12)),
				containmentFileEnvelope("repo-b", "/repos/repo-b/app/util.js", "app/util.js",
					containmentFunction("main", "repo-b:main", 1, 20)),
			},
			repoID: "repo-b",
			path:   "/repos/repo-b/app/util.js",
			line:   1,
			want:   "repo-b:main",
		},
		{
			name: "relative-only path still resolves within its repository",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "src/util.js", "src/util.js",
					containmentFunction("run", "repo-a:run", 1, 20)),
			},
			repoID: "repo-a",
			path:   "src/util.js",
			line:   1,
			want:   "repo-a:run",
		},
		{
			name: "line with no declaration resolves to nothing",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/lib/util.js", "lib/util.js",
					containmentFunction("helper", "repo-a:helper", 10, 12)),
			},
			repoID: "repo-a",
			path:   "/repos/repo-a/lib/util.js",
			line:   99,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			index := BuildEntityIndex(tt.envelopes)
			got := resolveEntityForTest(index, tt.repoID, tt.path, tt.line)
			if got != tt.want {
				t.Fatalf("ResolveEntityID(%q, %q, %d) = %q, want %q",
					tt.repoID, tt.path, tt.line, got, tt.want)
			}
		})
	}
}

// resolveConstructorForTest is the single seam into the constructor lookup,
// so the table below runs against the repository-scoped signature.
func resolveConstructorForTest(index EntityIndex, repoID string, calleeFilePath string, edge map[string]any) string {
	return ResolveConstructorMethodCalleeID(index, repoID, calleeFilePath, edge)
}

// TestResolveConstructorMethodCalleeStaysInsideTheCalleeFile is the #7642
// regression table for the constructor lookup: a constructor resolves only
// under its own (repository, file) identity, never through a bare or shared
// relative file name from another directory or repository.
func TestResolveConstructorMethodCalleeStaysInsideTheCalleeFile(t *testing.T) {
	t.Parallel()

	constructorCall := map[string]any{"call_kind": "constructor_call", "name": "Widget"}
	plainCall := map[string]any{"call_kind": "call", "name": "Widget"}

	tests := []struct {
		name           string
		envelopes      []facts.Envelope
		repoID         string
		calleeFilePath string
		edge           map[string]any
		want           string
	}{
		{
			name: "same relative path in another repository",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/Widget.php", "src/Widget.php",
					map[string]any{"name": "constructor", "uid": "repo-a:ctor", "line_number": 1, "end_line": 5, "class_context": "Widget"}),
				containmentFileEnvelope("repo-b", "/repos/repo-b/src/Widget.php", "src/Widget.php",
					containmentFunction("other", "repo-b:other", 1, 5)),
			},
			repoID:         "repo-b",
			calleeFilePath: "src/Widget.php",
			edge:           constructorCall,
			want:           "",
		},
		{
			name: "same bare name in another directory of the same repository",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/a/Widget.php", "src/a/Widget.php",
					map[string]any{"name": "constructor", "uid": "repo-a:ctor", "line_number": 1, "end_line": 5, "class_context": "Widget"}),
			},
			repoID:         "repo-a",
			calleeFilePath: "/repos/repo-a/src/b/Widget.php",
			edge:           constructorCall,
			want:           "",
		},
		{
			name: "own file constructor still resolves through its relative path",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/Widget.php", "src/Widget.php",
					map[string]any{"name": "constructor", "uid": "repo-a:ctor", "line_number": 1, "end_line": 5, "class_context": "Widget"}),
			},
			repoID:         "repo-a",
			calleeFilePath: "src/Widget.php",
			edge:           constructorCall,
			want:           "repo-a:ctor",
		},
		{
			name: "non-constructor edge resolves to nothing",
			envelopes: []facts.Envelope{
				containmentFileEnvelope("repo-a", "/repos/repo-a/src/Widget.php", "src/Widget.php",
					map[string]any{"name": "constructor", "uid": "repo-a:ctor", "line_number": 1, "end_line": 5, "class_context": "Widget"}),
			},
			repoID:         "repo-a",
			calleeFilePath: "src/Widget.php",
			edge:           plainCall,
			want:           "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			index := BuildEntityIndex(tt.envelopes)
			got := resolveConstructorForTest(index, tt.repoID, tt.calleeFilePath, tt.edge)
			if got != tt.want {
				t.Fatalf("ResolveConstructorMethodCalleeID(%q, %q) = %q, want %q",
					tt.repoID, tt.calleeFilePath, got, tt.want)
			}
		})
	}
}
