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
// transitively by re-dispatching inner tool calls through this same gate), while
// a non-orchestration whole-graph route such as code/dead-code (still in the
// #5167 pendingRowFilteringRoutes backlog, owned by a different family
// workstream) is not. GET /api/v0/ecosystem/overview moved off this negative
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
		{http.MethodGet, "/api/v0/ask", false},             // only POST is the ask endpoint
		{http.MethodPost, "/api/v0/code/dead-code", false}, // whole-graph, not allowlisted
		{http.MethodGet, "/api/v0/code/dead-code", false},  // not allowlisted under any method
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
