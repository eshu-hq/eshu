// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// Probe-path tests for the #8803 empty-answer classification: rows-present
// skip, store failures, the unanchored read, scoped callers, and the probe
// deadline. The fixtures and the invariant table live in
// security_alert_coverage_test.go.

// TestSecurityAlertCoverageRowsPresentSkipsProbe pins the rows-present branch:
// returned rows are their own proof, so the collection-state store is never
// read and the coverage keeps its existing shape with no reasons.
func TestSecurityAlertCoverageRowsPresentSkipsProbe(t *testing.T) {
	t.Parallel()

	for route, target := range coverageRoutes(coverageTestRepositoryID) {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			store := &fakeSecurityAlertCollectionState{}
			code, body, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 2), target)
			if code != http.StatusOK {
				t.Fatalf("status = %d; body = %s", code, body)
			}
			if configCalls, stateCalls := store.calls(); configCalls != 0 || stateCalls != 0 {
				t.Fatalf("probe ran on a non-empty answer: config=%d state=%d", configCalls, stateCalls)
			}
			if resp.Data.CollectorReadiness != nil {
				t.Fatalf("collector_readiness attached to a non-empty answer: %s", body)
			}
			if resp.Truth.Level != "exact" || resp.Truth.Freshness.State != "fresh" {
				t.Fatalf("truth = %s/%s, want exact/fresh", resp.Truth.Level, resp.Truth.Freshness.State)
			}
			if resp.Data.Coverage != nil && len(resp.Data.Coverage.Reasons) != 0 {
				t.Fatalf("non-empty answer carries reasons %v", resp.Data.Coverage.Reasons)
			}
		})
	}
}

// TestSecurityAlertCoverageProbeFailure keeps the answer when the
// collection-state read fails: the page is served, but it cannot read as a
// complete, exact, fresh answer and it names no proven cause.
func TestSecurityAlertCoverageProbeFailure(t *testing.T) {
	t.Parallel()

	for _, store := range []*fakeSecurityAlertCollectionState{
		{configErr: errors.New("config read failed")},
		{configs: []string{coverageTestRepoTarget}, stateErr: errors.New("state read failed")},
	} {
		for route, target := range coverageRoutes(coverageTestRepositoryID) {
			code, body, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0), target)
			if code != http.StatusOK {
				t.Fatalf("%s: status = %d; body = %s", route, code, body)
			}
			assertCoverage(t, resp, coverageWant{
				"target_incomplete", []string{"collector_state_unavailable"}, "fallback", "unavailable", "", "readiness_unavailable",
			})
			assertCoverageInvariant(t, resp)
			if strings.Contains(body, "read failed") {
				t.Fatalf("%s: probe error text leaked: %s", route, body)
			}
		}
	}
}

// TestSecurityAlertCoverageUnanchoredReadsEveryTarget pins the read without a
// repository anchor: every configured target must be healthy for the empty
// answer to be complete, and the reasons name each distinct failing state.
func TestSecurityAlertCoverageUnanchoredReadsEveryTarget(t *testing.T) {
	t.Parallel()

	store := &fakeSecurityAlertCollectionState{
		configs: []string{coverageTestRepoTarget, coverageTestOrgTarget},
		states: map[string]SecurityAlertTargetState{
			"sa-target-repo": {LatestWorkItemStatus: "completed", CommittedGeneration: true},
			"sa-target-org":  terminalState("rate_limited", true),
		},
	}
	code, body, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0),
		"/api/v0/supply-chain/security-alerts/reconciliations?limit=10&cve_id=CVE-2026-0001")
	if code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", code, body)
	}
	assertCoverage(t, resp, coverageWant{
		"target_incomplete", []string{"terminal_fetch_failure:rate_limited"}, "derived", "stale", "missing_collector_completion", "",
	})
	if got := store.scopeIDs(); got != "sa-target-org,sa-target-repo" {
		t.Fatalf("unanchored state read scope ids = %q, want every configured target", got)
	}
	assertNoTargetIdentity(t, body)
}

// TestSecurityAlertCoverageScopedCallers proves the scoped-token contract: a
// selector outside the caller's grants answers 404 before the collection-state
// store is read, so no target membership is computed for it; an in-grant
// repository learns only its own coverage.
func TestSecurityAlertCoverageScopedCallers(t *testing.T) {
	t.Parallel()

	scoped := func(repoIDs ...string) context.Context {
		return auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
			Mode:                 auth.AuthModeScoped,
			AllowedRepositoryIDs: repoIDs,
		})
	}
	for route, target := range coverageRoutes(coverageTestRepositoryID) {
		store := &fakeSecurityAlertCollectionState{configs: []string{coverageTestRepoTarget}}
		code, body, _ := serveCoverage(scoped("repository:r_granted"), t, coverageTestHandler(store, 0), target)
		if code != http.StatusNotFound {
			t.Fatalf("%s out of grant: status = %d, want 404; body = %s", route, code, body)
		}
		if configCalls, stateCalls := store.calls(); configCalls != 0 || stateCalls != 0 {
			t.Fatalf("%s out of grant: probe ran (config=%d state=%d)", route, configCalls, stateCalls)
		}

		store = &fakeSecurityAlertCollectionState{configs: []string{coverageTestRepoTarget, coverageTestOrgTarget}}
		code, body, resp := serveCoverage(scoped(coverageTestRepositoryID), t, coverageTestHandler(store, 0), target)
		if code != http.StatusOK {
			t.Fatalf("%s in grant: status = %d; body = %s", route, code, body)
		}
		if got := store.scopeIDs(); got != "sa-target-repo" {
			t.Fatalf("%s in grant: state read scope ids = %q, want only the requested repository's target", route, got)
		}
		assertCoverage(t, resp, coverageWant{
			"target_incomplete", []string{"no_committed_generation"}, "derived", "building", "missing_collector_completion", "",
		})
		assertNoTargetIdentity(t, body)
	}
}

// TestSecurityAlertCoverageProbeDeadlineFailsClosed blocks the
// collection-state read until its context ends. The probe's own deadline must
// end it, and the page must still be served as an incomplete, non-fresh answer
// rather than hanging or answering 5xx.
func TestSecurityAlertCoverageProbeDeadlineFailsClosed(t *testing.T) {
	t.Parallel()

	for route, target := range coverageRoutes(coverageTestRepositoryID) {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			store := &fakeSecurityAlertCollectionState{block: true}
			started := time.Now()
			code, body, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0), target)
			elapsed := time.Since(started)
			if code != http.StatusOK {
				t.Fatalf("status = %d; body = %s", code, body)
			}
			if elapsed > 3*securityAlertCollectionStateTimeout {
				t.Fatalf("probe ran %s, want it bounded near %s", elapsed, securityAlertCollectionStateTimeout)
			}
			assertCoverage(t, resp, coverageWant{
				"target_incomplete", []string{"collector_state_unavailable"}, "fallback", "unavailable", "", "readiness_unavailable",
			})
			assertCoverageInvariant(t, resp)
		})
	}
}
