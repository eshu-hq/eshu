// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// TestSecurityAlertCoverageGrantlessScopedToken pins the #8803 F1 contract: a
// scoped token with no grants is answered without a collector read, and its
// empty answer reports not_configured with reason no_authorized_repositories,
// fallback truth, and unavailable freshness, never a vacuous complete. It
// carries no collector_readiness envelope and no freshness cause, because it
// says nothing about collector health, and it emits no stage event. The
// counter still records the classified answer. The rows store holds rows, so
// any read of it would make the answer non-empty.
func TestSecurityAlertCoverageGrantlessScopedToken(t *testing.T) {
	t.Parallel()

	grantless := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{Mode: auth.AuthModeScoped})
	routes := map[string]string{
		"list unanchored":      "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&cve_id=CVE-2026-0001",
		"count unanchored":     "/api/v0/supply-chain/security-alerts/reconciliations/count?cve_id=CVE-2026-0001",
		"inventory unanchored": "/api/v0/supply-chain/security-alerts/reconciliations/inventory?cve_id=CVE-2026-0001",
	}
	for route, target := range coverageRoutes(coverageTestRepositoryID) {
		routes[route+" anchored"] = target
	}
	for route, target := range routes {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			store := &fakeSecurityAlertCollectionState{configs: []string{coverageTestRepoTarget}}
			signals := &coverageSignals{}
			h := signals.instrument(t, coverageTestHandler(store, 2))
			code, body, resp := serveCoverage(grantless, t, h, target)
			if code != http.StatusOK {
				t.Fatalf("status = %d; body = %s", code, body)
			}
			if configCalls, stateCalls := store.calls(); configCalls != 0 || stateCalls != 0 {
				t.Fatalf("grantless token read collector state (config=%d state=%d)", configCalls, stateCalls)
			}
			assertCoverage(t, resp, coverageWant{
				"not_configured", []string{"no_authorized_repositories"}, "fallback", "unavailable", "", "",
			})
			assertCoverageInvariant(t, resp)
			assertNoTargetIdentity(t, body)
			if events := signals.collectionStateEvents(t); len(events) != 0 {
				t.Fatalf("grantless token emitted %d collection-state stage events", len(events))
			}
			counts := signals.emptyAnswerCounts(t)
			if got := counts["not_configured|no_authorized_repositories"]; got != 1 || len(counts) != 1 {
				t.Fatalf("empty answer counter = %v, want one not_configured|no_authorized_repositories", counts)
			}
		})
	}
}
