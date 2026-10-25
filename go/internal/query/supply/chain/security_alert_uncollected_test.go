// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"net/http"
	"testing"
)

// coverageTestOrgTargetBefore is the org target before its allowlist gained
// the requested repository; coverageTestCoveringOrgTarget is the same target
// (same scope_id) after.
const coverageTestOrgTargetBefore = `{"targets":[{"provider":"github_dependabot","scope":"organization","scope_id":"sa-target-org-covering","organization":"example-org","allowed_repositories":["example-org/other-repo"]}]}`

// TestSecurityAlertEmptyAnswerReportsARepositoryAddedAfterTheLatestGeneration
// is the #8838 invariant as a table: a repository the configuration covers but
// the target's latest committed generation did not collect reads as not yet
// collected (no_committed_generation, building), never complete. A generation
// that collected it, or one committed before the target set was recorded,
// keeps the answer it had.
func TestSecurityAlertEmptyAnswerReportsARepositoryAddedAfterTheLatestGeneration(t *testing.T) {
	t.Parallel()

	const cause = "missing_collector_completion"
	committed := SecurityAlertTargetState{LatestWorkItemStatus: "completed", CommittedGeneration: true}
	complete := coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"}
	building := coverageWant{"target_incomplete", []string{"no_committed_generation"}, "derived", "building", cause, ""}
	cases := []struct {
		name       string
		configs    []string
		states     map[string]SecurityAlertTargetState
		targetSets map[string][]string
		want       coverageWant
	}{
		{
			name:       "positive: the latest generation collected the repository",
			configs:    []string{coverageTestCoveringOrgTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-org-covering": committed},
			targetSets: map[string][]string{"sa-target-org-covering": {"example-org/example-repo", "example-org/other-repo"}},
			want:       complete,
		},
		{
			name:       "negative: allowlist gained the repository after the latest generation",
			configs:    []string{coverageTestCoveringOrgTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-org-covering": committed},
			targetSets: map[string][]string{"sa-target-org-covering": {"example-org/other-repo"}},
			want:       building,
		},
		{
			name:       "negative: a reused repository scope_id now names another repository",
			configs:    []string{coverageTestRepoTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-repo": committed},
			targetSets: map[string][]string{"sa-target-repo": {"example-org/retired-repo"}},
			want:       building,
		},
		{
			name:       "negative: a recorded empty set collected nothing",
			configs:    []string{coverageTestCoveringOrgTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-org-covering": committed},
			targetSets: map[string][]string{"sa-target-org-covering": {}},
			want:       building,
		},
		{
			name:       "negative: a terminal failure on a target that never collected the repository",
			configs:    []string{coverageTestCoveringOrgTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-org-covering": terminalState("auth_denied", true)},
			targetSets: map[string][]string{"sa-target-org-covering": {"example-org/other-repo"}},
			want:       coverageWant{"target_incomplete", []string{"terminal_fetch_failure:auth_denied"}, "fallback", "unavailable", cause, ""},
		},
		{
			name:       "negative: allowlist removed the repository the latest generation still holds",
			configs:    []string{coverageTestOrgTargetBefore},
			states:     map[string]SecurityAlertTargetState{"sa-target-org-covering": committed},
			targetSets: map[string][]string{"sa-target-org-covering": {"example-org/example-repo", "example-org/other-repo"}},
			want:       coverageWant{"not_configured", []string{"repository_outside_target_set"}, "fallback", "unavailable", cause, ""},
		},
		{
			name:    "legacy: a generation committed before the set was recorded",
			configs: []string{coverageTestCoveringOrgTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": committed},
			want:    complete,
		},
		{
			name:    "ambiguous: another covering target collected the repository",
			configs: []string{coverageTestRepoTarget, coverageTestCoveringOrgTarget},
			states: map[string]SecurityAlertTargetState{
				"sa-target-repo":         committed,
				"sa-target-org-covering": committed,
			},
			targetSets: map[string][]string{
				"sa-target-repo":         {"example-org/example-repo"},
				"sa-target-org-covering": {"example-org/other-repo"},
			},
			want: complete,
		},
		{
			name:    "ambiguous: no covering target collected the repository yet",
			configs: []string{coverageTestRepoTarget, coverageTestCoveringOrgTarget},
			states: map[string]SecurityAlertTargetState{
				"sa-target-repo":         {LatestWorkItemStatus: "running"},
				"sa-target-org-covering": committed,
			},
			targetSets: map[string][]string{"sa-target-org-covering": {"example-org/other-repo"}},
			want:       building,
		},
	}
	for _, tc := range cases {
		for route, target := range coverageRoutes(coverageTestRepositoryID) {
			t.Run(tc.name+"/"+route, func(t *testing.T) {
				t.Parallel()
				store := &fakeSecurityAlertCollectionState{configs: tc.configs, states: tc.states, targetSets: tc.targetSets}
				code, body, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0), target)
				if code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body = %s", code, body)
				}
				assertCoverage(t, resp, tc.want)
				assertNoTargetIdentity(t, body)
				assertCoverageInvariant(t, resp)
			})
		}
	}
}

// TestSecurityAlertAllowlistChangeBetweenTwoCycles walks the #8838 acceptance
// sequence on one target: before the allowlist change the repository is
// outside the target; after the change, until a generation that collected it
// projects, it is not yet collected; after that projection it is complete.
func TestSecurityAlertAllowlistChangeBetweenTwoCycles(t *testing.T) {
	t.Parallel()

	committed := SecurityAlertTargetState{LatestWorkItemStatus: "completed", CommittedGeneration: true}
	steps := []struct {
		name      string
		config    string
		targetSet []string
		want      coverageWant
	}{
		{
			name:      "cycle 1 projected, repository not allowlisted",
			config:    coverageTestOrgTargetBefore,
			targetSet: []string{"example-org/other-repo"},
			want:      coverageWant{"not_configured", []string{"repository_outside_target_set"}, "fallback", "unavailable", "missing_collector_completion", ""},
		},
		{
			name:      "allowlist changed, cycle 2 not yet projected",
			config:    coverageTestCoveringOrgTarget,
			targetSet: []string{"example-org/other-repo"},
			want:      coverageWant{"target_incomplete", []string{"no_committed_generation"}, "derived", "building", "missing_collector_completion", ""},
		},
		{
			name:      "cycle 2 projected with the added repository",
			config:    coverageTestCoveringOrgTarget,
			targetSet: []string{"example-org/example-repo", "example-org/other-repo"},
			want:      coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"},
		},
	}
	for _, step := range steps {
		store := &fakeSecurityAlertCollectionState{
			configs:    []string{step.config},
			states:     map[string]SecurityAlertTargetState{"sa-target-org-covering": committed},
			targetSets: map[string][]string{"sa-target-org-covering": step.targetSet},
		}
		code, body, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0),
			coverageRoutes(coverageTestRepositoryID)["list"])
		if code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200; body = %s", step.name, code, body)
		}
		t.Run(step.name, func(t *testing.T) { assertCoverage(t, resp, step.want) })
	}
}

// TestSecurityAlertUnanchoredEmptyAnswerIgnoresTheTargetSet pins that an
// unanchored read requests no slug, so the recorded set cannot mark it.
func TestSecurityAlertUnanchoredEmptyAnswerIgnoresTheTargetSet(t *testing.T) {
	t.Parallel()

	store := &fakeSecurityAlertCollectionState{
		configs:    []string{coverageTestCoveringOrgTarget},
		states:     map[string]SecurityAlertTargetState{"sa-target-org-covering": {LatestWorkItemStatus: "completed", CommittedGeneration: true}},
		targetSets: map[string][]string{"sa-target-org-covering": {"example-org/other-repo"}},
	}
	_, _, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0),
		"/api/v0/supply-chain/security-alerts/reconciliations?limit=10&cve_id=CVE-2026-0001")
	assertCoverage(t, resp, coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"})
}

// TestClassifySecurityAlertCollectionCountsOnlySlugsTheTargetCovers proves a
// multi-repository anchored read marks a target uncollected only for a slug
// that target covers: an org target never lists another owner's repository,
// so that repository being absent from its set says nothing.
func TestClassifySecurityAlertCollectionCountsOnlySlugsTheTargetCovers(t *testing.T) {
	t.Parallel()

	targets := decodeSecurityAlertTargets([]string{coverageTestCoveringOrgTarget})
	state := SecurityAlertTargetState{
		ScopeID: "sa-target-org-covering", LatestWorkItemStatus: "completed", CommittedGeneration: true,
		// The store reports every requested slug outside the set, including one
		// this target can never cover.
		UncollectedRequested: []string{"another-org/unrelated"},
	}
	slugs := []string{"example-org/example-repo", "another-org/unrelated"}
	verdict := classifySecurityAlertCollection(targets, []string{"sa-target-org-covering"}, []SecurityAlertTargetState{state}, slugs, true)
	if !verdict.healthy() {
		t.Fatalf("verdict = %+v, want healthy: the uncollected slug is not one this target covers", verdict)
	}
	state.UncollectedRequested = []string{"another-org/unrelated", "example-org/example-repo"}
	verdict = classifySecurityAlertCollection(targets, []string{"sa-target-org-covering"}, []SecurityAlertTargetState{state}, slugs, true)
	if verdict.collectorState != securityAlertCollectorStateCollecting {
		t.Fatalf("collectorState = %q, want %q for a covered, uncollected slug", verdict.collectorState, securityAlertCollectorStateCollecting)
	}
}
