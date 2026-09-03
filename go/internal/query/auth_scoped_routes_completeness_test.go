// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// TestScopedTokenAllowlistCompleteness is the #5154 CI gate. It anchors
// "advertised tenant-scope support" to the union of the two mutually
// exclusive OpenAPI markers -- "x-scoped-token-support": true
// (openAPIScopedTokenSupportRoutes) and "x-browser-session-only": true
// (openAPIBrowserSessionOnlyRoutes) -- the structured, machine-checkable
// facts issue #5154 requirement #1 demands, and fails when that union
// disagrees in either direction with the derived behavior of
// scopedHTTPRouteSupportsTenantFilter, or when a route carries both markers
// at once. It also cross-checks the hand-maintained scopedTokenAdvertisedRoutes
// ledger (auth_scoped_routes_completeness.go) against the same union, so the
// ledger stays a reliable secondary audit trail rather than an independent,
// driftable source of truth.
//
// The #5150 review retro P1 was exactly the marker-vs-wired direction below:
// GET /api/v0/repositories/{repo_id}/freshness advertised scoped-token
// support in its handler doc, OpenAPI description, and the HTTP-API
// reference (and today also carries the marker), but
// scopedHTTPRouteSupportsTenantFilter had no matcher for it, so every scoped
// and browser-session caller got a middleware 403 before the handler's own
// grant filtering ever ran. A route that only ever advertised scoped support
// in prose -- never in the ledger, never wired -- would have passed a
// ledger-only gate silently; anchoring to the marker instead means the
// prose-adjacent structured fact is what the gate reads.
//
// This test only proves allowlist membership is honestly declared for
// *some* form of tenant-scoped access. It does not by itself prove which
// form (scoped bearer token vs browser-session cookie) actually works --
// that is TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware's
// job for "x-scoped-token-support" routes and
// TestScopedBearerTokenRejectedByBrowserSessionOnlyRoutes's job (the inverse
// assertion) for "x-browser-session-only" routes.
func TestScopedTokenAllowlistCompleteness(t *testing.T) {
	surfaces := implementedAPIRouteSurfaces(t)
	surfaceSet := make(map[string]struct{}, len(surfaces))
	tokenAdvertised := openAPIScopedTokenSupportRoutes(t)
	browserOnlyAdvertised := openAPIBrowserSessionOnlyRoutes(t)
	sharedKeyOnlyAdvertised := openAPISharedKeyOnlyRoutes(t)
	knownDrift := openAPIKnownDriftRoutes(t)

	for name := range tokenAdvertised {
		if _, both := browserOnlyAdvertised[name]; both {
			t.Errorf("%s: carries both \"x-scoped-token-support\": true and \"x-browser-session-only\": true -- exactly one tenant-scope marker must apply per route", name)
		}
		if _, both := sharedKeyOnlyAdvertised[name]; both {
			t.Errorf("%s: carries both \"x-scoped-token-support\": true and \"x-shared-key-only\": true -- exactly one tenant-scope marker must apply per route", name)
		}
	}
	for name := range browserOnlyAdvertised {
		if _, both := sharedKeyOnlyAdvertised[name]; both {
			t.Errorf("%s: carries both \"x-browser-session-only\": true and \"x-shared-key-only\": true -- exactly one tenant-scope marker must apply per route", name)
		}
	}

	for _, name := range surfaces {
		surfaceSet[name] = struct{}{}
		req := surfaceNameToRequest(t, name)
		wired := scopedHTTPRouteSupportsTenantFilter(req)
		_, tokenMarked := tokenAdvertised[name]
		_, browserOnlyMarked := browserOnlyAdvertised[name]
		_, sharedKeyOnlyMarked := sharedKeyOnlyAdvertised[name]
		marked := tokenMarked || browserOnlyMarked
		_, ledgered := scopedTokenAdvertisedRoutes[name]
		_, sharedKeyOnlyLedgered := sharedKeyOnlyRoutes[name]

		switch {
		case marked && !wired:
			t.Errorf("%s: OpenAPI path entry carries a tenant-scope marker, but scopedHTTPRouteSupportsTenantFilter(r) returns false -- wire a matcher for it (this is the #5150 P1 shape: an advertised-but-unwired route 403s every scoped/browser-session caller before the handler's own grant filtering runs)", name)
		case wired && !marked:
			t.Errorf("%s: scopedHTTPRouteSupportsTenantFilter(r) returns true for this route, but its OpenAPI path entry has neither \"x-scoped-token-support\": true nor \"x-browser-session-only\": true -- add the marker that matches the handler's actual auth.Mode requirement next to the route's operation in its openapi_paths_*.go source so the served contract matches the wired behavior", name)
		}
		switch {
		case marked && !ledgered:
			t.Errorf("%s: OpenAPI path entry carries a tenant-scope marker, but the route is missing from scopedTokenAdvertisedRoutes -- add it to the ledger (auth_scoped_routes_completeness.go)", name)
		case ledgered && !marked:
			t.Errorf("%s: scopedTokenAdvertisedRoutes declares this route scoped, but its OpenAPI path entry has neither tenant-scope marker -- add the marker that matches the handler's actual auth.Mode requirement in its openapi_paths_*.go source, or remove the stale ledger entry", name)
		}

		// #5167 Group C: a shared-key-only marked route must be the opposite
		// of the other two markers -- it must stay OFF the tenant-filter
		// allowlist (wired == false), since its handler executes
		// caller-supplied Cypher with nothing to bind a grant against, and it
		// must be declared in the sharedKeyOnlyRoutes ledger
		// (auth_scoped_routes_shared_key_only.go).
		switch {
		case sharedKeyOnlyMarked && wired:
			t.Errorf("%s: carries \"x-shared-key-only\": true but scopedHTTPRouteSupportsTenantFilter(r) returns true -- a shared-key-only route must never clear the tenant-filter allowlist", name)
		case sharedKeyOnlyMarked && !sharedKeyOnlyLedgered:
			t.Errorf("%s: OpenAPI path entry carries \"x-shared-key-only\": true, but the route is missing from sharedKeyOnlyRoutes -- add it to the ledger (auth_scoped_routes_shared_key_only.go)", name)
		case sharedKeyOnlyLedgered && !sharedKeyOnlyMarked:
			t.Errorf("%s: sharedKeyOnlyRoutes declares this route shared-key-only, but its OpenAPI path entry has no \"x-shared-key-only\": true marker -- add the marker in its openapi_paths_*.go source, or remove the stale ledger entry", name)
		}
	}

	for name := range scopedTokenAdvertisedRoutes {
		if _, ok := surfaceSet[name]; !ok {
			t.Errorf("%s: scopedTokenAdvertisedRoutes has a stale entry -- no implemented api_route surface has this name; remove the entry or fix the surface name to match capabilitycatalog.LoadSurfaceInventory()", name)
		}
	}
	for name := range sharedKeyOnlyRoutes {
		if _, ok := surfaceSet[name]; ok {
			// The route is in the served OpenAPI surface (e.g.
			// POST /api/v0/code/cypher): the loop above already validated its
			// x-shared-key-only marker, so nothing more to check here.
			continue
		}
		// The route is not in the served surface inventory. That is legitimate
		// only when the route is intentionally OpenAPI-excluded via
		// .github/openapi-known-drift.txt: it would be a real, shared-key-only
		// handler that verify-openapi.sh treats as covered, so this Go ledger
		// would be its sole machine-checkable classification -- it must NOT
		// also carry an x-shared-key-only OpenAPI marker (it has no OpenAPI
		// entry to carry one). Any other missing surface is a genuinely stale
		// ledger entry. (POST /api/v0/code/visualize was this case, #3781,
		// until #5762 gave it a real openapi_paths_code_graph.go entry.)
		if _, excluded := knownDrift[name]; !excluded {
			t.Errorf("%s: sharedKeyOnlyRoutes has a stale entry -- no implemented api_route surface has this name and it is not in .github/openapi-known-drift.txt; remove the entry, fix the surface name to match capabilitycatalog.LoadSurfaceInventory(), or add it to known-drift if the route is intentionally OpenAPI-excluded", name)
			continue
		}
		if _, marked := sharedKeyOnlyAdvertised[name]; marked {
			t.Errorf("%s: is in .github/openapi-known-drift.txt (intentionally OpenAPI-excluded) yet also carries an \"x-shared-key-only\": true OpenAPI marker -- an OpenAPI-excluded route has no OpenAPI operation to mark; remove the marker or remove the route from known-drift", name)
		}
	}
}

// TestPendingRowFilteringRoutesDisjointFromScopedAndSharedKey is the #5167 W1
// guardrail for the family workstreams (W2-W6). Each of the three route
// classifications is a distinct terminal state, so a route may belong to
// exactly one: the scoped-token allowlist ledger
// (scopedTokenAdvertisedRoutes), the shared-key-only ledger
// (sharedKeyOnlyRoutes), or the pending-row-filtering backlog
// (pendingRowFilteringRoutes). This test fails the build when
// pendingRowFilteringRoutes overlaps either of the other two, which is exactly
// the mistake a family workstream makes when it lands the #5137 row-filtering
// pattern for a Group B route and adds it to scopedTokenAdvertisedRoutes
// (plus a matcher and marker) without deleting the now-stale
// pendingRowFilteringRoutes entry. Without this check the route would be both
// allowlisted and still advertised as an unfiltered gap -- a contradiction the
// two staleness checks above do not catch, because both entries would name a
// real implemented surface.
//
// All three maps are package-level vars in this package, so this literal-map
// disjointness check lives here rather than in the go/internal/mcp
// exhaustiveness test, which only sees the exported surface slices. The one
// parameterized Group B entry, GET /api/v0/evidence/relationships/{id}, was
// cleared by the #5167 F-6 W6 cloud/aws family workstream
// (scopedRelationshipEvidenceRoute, auth_scoped_routes_cloud.go) and no
// longer exists as a pending-ledger special case.
func TestPendingRowFilteringRoutesDisjointFromScopedAndSharedKey(t *testing.T) {
	for name := range pendingRowFilteringRoutes {
		if _, ok := scopedTokenAdvertisedRoutes[name]; ok {
			t.Errorf("%s: is in BOTH pendingRowFilteringRoutes and scopedTokenAdvertisedRoutes -- when a family workstream (W2-W6) allowlists a Group B route after adding real grant filtering, it MUST delete the route from pendingRowFilteringRoutes (auth_scoped_routes_pending_row_filtering.go); a route cannot be both allowlisted and advertised as an unfiltered pending gap", name)
		}
		if _, ok := sharedKeyOnlyRoutes[name]; ok {
			t.Errorf("%s: is in BOTH pendingRowFilteringRoutes and sharedKeyOnlyRoutes -- a route is either a pending row-filtering gap or permanently shared-key-only, never both; remove it from pendingRowFilteringRoutes (auth_scoped_routes_pending_row_filtering.go)", name)
		}
	}
}

// TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware is the
// #5154 convention-check gate. It sources its route set directly from
// openAPIScopedTokenSupportRoutes (the OpenAPI marker), not from the
// hand-maintained ledger, and proves every one of those routes actually
// clears a real AuthMiddlewareWithScopedTokens round trip under an
// all-scopes scoped token, instead of relying on a per-route bare-mux
// handler test. The #5150 incident's handler-level tests mounted a bare
// http.NewServeMux(), which never runs AuthMiddlewareWithScopedTokens at
// all, so those tests stayed green while every real scoped/browser-session
// caller was rejected with 403 ahead of the handler. This test exercises the
// actual middleware for every marker-advertised route, closing that gap for
// every route this test currently covers; a route that gains the marker
// without ever appearing in the live OpenAPI spec (for example a dead,
// unreferenced openapi_paths_*.go constant) would not be caught by this test
// alone -- that failure mode is covered separately by TestServeOpenAPI and
// the surface-inventory drift gate, which both operate on the same served
// OpenAPISpec() this test reads.
//
// This test deliberately does not cover "x-browser-session-only" routes: a
// scoped bearer token is admitted past the middleware for those routes (by
// design -- see openAPIBrowserSessionOnlyRoutes) but must NOT get a 2xx from
// the handler. TestScopedBearerTokenRejectedByBrowserSessionOnlyRoutes
// proves that inverse.
//
// It also deliberately skips the two routes in
// scopedTokenAllScopeBearerRefusedRoutes: GET /api/v0/freshness/changed-since
// and GET /api/v0/freshness/generations are marker-advertised and DO clear
// the middleware for a scoped bearer carrying a real repository or scope
// grant, but this test's fixture is an all-scopes token, and PR #6472 review
// finding 1 closed the #6450 residual specifically for these two --
// scopedFreshnessDeltaRouteRefusesAllScopeBearer refuses an all-scope bearer
// here in every deployment mode, unlike the rest of this test's routes, which
// still admit one (the #6450 residual, open elsewhere).
// TestAllScopeBearerTokenReachesFreshnessDeltaPairOnly
// (auth_scoped_routes_freshness_delta_test.go) is this pair's inverse: it
// proves the 403 and its reason code against the same real middleware, plus
// the ordinary-grant bearer's 200 (TestScopedTokenReachesFreshnessDeltaPairOnly).
func TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware(t *testing.T) {
	advertised := openAPIScopedTokenSupportRoutes(t)
	names := make([]string, 0, len(advertised))
	for name := range advertised {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if scopedTokenAllScopeBearerRefusedRoutes[name] {
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{
				context: AuthContext{
					Mode:        AuthModeScoped,
					TenantID:    "tenant_a",
					WorkspaceID: "workspace_a",
					AllScopes:   true,
				},
				ok: true,
			}
			called := false
			handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))

			req := surfaceNameToRequest(t, name)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if !called {
				t.Fatalf("next handler not called; AuthMiddlewareWithScopedTokens rejected a marker-advertised scoped route with status %d, body = %s", rec.Code, rec.Body.String())
			}
			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

// scopedTokenAllScopeBearerRefusedRoutes names the marker-advertised routes
// TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware
// excludes from its otherwise-universal all-scopes-bearer fixture; see that
// test's doc comment for why. TestScopedTokenAllScopeBearerRefusedRoutesAreAdvertised
// keeps this map from going stale if a listed route ever loses its
// "x-scoped-token-support" marker.
var scopedTokenAllScopeBearerRefusedRoutes = map[string]bool{
	"GET /api/v0/freshness/changed-since": true,
	"GET /api/v0/freshness/generations":   true,
}

// TestScopedTokenAllScopeBearerRefusedRoutesAreAdvertised fails if
// scopedTokenAllScopeBearerRefusedRoutes ever names a route the marker union
// no longer advertises: an unmatched `continue` in
// TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware would
// otherwise silently stop excluding anything, and the map would look correct
// while covering nothing.
func TestScopedTokenAllScopeBearerRefusedRoutesAreAdvertised(t *testing.T) {
	advertised := openAPIScopedTokenSupportRoutes(t)
	for name := range scopedTokenAllScopeBearerRefusedRoutes {
		if _, ok := advertised[name]; !ok {
			t.Errorf("%s: in scopedTokenAllScopeBearerRefusedRoutes but not in openAPIScopedTokenSupportRoutes(t) -- the exclusion in TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware is now a no-op; fix the surface name or remove the stale entry", name)
		}
	}
}

// TestScopedBearerTokenRejectedByBrowserSessionOnlyRoutes is the inverse of
// TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware for
// "x-browser-session-only" marked routes (codex PR #5185 review, P2 --
// see openAPIBrowserSessionOnlyRoutes's doc comment for the finding). It
// mounts the real production handlers (BrowserSessionHandler,
// BrowserSessionListHandler), not a stub, behind the real
// AuthMiddlewareWithScopedTokens, and proves a scoped bearer token that
// clears the middleware allowlist still never gets a 2xx from the handler:
// the handler's own auth.Mode == AuthModeBrowserSession requirement is what
// actually protects these routes, not the allowlist. This is the honest
// counterpart to the scoped-token-support round trip -- it is cheap because
// these routes' fake stores never need seeded data: every rejection happens
// before the handler touches its Store.
func TestScopedBearerTokenRejectedByBrowserSessionOnlyRoutes(t *testing.T) {
	advertised := openAPIBrowserSessionOnlyRoutes(t)
	names := make([]string, 0, len(advertised))
	for name := range advertised {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("openAPIBrowserSessionOnlyRoutes(t) returned no routes; expected getBrowserSession, deleteBrowserSession, switchBrowserSessionContext, and listAuthSessions to carry \"x-browser-session-only\": true")
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := &fakeBrowserSessionListStore{}
			mux := http.NewServeMux()
			(&BrowserSessionHandler{Store: store}).Mount(mux)
			(&BrowserSessionListHandler{Store: store}).Mount(mux)

			resolver := &fakeScopedTokenResolver{
				context: AuthContext{
					Mode:        AuthModeScoped,
					TenantID:    "tenant_a",
					WorkspaceID: "workspace_a",
					AllScopes:   true,
				},
				ok: true,
			}
			handler := AuthMiddlewareWithScopedTokens("", resolver, mux)

			req := surfaceNameToRequest(t, name)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code >= 200 && rec.Code < 300 {
				t.Fatalf("status = %d, want a non-2xx rejection; a scoped bearer token cleared the middleware allowlist and then got a successful response from a browser-session-only handler, contradicting its \"x-browser-session-only\" marker; body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}
