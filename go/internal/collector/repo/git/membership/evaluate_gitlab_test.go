// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"testing"
)

// TestEvaluateGitLabGroupJudgesOnlyTheGroupPrefix proves the path-prefix
// partition: a gitlab_group selector for acme/platform judges only scopes
// under that prefix. The sibling group's scope and the lookalike-prefix
// scope get no rows at all.
func TestEvaluateGitLabGroupJudgesOnlyTheGroupPrefix(t *testing.T) {
	t.Parallel()

	selector := NewGitLabGroupSelector("gitlabGroup", "gitlab.com", "Acme/Platform", nil, false, "")
	known := []KnownScope{
		{ScopeID: scopeIDFor("acme/platform/a"), Slug: "acme/platform/a"},
		{ScopeID: scopeIDFor("acme/platform/sub/b"), Slug: "acme/platform/sub/b"},
		{ScopeID: scopeIDFor("acme/other/c"), Slug: "acme/other/c"},
		{ScopeID: scopeIDFor("acme/platformer/d"), Slug: "acme/platformer/d"},
	}
	listing := Listing{Complete: true, Repositories: []ListedRepository{
		{ScopeID: scopeIDFor("acme/platform/a"), Slug: "gitlab/acme/platform/a", GitHubID: 101, State: StateSelected},
	}}
	result := Evaluate(Input{Selector: selector, Now: cycleOne, LivenessWindow: testWindow, Listing: listing, Known: known})

	if result.Outcome != OutcomeEvaluated {
		t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeEvaluated)
	}
	if result.Counts.Known != 2 {
		t.Fatalf("known = %d, want 2 (only the group prefix)", result.Counts.Known)
	}
	rows := make(map[string]Row, len(result.Batch.Rows))
	for _, row := range result.Batch.Rows {
		rows[row.ScopeID] = row
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want exactly the two in-prefix scopes", result.Batch.Rows)
	}
	if row := rows[scopeIDFor("acme/platform/a")]; row.State != StateSelected || row.GitHubRepoID != 101 {
		t.Fatalf("a row = %+v, want selected with the provider numeric id", row)
	}
	if row := rows[scopeIDFor("acme/platform/sub/b")]; row.State != StateNotListed {
		t.Fatalf("sub/b row = %+v, want not_listed", row)
	}
}

// TestFirstSegmentPartitionMassExcludesNestedGroups pins the ruling's hazard
// that the prefix partition removes: with a first-segment partition, owner
// acme mass-marks the sibling and lookalike scopes not_listed, and owner
// acme/platform judges nothing at all.
func TestFirstSegmentPartitionMassExcludesNestedGroups(t *testing.T) {
	t.Parallel()

	known := []KnownScope{
		{ScopeID: scopeIDFor("acme/platform/a"), Slug: "acme/platform/a"},
		{ScopeID: scopeIDFor("acme/other/c"), Slug: "acme/other/c"},
		{ScopeID: scopeIDFor("acme/platformer/d"), Slug: "acme/platformer/d"},
	}
	listing := Listing{Complete: true, Repositories: []ListedRepository{
		{ScopeID: scopeIDFor("acme/platform/a"), Slug: "acme/platform/a", GitHubID: 101, State: StateSelected},
	}}

	top := Evaluate(Input{
		Selector:       NewGitHubOrgSelector("githubOrg", "acme", nil, false, ""),
		Now:            cycleOne,
		LivenessWindow: testWindow,
		Listing:        listing,
		Known:          known,
	})
	if top.Outcome != OutcomeEvaluated {
		t.Fatalf("top-level outcome = %q, want %q", top.Outcome, OutcomeEvaluated)
	}
	states := make(map[string]State, len(top.Batch.Rows))
	for _, row := range top.Batch.Rows {
		states[row.ScopeID] = row.State
	}
	if states[scopeIDFor("acme/other/c")] != StateNotListed || states[scopeIDFor("acme/platformer/d")] != StateNotListed {
		t.Fatalf("first-segment rows = %+v, want c and d not_listed", states)
	}

	nested := Evaluate(Input{
		Selector:       NewGitHubOrgSelector("githubOrg", "acme/platform", nil, false, ""),
		Now:            cycleOne,
		LivenessWindow: testWindow,
		Listing:        listing,
		Known:          known,
	})
	if nested.Outcome != OutcomeEvaluated {
		t.Fatalf("nested outcome = %q, want %q", nested.Outcome, OutcomeEvaluated)
	}
	if nested.Counts.Known != 0 || len(nested.Batch.Rows) != 0 {
		t.Fatalf("nested known = %d rows = %d, want nothing judged", nested.Counts.Known, len(nested.Batch.Rows))
	}
}
