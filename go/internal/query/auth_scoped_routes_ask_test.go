// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestScopedHTTPRoute_Ask verifies the scoped-token allowlist for the Ask Eshu
// endpoint: POST /api/v0/ask is permitted (its tenant scoping is enforced
// transitively by re-dispatching inner tool calls through this same gate),
// while the same paths under a method they do not serve are not.
// GET /api/v0/freshness/services/changed-since was this test's negative
// example while it sat in the #5167 pendingRowFilteringRoutes backlog; #6475
// part B bound its lineage read to the caller's grant and promoted it, so it
// is now a positive row and only its POST form stays refused.
// POST /api/v0/code/dead-code, POST /api/v0/code/bundles and POST
// /api/v0/code/relationships each left the negative list the same way.
// GET /api/v0/ecosystem/overview moved off this negative
// list in the #5167 F-6 W6 cloud/aws family workstream: getEcosystemOverview
// now restricts every count to the caller's granted repositories
// (runEcosystemOverviewCounts), so it is a real allowlist member -- see
// TestScopedHTTPRouteSupportsTenantFilterAllowsEcosystemOverview.
func TestScopedHTTPRoute_Ask(t *testing.T) {
	t.Parallel()

	cases := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodPost, "/api/v0/ask", true},
		{http.MethodGet, "/api/v0/ask", false},                               // only POST is the ask endpoint
		{http.MethodGet, "/api/v0/freshness/services/changed-since", true},   // lineage grant-bound in SQL (#6475)
		{http.MethodPost, "/api/v0/freshness/services/changed-since", false}, // not allowlisted under any method
		{http.MethodPost, "/api/v0/code/bundles", true},                      // public-only for a scoped caller (#5167)
		{http.MethodGet, "/api/v0/code/bundles", false},                      // only POST is the bundles route
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		if got := scopedHTTPRouteSupportsTenantFilter(req); got != c.want {
			t.Errorf("%s %s: got %v, want %v", c.method, c.path, got, c.want)
		}
	}
}

// TestScopedHTTPRouteSupportsTenantFilterAllowsEcosystemOverview is the
// direct matcher-level counterpart cited in TestScopedHTTPRoute_Ask's doc
// comment: it confirms GET /api/v0/ecosystem/overview is allowlisted (the
// grant-bound handler behavior itself is proven in
// infra_ecosystem_overview_test.go and auth_browser_session_all_scopes_test.go).
func TestScopedHTTPRouteSupportsTenantFilterAllowsEcosystemOverview(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ecosystem/overview", nil)
	if !scopedHTTPRouteSupportsTenantFilter(req) {
		t.Fatal("GET /api/v0/ecosystem/overview must be scoped-token allowlisted (#5167 F-6 W6)")
	}
	if got := scopedHTTPRouteSupportsTenantFilter(httptest.NewRequest(http.MethodPost, "/api/v0/ecosystem/overview", nil)); got {
		t.Fatal("POST /api/v0/ecosystem/overview must not be allowlisted; only GET is the ecosystem-overview route")
	}
}
