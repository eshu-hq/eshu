// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// importsCollectEnvelope builds a repository or file fact envelope carrying
// the given payload fields.
func importsCollectEnvelope(kind, repoID string, extra map[string]any) facts.Envelope {
	payload := map[string]any{"repo_id": repoID}
	for key, value := range extra {
		payload[key] = value
	}
	return facts.Envelope{FactKind: kind, Payload: payload}
}

// TestCollectRepositoryIDs proves repository and file facts contribute
// their repo IDs sorted and deduplicated, with graph_id as fallback.
func TestCollectRepositoryIDs(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		importsCollectEnvelope("repository", "b-repo", nil),
		importsCollectEnvelope("file", "a-repo", nil),
		importsCollectEnvelope("file", "a-repo", nil),
		importsCollectEnvelope("repository", "", map[string]any{"graph_id": "c-repo"}),
		importsCollectEnvelope("repository", "", nil),
		importsCollectEnvelope("call", "d-repo", nil),
	}
	got := CollectRepositoryIDs(envelopes)
	want := []string{"a-repo", "b-repo", "c-repo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectRepositoryIDs = %#v, want %#v", got, want)
	}
	if got := CollectRepositoryIDs(nil); len(got) != 0 {
		t.Fatalf("CollectRepositoryIDs(nil) = %#v, want empty", got)
	}
}

// TestCollectRepositoryImports proves imports_map payloads normalize into
// the per-repository symbol map, skipping facts without an ID or map.
func TestCollectRepositoryImports(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		importsCollectEnvelope("repository", "repo-a", map[string]any{
			"imports_map": map[string]any{
				"serve": []any{"./server.js", "./server.js", "  "},
			},
		}),
		importsCollectEnvelope("repository", "repo-b", map[string]any{
			"imports_map": map[string][]string{"run": {"./run.js"}},
		}),
		importsCollectEnvelope("repository", "repo-c", map[string]any{}),
		importsCollectEnvelope("repository", "", map[string]any{
			"imports_map": map[string]any{"x": []any{"./x.js"}},
		}),
		importsCollectEnvelope("file", "repo-a", map[string]any{
			"imports_map": map[string]any{"ignored": []any{"./ignored.js"}},
		}),
	}
	got := CollectRepositoryImports(envelopes)
	want := map[string]map[string][]string{
		"repo-a": {"serve": {"server.js"}},
		"repo-b": {"run": {"run.js"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectRepositoryImports = %#v, want %#v", got, want)
	}
}

// TestRepositoryImportPaths proves flattening dedups and normalizes across
// symbols while dropping blanks.
func TestRepositoryImportPaths(t *testing.T) {
	t.Parallel()

	got := RepositoryImportPaths(map[string][]string{
		"serve": {"./a.js", "./a.js", "  "},
		"run":   {"./b.js"},
	})
	if len(got) != 2 {
		t.Fatalf("RepositoryImportPaths = %#v, want 2 paths", got)
	}
	seen := map[string]bool{}
	for _, path := range got {
		seen[path] = true
	}
	if !seen["a.js"] || !seen["b.js"] {
		t.Fatalf("RepositoryImportPaths = %#v, want a.js and b.js", got)
	}
	if got := RepositoryImportPaths(nil); len(got) != 0 {
		t.Fatalf("RepositoryImportPaths(nil) = %#v, want empty", got)
	}
}

// TestCacheRepositoryImportPaths proves the extraction cache fills once and
// the resolution accessor prefers it over a fresh flatten.
func TestCacheRepositoryImportPaths(t *testing.T) {
	t.Parallel()

	index := EntityIndex{}
	CacheRepositoryImportPaths(&index, map[string]map[string][]string{
		"repo-a": {"serve": {"./server.js"}},
	})
	if got := index.RepositoryImportPathsByRepo("repo-a"); !reflect.DeepEqual(got, []string{"server.js"}) {
		t.Fatalf("cached paths = %#v, want [server.js]", got)
	}
	if got := index.RepositoryImportPathsByRepo("missing"); len(got) != 0 {
		t.Fatalf("cached paths for missing repo = %#v, want empty", got)
	}

	resolved := RepositoryImportPathsForResolution(
		index, "repo-a", map[string][]string{"other": {"./other.js"}},
	)
	if !reflect.DeepEqual(resolved, []string{"server.js"}) {
		t.Fatalf("resolution with cache = %#v, want cached server.js", resolved)
	}
	fallback := RepositoryImportPathsForResolution(
		EntityIndex{}, "repo-a", map[string][]string{"other": {"./other.js"}},
	)
	if !reflect.DeepEqual(fallback, []string{"other.js"}) {
		t.Fatalf("resolution without cache = %#v, want flattened other.js", fallback)
	}
	if got := RepositoryImportPathsForResolution(index, "repo-a", nil); got != nil {
		t.Fatalf("resolution with empty imports = %#v, want nil", got)
	}
}

// TestPrefersImportedTarget proves the JavaScript-qualified preference and
// the unqualified JS/Python preference with the qualified veto.
func TestPrefersImportedTarget(t *testing.T) {
	t.Parallel()

	qualified := map[string]any{"name": "listen", "full_name": "server.listen"}
	if !PrefersImportedQualifiedTarget(qualified, "javascript") {
		t.Fatal("qualified JS call should prefer its import binding")
	}
	if PrefersImportedQualifiedTarget(qualified, "python") {
		t.Fatal("qualified Python call must not take the JS qualified path")
	}
	if PrefersImportedQualifiedTarget(map[string]any{"name": "listen"}, "javascript") {
		t.Fatal("unqualified call must not take the qualified path")
	}
	if !PrefersImportedTargetBeforeRepoFallback(map[string]any{"name": "f"}, "typescript") {
		t.Fatal("unqualified TS call should prefer its import binding")
	}
	if !PrefersImportedTargetBeforeRepoFallback(map[string]any{"name": "f"}, "python") {
		t.Fatal("unqualified Python call should prefer its import binding")
	}
	if PrefersImportedTargetBeforeRepoFallback(qualified, "javascript") {
		t.Fatal("qualified call must not take the unqualified path")
	}
	if PrefersImportedTargetBeforeRepoFallback(map[string]any{"name": "f"}, "go") {
		t.Fatal("Go call must not take the import-first path")
	}
}

// TestHasRepositoryImportedTargetBinding proves the barrier trips only
// when an explicit import source matches a known repository path.
func TestHasRepositoryImportedTargetBinding(t *testing.T) {
	t.Parallel()

	fileData := map[string]any{
		"imports": []any{
			map[string]any{"name": "serve", "source": "./server"},
		},
	}
	call := map[string]any{"name": "serve", "full_name": "serve", "lang": "javascript"}
	bound := map[string][]string{"serve": {"/repo/src/server.js"}}
	if !HasRepositoryImportedTargetBinding(bound, nil, "/repo/src/a.js", "src/a.js", fileData, call) {
		t.Fatal("matching import binding should trip the barrier")
	}
	if HasRepositoryImportedTargetBinding(bound, nil, "/repo/src/a.js", "src/a.js", fileData,
		map[string]any{"name": "other", "lang": "javascript"}) {
		t.Fatal("unbound call must not trip the barrier")
	}
	if HasRepositoryImportedTargetBinding(nil, nil, "/repo/src/a.js", "src/a.js", fileData, call) {
		t.Fatal("empty repository imports must not trip the barrier")
	}
	if HasRepositoryImportedTargetBinding(
		map[string][]string{"serve": {"/repo/src/unrelated.js"}},
		[]string{"/repo/src/other.js"},
		"/repo/src/a.js", "src/a.js", fileData, call) {
		t.Fatal("unmatched import source must not trip the barrier")
	}
	matchedByFallback := map[string][]string{"serve": {"/repo/src/unrelated.js"}}
	if !HasRepositoryImportedTargetBinding(
		matchedByFallback,
		[]string{"/repo/src/server.js"},
		"/repo/src/a.js", "src/a.js", fileData, call) {
		t.Fatal("repository-path fallback match should trip the barrier")
	}
}
