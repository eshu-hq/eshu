// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// coverageTestCoveringOrgTarget is an organization target whose allowlist holds
// the repository the coverage tests ask about.
const coverageTestCoveringOrgTarget = `{"targets":[{"provider":"github_dependabot","scope":"org","scope_id":"sa-target-org-covering","organization":"example-org","allowed_repositories":["example-org/example-repo","example-org/other-repo"]}]}`

// TestSecurityAlertEmptyAnswerReportsAnUnreadRepositoryAsIncomplete is the
// #8943 invariant as a table: an empty answer for a repository the collector
// did not read in full carries the stored reason and never reads complete,
// while a repository read in full, or covered by another target that read it,
// still reads complete. The org stream's truncation alone changes nothing.
func TestSecurityAlertEmptyAnswerReportsAnUnreadRepositoryAsIncomplete(t *testing.T) {
	t.Parallel()

	committed := SecurityAlertTargetState{LatestWorkItemStatus: "completed", CommittedGeneration: true}
	unread := func(reason string) SecurityAlertTargetState {
		state := committed
		state.UnreadTotal = 1
		state.UnreadReasons = []string{reason}
		state.UnreadRequested = map[string]string{"example-org/example-repo": reason}
		return state
	}
	otherUnread := committed
	otherUnread.UnreadTotal = 2
	otherUnread.UnreadReasons = []string{"org_repository_read_failed"}
	// Unread repositories exist, but none is the requested one.
	complete := coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"}
	incomplete := func(reasons ...string) coverageWant {
		return coverageWant{"target_incomplete", reasons, "derived", "fresh", "", ""}
	}
	cases := []struct {
		name    string
		configs []string
		states  map[string]SecurityAlertTargetState
		want    coverageWant
	}{
		{
			name:    "rate limited before the repository was read",
			configs: []string{coverageTestCoveringOrgTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": unread("org_repository_read_rate_limited")},
			want:    incomplete("org_repository_read_rate_limited"),
		},
		{
			name:    "past the fan-out cap",
			configs: []string{coverageTestCoveringOrgTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": unread("org_repository_fanout_cap_reached")},
			want:    incomplete("org_repository_fanout_cap_reached"),
		},
		{
			name:    "own read failed",
			configs: []string{coverageTestCoveringOrgTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": unread("org_repository_read_failed")},
			want:    incomplete("org_repository_read_failed"),
		},
		{
			name:    "fan-out stopped before the repository",
			configs: []string{coverageTestCoveringOrgTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": unread("org_repository_fanout_stopped")},
			want:    incomplete("org_repository_fanout_stopped"),
		},
		{
			name:    "alerts disabled for the repository",
			configs: []string{coverageTestCoveringOrgTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": unread("org_repository_alerts_disabled")},
			want:    incomplete("org_repository_alerts_disabled"),
		},
		{
			name:    "own read stopped at max_pages",
			configs: []string{coverageTestCoveringOrgTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": unread("provider_open_alert_page_limit_reached")},
			want:    incomplete("provider_open_alert_page_limit_reached"),
		},
		{
			name:    "a reason this build does not know stays a closed token",
			configs: []string{coverageTestCoveringOrgTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": unread("status 403 from https://example.invalid")},
			want:    incomplete("org_repository_read_incomplete"),
		},
		{
			name:    "other repositories unread but the requested one read in full",
			configs: []string{coverageTestCoveringOrgTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": otherUnread},
			want:    complete,
		},
		{
			name:    "read in full with nothing unread",
			configs: []string{coverageTestCoveringOrgTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": committed},
			want:    complete,
		},
		{
			name:    "another covering target read the repository in full",
			configs: []string{coverageTestRepoTarget, coverageTestCoveringOrgTarget},
			states: map[string]SecurityAlertTargetState{
				"sa-target-repo":         committed,
				"sa-target-org-covering": unread("org_repository_read_rate_limited"),
			},
			want: complete,
		},
		{
			name:    "a terminal failure outranks the unread reason",
			configs: []string{coverageTestCoveringOrgTarget},
			states: map[string]SecurityAlertTargetState{"sa-target-org-covering": func() SecurityAlertTargetState {
				state := unread("org_repository_read_failed")
				state.LatestWorkItemStatus, state.LatestFailureClass, state.LatestFailureTerminal = "failed_terminal", "auth_denied", true
				return state
			}()},
			want: coverageWant{"target_incomplete", []string{"terminal_fetch_failure:auth_denied"}, "derived", "stale", "missing_collector_completion", ""},
		},
	}
	for _, tc := range cases {
		for route, target := range coverageRoutes(coverageTestRepositoryID) {
			t.Run(tc.name+"/"+route, func(t *testing.T) {
				t.Parallel()
				store := &fakeSecurityAlertCollectionState{configs: tc.configs, states: tc.states}
				code, body, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0), target)
				if code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body = %s", code, body)
				}
				assertCoverage(t, resp, tc.want)
				assertNoTargetIdentity(t, body)
				assertCoverageInvariant(t, resp)
				if got := store.lastRequestedSlugs(); !slices.Equal(got, []string{"example-org/example-repo"}) {
					t.Fatalf("state read requested slugs = %v, want the one requested repository", got)
				}
			})
		}
	}
}

// TestSecurityAlertUnanchoredEmptyAnswerReportsAnyUnreadRepository pins the
// read without a repository anchor: any enabled target with unread
// repositories means the empty answer may be missing alerts for them.
func TestSecurityAlertUnanchoredEmptyAnswerReportsAnyUnreadRepository(t *testing.T) {
	t.Parallel()

	committed := SecurityAlertTargetState{LatestWorkItemStatus: "completed", CommittedGeneration: true}
	partial := committed
	partial.UnreadTotal = 3
	partial.UnreadReasons = []string{"org_repository_fanout_cap_reached", "org_repository_read_failed"}
	store := &fakeSecurityAlertCollectionState{
		configs: []string{coverageTestRepoTarget, coverageTestCoveringOrgTarget},
		states:  map[string]SecurityAlertTargetState{"sa-target-repo": committed, "sa-target-org-covering": partial},
	}
	code, body, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0),
		"/api/v0/supply-chain/security-alerts/reconciliations?limit=10&cve_id=CVE-2026-0001")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", code, body)
	}
	assertCoverage(t, resp, coverageWant{
		"target_incomplete",
		[]string{"org_repository_fanout_cap_reached", "org_repository_read_failed"},
		"derived", "fresh", "", "",
	})
	assertNoTargetIdentity(t, body)
	if got := store.lastRequestedSlugs(); len(got) != 0 {
		t.Errorf("unanchored state read requested slugs = %v, want none", got)
	}

	// A fully read org target leaves the unanchored answer complete.
	store = &fakeSecurityAlertCollectionState{
		configs: []string{coverageTestCoveringOrgTarget},
		states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": committed},
	}
	_, body, resp = serveCoverage(context.Background(), t, coverageTestHandler(store, 0),
		"/api/v0/supply-chain/security-alerts/reconciliations?limit=10&cve_id=CVE-2026-0001")
	assertCoverage(t, resp, coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"})
	if strings.Contains(body, "org_repository") {
		t.Errorf("complete answer names an unread reason: %s", body)
	}
}
