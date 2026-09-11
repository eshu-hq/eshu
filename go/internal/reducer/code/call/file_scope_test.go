// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

func TestBuildCodeCallFileScopesFallsBackForUnsafeFullRefreshOwnership(t *testing.T) {
	t.Parallel()

	result := BuildFileScopesByRepoID([]facts.Envelope{
		{
			FactKind: factload.FactKindRepository,
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"source_run_id": "run-a",
				"path":          "/repo",
			},
		},
		{
			FactKind: factload.FactKindFile,
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"relative_path": "../outside.py",
				"parsed_file_data": map[string]any{
					"path": "../outside.py",
				},
			},
		},
	})

	if _, ok := result.ScopesByRepoID["repo-a"]; ok {
		t.Fatalf("unsafe full-refresh file ownership produced scope: %#v", result.ScopesByRepoID["repo-a"])
	}
	if got, want := result.FullRefreshFallbackRepos, 1; got != want {
		t.Fatalf("FullRefreshFallbackRepos = %d, want %d", got, want)
	}
}

func TestBuildCodeCallFileScopesFallsBackWhenFullRefreshExceedsSafetyCap(t *testing.T) {
	t.Parallel()

	envelopes := make([]facts.Envelope, 0, 3)
	envelopes = append(envelopes, facts.Envelope{
		FactKind: factload.FactKindRepository,
		Payload: map[string]any{
			"repo_id":       "repo-a",
			"source_run_id": "run-a",
			"path":          "/repo",
		},
	})
	for _, relativePath := range []string{"a.py", "b.py"} {
		envelopes = append(envelopes, facts.Envelope{
			FactKind: factload.FactKindFile,
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"relative_path": relativePath,
				"parsed_file_data": map[string]any{
					"path": relativePath,
				},
			},
		})
	}

	ScopesByRepoID, fallbackRepos := buildCodeCallFullRefreshFileScopesByRepoIDWithLimit(envelopes, nil, 1)
	if _, ok := ScopesByRepoID["repo-a"]; ok {
		t.Fatalf("over-cap full-refresh file ownership produced scope: %#v", ScopesByRepoID["repo-a"])
	}
	if got, want := fallbackRepos, 1; got != want {
		t.Fatalf("fallbackRepos = %d, want %d", got, want)
	}
}

func TestBuildCodeCallFileScopesFallsBackForConflictingFullRefreshRoots(t *testing.T) {
	t.Parallel()

	result := BuildFileScopesByRepoID([]facts.Envelope{
		{
			FactKind: factload.FactKindRepository,
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"source_run_id": "run-a",
				"path":          "/repo-a",
			},
		},
		{
			FactKind: factload.FactKindRepository,
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"source_run_id": "run-a",
				"path":          "/other-repo-a",
			},
		},
		{
			FactKind: factload.FactKindFile,
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"relative_path": "caller.py",
				"parsed_file_data": map[string]any{
					"path": "caller.py",
				},
			},
		},
	})

	if _, ok := result.ScopesByRepoID["repo-a"]; ok {
		t.Fatalf("conflicting roots produced full-refresh file scope: %#v", result.ScopesByRepoID["repo-a"])
	}
	if got, want := result.FullRefreshFallbackRepos, 1; got != want {
		t.Fatalf("FullRefreshFallbackRepos = %d, want %d", got, want)
	}
}
