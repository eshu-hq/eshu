// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"testing"
)

// scopedRouteClassNames is the test-local rendering of the five
// scopedRouteClass constants, so a lockstep failure names the class a route
// was given instead of printing a bare integer. It is deliberately not a
// String() method on the production type: the class has no wire or log
// meaning, and adding one would invite callers to render it somewhere a
// reader would mistake it for part of the API contract.
var scopedRouteClassNames = map[scopedRouteClass]string{
	scopedRouteGrantBound:       "grant_bound",
	scopedRouteIdentityBound:    "identity_bound",
	scopedRouteTenantDataFree:   "tenant_data_free",
	scopedRouteDeploymentScoped: "deployment_scoped",
	scopedRouteTransitive:       "transitive",
}

// TestScopedRouteClassLedgerAgreesWithPredicate is the lockstep gate between
// the scopedTokenAdvertisedRoutes classification data and the request-shaped
// accessor the auth middleware actually calls. The two encode the same fact
// -- whether an all-scope browser session may enter a route without the
// BrowserSessionRoutePolicy mode check -- in two places, so they can drift:
// classifying a route identity_bound without wiring a matcher into
// scopedRouteNeedsNoCallerGrant leaves it fail-closed while the ledger claims
// otherwise, and adding a matcher without reclassifying the route opens it at
// runtime while the ledger still reads grant_bound. Either direction fails
// here.
//
// It also pins the fail-closed default for the two allowlisted routes that
// carry no ledger entry at all, the MCP transport paths.
func TestScopedRouteClassLedgerAgreesWithPredicate(t *testing.T) {
	t.Parallel()

	for name, class := range scopedTokenAdvertisedRoutes {
		name, class := name, class
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			className, known := scopedRouteClassNames[class]
			if !known {
				t.Fatalf("%s: class = %d, which is not one of the five scopedRouteClass constants -- a new class needs a case in scopedRouteClass's const block, in admitsAllScopesSessionWithoutPolicy, and in scopedRouteClassNames", name, class)
			}

			req := surfaceNameToRequest(t, name)
			got := scopedRouteNeedsNoCallerGrant(req)
			want := class.admitsAllScopesSessionWithoutPolicy()
			if got != want {
				t.Fatalf(
					"%s: scopedRouteNeedsNoCallerGrant = %t, but the ledger classifies it %s (admitsAllScopesSessionWithoutPolicy = %t) -- either wire the route's matcher into scopedRouteNeedsNoCallerGrant (auth_browser_session_route_policy.go) or fix its class in scopedTokenAdvertisedRoutes (auth_scoped_routes_completeness.go)",
					name, got, className, want,
				)
			}

			// A route can only be classified if it is on the allowlist in the
			// first place; the ledger and scopedHTTPRouteSupportsTenantFilter
			// are kept in step by TestScopedTokenAllowlistCompleteness, and a
			// disagreement here would make the class unreachable.
			if !scopedHTTPRouteSupportsTenantFilter(req) {
				t.Fatalf("%s: is in scopedTokenAdvertisedRoutes but scopedHTTPRouteSupportsTenantFilter rejects it, so its %s class is never consulted", name, className)
			}
		})
	}
}

// TestScopedRouteClassUnledgeredTransportRoutesNeedCallerGrant pins the
// fail-closed default for the two routes that clear
// scopedHTTPRouteSupportsTenantFilter without a scopedTokenAdvertisedRoutes
// entry: the MCP transport paths, which carry no OpenAPI operation and so no
// surface-inventory name to ledger. They have no class, so an all-scope
// browser session on them must fall through to the
// BrowserSessionRoutePolicy check rather than being waved past it.
//
// Known residual: these two names are hardcoded, because nothing in the
// package enumerates "clears scopedHTTPRouteSupportsTenantFilter but is
// absent from implementedAPIRouteSurfaces". That set cannot be derived from
// the surface inventory, since being absent from the inventory is what
// defines it, and scopedHTTPRouteSupportsTenantFilter is a predicate over
// requests rather than a list that can be walked. So a future allowlisted
// route that is likewise not inventoried would not be caught here. That is
// survivable in one direction and not the other: left alone it lands on the
// grant_bound fail-closed default, which is the safe answer, and the only way
// it becomes an opening is if someone also wires it into
// scopedRouteNeedsNoCallerGrant -- an edit to that closed union, where the
// reviewer is looking straight at the admission decision.
func TestScopedRouteClassUnledgeredTransportRoutesNeedCallerGrant(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"GET /sse", "POST /mcp/message"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, ledgered := scopedTokenAdvertisedRoutes[name]; ledgered {
				t.Fatalf("%s: gained a ledger entry -- give it a class and move it into TestScopedRouteClassLedgerAgreesWithPredicate instead of asserting the unledgered default here", name)
			}
			req := surfaceNameToRequest(t, name)
			if !scopedHTTPRouteSupportsTenantFilter(req) {
				t.Fatalf("%s: left the scoped-route allowlist; this test no longer proves anything about all-scope admission", name)
			}
			if scopedRouteNeedsNoCallerGrant(req) {
				t.Fatalf("%s: scopedRouteNeedsNoCallerGrant = true, want false -- an unledgered allowlisted route must stay policy-gated for all-scope sessions", name)
			}
		})
	}
}

// TestScopedRouteClassZeroValueIsGrantBound pins the fail-closed default an
// unclassified ledger entry inherits. A contributor who adds a route to
// scopedTokenAdvertisedRoutes with `{}`-style shorthand, or a future refactor
// that reorders the const block, would otherwise silently hand every such
// route the all-scope opening.
func TestScopedRouteClassZeroValueIsGrantBound(t *testing.T) {
	t.Parallel()

	var zero scopedRouteClass
	if zero != scopedRouteGrantBound {
		t.Fatalf("zero scopedRouteClass = %d, want scopedRouteGrantBound (%d)", zero, scopedRouteGrantBound)
	}
	if zero.admitsAllScopesSessionWithoutPolicy() {
		t.Fatal("the zero scopedRouteClass admits all-scope sessions without the policy check; it must fail closed")
	}
	for class, name := range scopedRouteClassNames {
		want := name == "identity_bound" || name == "tenant_data_free"
		if got := class.admitsAllScopesSessionWithoutPolicy(); got != want {
			t.Errorf("%s.admitsAllScopesSessionWithoutPolicy() = %t, want %t", name, got, want)
		}
	}
}

// compile-time proof the predicate keeps the http.Request-shaped signature the
// middleware calls it with; a change to a path-string argument would lose the
// method, which several matchers in the union depend on.
var _ func(*http.Request) bool = scopedRouteNeedsNoCallerGrant
