// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"net/http"
	"slices"
	"testing"
)

// TestSecurityAlertCoverageReasonsPinned pins the closed reason set by value,
// so the OpenAPI enum test (which compares the spec to the same Go list) cannot
// pass on a set that silently lost a class. The terminal classes are every
// class the security-alert claim runner can persist on a terminal row
// (#8803 F2): the provider classes, the retry-budget class, identity_mismatch,
// and the claim-stage fallbacks.
func TestSecurityAlertCoverageReasonsPinned(t *testing.T) {
	t.Parallel()

	want := []string{
		"collector_not_configured",
		"collector_state_unavailable",
		"no_authorized_repositories",
		"no_committed_generation",
		"repository_outside_target_set",
		"terminal_fetch_failure:attempt_budget_exhausted",
		"terminal_fetch_failure:auth_denied",
		"terminal_fetch_failure:collect_failure",
		"terminal_fetch_failure:commit_failure",
		"terminal_fetch_failure:identity_mismatch",
		"terminal_fetch_failure:not_found",
		"terminal_fetch_failure:rate_limited",
		"terminal_fetch_failure:retryable",
		"terminal_fetch_failure:terminal",
		"terminal_fetch_failure:unclassified",
	}
	if got := securityAlertCoverageReasons(); !slices.Equal(got, want) {
		t.Fatalf("securityAlertCoverageReasons() = %v\nwant %v", got, want)
	}
}

// TestSecurityAlertCoverageTerminalClasses proves every persisted terminal
// class reaches the payload as its own reason instead of collapsing to
// unclassified. A retry budget exhausted on a transient provider failure
// persists class retryable on a terminal row; a budget exhausted on a cause
// that carries no class persists attempt_budget_exhausted.
func TestSecurityAlertCoverageTerminalClasses(t *testing.T) {
	t.Parallel()

	for _, class := range []string{
		"auth_denied", "not_found", "rate_limited", "terminal", "retryable",
		"attempt_budget_exhausted", "identity_mismatch", "collect_failure", "commit_failure",
	} {
		t.Run(class, func(t *testing.T) {
			t.Parallel()
			store := &fakeSecurityAlertCollectionState{
				configs: []string{coverageTestRepoTarget},
				states:  map[string]SecurityAlertTargetState{"sa-target-repo": terminalState(class, false)},
			}
			target := coverageRoutes(coverageTestRepositoryID)["list"]
			code, body, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0), target)
			if code != http.StatusOK {
				t.Fatalf("status = %d; body = %s", code, body)
			}
			assertCoverage(t, resp, coverageWant{
				"target_incomplete", []string{"terminal_fetch_failure:" + class}, "fallback", "unavailable", "missing_collector_completion", "",
			})
		})
	}
}
