// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// mcpReachableRouteTenantScopeCovered is the #5167 W1 CI-enforced
// exhaustiveness gate check: every MCP-dispatch route must be either wired
// into the scoped-token tenant-filter allowlist
// (query.ScopedHTTPRouteSupportsTenantFilter), explicitly annotated
// shared-key/all-scope-only (query.IsSharedKeyOnlyRoute, #5167 Group C: raw
// caller-supplied Cypher with no bindable selector), or explicitly tracked as
// a known, not-yet-safe-to-allowlist gap awaiting real row-filtering
// (query.IsPendingRowFilteringRoute, #5167 Group B, closed ledger). A route
// in none of the three is either a brand new route nobody classified yet, or
// an existing classification that regressed -- both are build failures, not
// silent gaps, per the #5167 issue's "Scoped-route coverage inventory": a
// route an MCP tool dispatches to that isn't allowlisted 403s a scoped or
// browser-session caller, and because POST /api/v0/ask re-dispatches inner
// tools under the caller's own token, the gap is not bypassable through
// natural language either.
func mcpReachableRouteTenantScopeCovered(r *http.Request) bool {
	return query.ScopedHTTPRouteSupportsTenantFilter(r) ||
		query.IsSharedKeyOnlyRoute(r) ||
		query.IsPendingRowFilteringRoute(r)
}

// TestMCPReachableRouteTenantScopeCoverageRejectsUnannotatedRoute is the
// #5167 W1 negative proof: a route that is wired into none of the three
// ledgers -- not the allowlist, not shared-key-only, not the pending
// row-filtering backlog -- must fail the coverage check. This is what
// TestEveryMCPReachableRouteIsScopedOrAnnotated below would catch for a real
// new MCP tool dispatching to an unclassified route; this test proves the
// underlying check itself actually rejects that shape, without permanently
// wiring a fake tool into ReadOnlyTools() just to prove a negative.
func TestMCPReachableRouteTenantScopeCoverageRejectsUnannotatedRoute(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/v0/definitely-not-a-real-mcp-route", nil)
	if mcpReachableRouteTenantScopeCovered(req) {
		t.Fatal("mcpReachableRouteTenantScopeCovered(unclassified route) = true, want false -- an unwired, unannotated route must fail the #5167 exhaustiveness gate")
	}
}

// TestEveryMCPReachableRouteIsScopedOrAnnotated is the #5167 W1 CI-enforced
// exhaustiveness gate. It sources its route set from ReadOnlyTools() --
// exactly the tool registry go/internal/mcp/server.go exposes to callers,
// and the same registry POST /api/v0/ask re-dispatches through under the
// caller's own token -- resolving each tool to its dispatched route the same
// way dispatchTool does. A new MCP tool whose route is neither allowlisted,
// shared-key-only, nor a tracked Group B gap fails this test, which is part
// of `go test ./internal/mcp` and therefore the same CI floor every other Go
// package test runs under (test.yml); no separate workflow is needed.
//
// It also staleness-checks the two closed ledgers
// (query.SharedKeyOnlyRouteSurfaces, query.PendingRowFilteringRouteSurfaces):
// an entry that no longer corresponds to any MCP-reachable route is dead
// weight a family workstream should have removed when it retired or
// re-routed the tool, mirroring scopedTokenAdvertisedRoutes's own staleness
// check in TestScopedTokenAllowlistCompleteness (go/internal/query).
func TestEveryMCPReachableRouteIsScopedOrAnnotated(t *testing.T) {
	reachable := map[string]bool{}
	for _, tool := range ReadOnlyTools() {
		args := minimalDispatchRouteArgs(tool.Name)
		route, err := resolveRoute(tool.Name, args)
		if err != nil {
			t.Fatalf("tool %q is registered but has no dispatch route: %v", tool.Name, err)
		}

		req := httptest.NewRequest(route.method, route.path, nil)
		if !mcpReachableRouteTenantScopeCovered(req) {
			t.Errorf(
				"tool %q dispatches to %s %s, which is neither scoped-token allowlisted, "+
					"shared-key-only annotated, nor tracked as a pending row-filtering gap -- "+
					"add a scopedHTTPRouteSupportsTenantFilter matcher and OpenAPI "+
					"\"x-scoped-token-support\" marker if the handler already grant-filters, "+
					"or an explicit annotation (sharedKeyOnlyRoutes / pendingRowFilteringRoutes "+
					"in go/internal/query) otherwise",
				tool.Name, route.method, route.path,
			)
		}
		reachable[route.method+" "+route.path] = true
	}

	assertLedgerNotStale(t, "sharedKeyOnlyRoutes", query.SharedKeyOnlyRouteSurfaces(), reachable)
	assertLedgerNotStale(t, "pendingRowFilteringRoutes", query.PendingRowFilteringRouteSurfaces(), reachable)
}

// assertLedgerNotStale fails t for every ledger entry that does not match any
// currently MCP-reachable route.
func assertLedgerNotStale(t *testing.T, ledgerName string, entries []string, reachable map[string]bool) {
	t.Helper()
	sort.Strings(entries)
	for _, entry := range entries {
		if !reachable[entry] {
			t.Errorf("%s has a stale entry %q -- no MCP tool dispatches to this route anymore; remove the entry", ledgerName, entry)
		}
	}
}
