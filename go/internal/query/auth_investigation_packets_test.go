// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareWithScopedTokensAllowsInvestigationPacketRoutes(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"/api/v0/investigations/supply-chain/impact/packet?finding_id=finding-1&repository_id=repo-team-a",
		"/api/v0/investigations/deployable-unit/packet?scope_id=repo-team-a&generation_id=generation-1",
		// Drift packets (#5167 W5) bind AllowedScopeIDs directly against the
		// cloud ingestion scope_id -- drift findings have no repository
		// dimension, so scope-grant proof (not repository-grant proof) is the
		// applicable filter, matching GET /api/v0/replatforming/selectors.
		"/api/v0/investigations/drift/packet?scope_id=account-1",
	}
	for _, target := range allowed {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			if !scopedHTTPRouteSupportsTenantFilter(req) {
				t.Fatalf("scopedHTTPRouteSupportsTenantFilter(GET %s) = false, want true", target)
			}
		})
	}
}
