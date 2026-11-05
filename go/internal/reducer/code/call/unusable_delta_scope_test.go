// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// unusableDeltaScopeCases are delta-generation repository facts whose changed
// paths cannot be qualified into a file scope (#10023 gap 1). A delta
// generation carries content-entity facts for its changed files only, so a
// whole-repository CALLS retract for any of them deletes every unchanged
// file's edge with nothing left to restore it.
func unusableDeltaScopeCases() map[string]map[string]any {
	return map[string]map[string]any{
		"no local path": {
			"repo_id":              "repo-a",
			"delta_generation":     true,
			"delta_relative_paths": []string{"src/caller.go"},
		},
		"whitespace local path": {
			"repo_id":              "repo-a",
			"path":                 "   ",
			"delta_generation":     true,
			"delta_relative_paths": []string{"src/caller.go"},
		},
		"unnormalizable changed path": {
			"repo_id":              "repo-a",
			"path":                 "/repo",
			"delta_generation":     true,
			"delta_relative_paths": []string{"src/caller.go", "../outside.go"},
		},
		"no changed paths": {
			"repo_id":          "repo-a",
			"path":             "/repo",
			"delta_generation": true,
		},
	}
}

func unusableDeltaScopeContexts() map[string]sharedintent.ProjectionContext {
	return map[string]sharedintent.ProjectionContext{
		"repo-a": {
			ScopeID:          "scope:git:repo-a",
			AcceptanceUnitID: "repository:repo-a",
			SourceRunID:      "run-a",
			GenerationID:     "gen-delta",
		},
	}
}

// TestBuildFileScopesDoesNotReportFullGenerationFallbacks pins that only a
// delta-generation repository is ever reported unusable: a full-generation
// repository with no usable full-refresh scope is a FullRefreshFallbackRepos
// count, because its own generation re-emits every file and the
// whole-repository refresh is correct for it.
func TestBuildFileScopesDoesNotReportFullGenerationFallbacks(t *testing.T) {
	t.Parallel()

	result := BuildFileScopesByRepoID([]facts.Envelope{
		{FactKind: factload.FactKindRepository, Payload: map[string]any{"repo_id": "repo-full-no-path"}},
		{FactKind: factload.FactKindRepository, Payload: map[string]any{"repo_id": "repo-full-bad-file", "path": "/repo"}},
		{FactKind: factload.FactKindFile, Payload: map[string]any{
			"repo_id": "repo-full-bad-file", "relative_path": "../outside.py",
			"parsed_file_data": map[string]any{"path": "../outside.py"},
		}},
	})
	if len(result.UnusableDeltaScopes) != 0 {
		t.Fatalf("full-generation fallbacks reported as unusable delta scopes: %#v", result.UnusableDeltaScopes)
	}
	if got, want := result.FullRefreshFallbackRepos, 2; got != want {
		t.Fatalf("FullRefreshFallbackRepos = %d, want %d", got, want)
	}
}

// TestBuildFileScopesKeepsDeletedOnlyDeltaUsable pins that a delta whose only
// change is a deletion is a usable scope: the deleted path is still a file
// the retract must bind.
func TestBuildFileScopesKeepsDeletedOnlyDeltaUsable(t *testing.T) {
	t.Parallel()

	result := BuildFileScopesByRepoID([]facts.Envelope{{
		FactKind: factload.FactKindRepository,
		Payload: map[string]any{
			"repo_id":                      "repo-a",
			"path":                         "/repo",
			"delta_generation":             true,
			"delta_deleted_relative_paths": []string{"src/removed.go"},
		},
	}})
	if reason, ok := result.UnusableDeltaScopes["repo-a"]; ok {
		t.Fatalf("deleted-only delta reported unusable (%q)", reason)
	}
	scope, ok := result.ScopesByRepoID["repo-a"]
	if !ok {
		t.Fatalf("deleted-only delta lost its scope: %#v", result)
	}
	if got, want := scope.filePaths, []string{"/repo/src/removed.go"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("filePaths = %#v, want %#v", got, want)
	}
}

// TestBuildRefreshIntentsKeepsWholeScopeForUnscopedFullGeneration pins the
// other half of the gate: a full-generation repository with no usable file
// scope still falls back to the whole-repository refresh, because its own
// generation re-emits every file's edges.
func TestBuildRefreshIntentsKeepsWholeScopeForUnscopedFullGeneration(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	scopes := BuildFileScopesByRepoID([]facts.Envelope{{
		FactKind: factload.FactKindRepository,
		Payload: map[string]any{
			"repo_id": "repo-a",
		},
	}})
	intents := BuildRefreshIntentsWithDeltaFileScopes(unusableDeltaScopeContexts(), scopes.ScopesByRepoID, createdAt)
	if got, want := len(intents), 1; got != want {
		t.Fatalf("len(intents) = %d, want %d", got, want)
	}
	if got, want := intents[0].PartitionKey, RefreshPartitionKey("repo-a"); got != want {
		t.Fatalf("PartitionKey = %q, want %q", got, want)
	}
	if PayloadBool(intents[0].Payload, "delta_projection") {
		t.Fatalf("full-generation refresh carries delta_projection: %#v", intents[0].Payload)
	}
}

// TestBuildFileScopesReportsUnusableDeltaScopes pins that a delta repository
// whose scope is unusable stays visible in the result with a bounded reason,
// instead of disappearing into the same "no scope" state as a full-generation
// repository.
func TestBuildFileScopesReportsUnusableDeltaScopes(t *testing.T) {
	t.Parallel()

	want := map[string]DeltaScopeUnusableReason{
		"no local path":               DeltaScopeUnusableMissingLocalPath,
		"whitespace local path":       DeltaScopeUnusableMissingLocalPath,
		"unnormalizable changed path": DeltaScopeUnusableUnnormalizablePath,
		"no changed paths":            DeltaScopeUnusableNoChangedPaths,
	}
	for name, payload := range unusableDeltaScopeCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result := BuildFileScopesByRepoID([]facts.Envelope{
				{FactKind: factload.FactKindRepository, Payload: payload},
				{
					FactKind: factload.FactKindRepository,
					Payload: map[string]any{
						"repo_id":              "repo-b",
						"path":                 "/repo-b",
						"delta_generation":     true,
						"delta_relative_paths": []string{"src/changed.go"},
					},
				},
			})
			if _, ok := result.ScopesByRepoID["repo-a"]; ok {
				t.Fatalf("unusable delta scope produced a file scope: %#v", result.ScopesByRepoID["repo-a"])
			}
			if got := result.UnusableDeltaScopes["repo-a"]; got != want[name] {
				t.Fatalf("UnusableDeltaScopes[repo-a] = %q, want %q", got, want[name])
			}
			if _, ok := result.UnusableDeltaScopes["repo-b"]; ok {
				t.Fatalf("usable delta repository reported unusable: %#v", result.UnusableDeltaScopes)
			}
			if _, ok := result.ScopesByRepoID["repo-b"]; !ok {
				t.Fatalf("usable delta repository lost its scope: %#v", result.ScopesByRepoID)
			}
			if got, want := result.FullRefreshFallbackRepos, 0; got != want {
				t.Fatalf("FullRefreshFallbackRepos = %d, want %d (delta repositories are not counted)", got, want)
			}
		})
	}
}

// TestCollectDeltaScopesPicksMostBasicReason pins the deterministic reason
// when one repository has several delta repository facts with different
// defects.
func TestCollectDeltaScopesPicksMostBasicReason(t *testing.T) {
	t.Parallel()

	got := collectCodeCallDeltaScopes([]facts.Envelope{
		{FactKind: factload.FactKindRepository, Payload: map[string]any{
			"repo_id": "repo-a", "path": "/repo", "delta_generation": true,
			"delta_relative_paths": []string{"../outside.go"},
		}},
		{FactKind: factload.FactKindRepository, Payload: map[string]any{
			"repo_id": "repo-a", "delta_generation": true,
			"delta_relative_paths": []string{"src/caller.go"},
		}},
	})
	if reason := got.unusableByRepoID["repo-a"]; reason != DeltaScopeUnusableMissingLocalPath {
		t.Fatalf("reason = %q, want %q", reason, DeltaScopeUnusableMissingLocalPath)
	}
	if _, ok := got.scopesByRepoID["repo-a"]; ok {
		t.Fatalf("unusable repository kept a scope: %#v", got.scopesByRepoID)
	}
}
