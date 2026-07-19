// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// httpOperationMethodNames is the set of OpenAPI path-item keys that are HTTP
// operations, mirroring cmd/capability-inventory's unexported
// httpOperationMethods. Duplicated here (rather than imported) because that
// set lives in package main and importing it would create a query -> cmd
// dependency the module does not otherwise have.
var httpOperationMethodNames = map[string]struct{}{
	"get": {}, "put": {}, "post": {}, "delete": {},
	"options": {}, "head": {}, "patch": {}, "trace": {},
}

// implementedAPIRouteSurfaces returns every "METHOD /path" surface name from
// the generated surface inventory (derived from the served OpenAPI spec paths
// by cmd/capability-inventory's enumerateAPIRoutes) whose category is
// api_route and readiness is implemented -- the actual served route set the
// OpenAPI spec promises callers today.
func implementedAPIRouteSurfaces(t *testing.T) []string {
	t.Helper()
	inventory, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		t.Fatalf("capabilitycatalog.LoadSurfaceInventory() error = %v", err)
	}
	var names []string
	for _, surface := range inventory.Surfaces {
		if surface.Category != capabilitycatalog.SurfaceAPIRoute || surface.Readiness != capabilitycatalog.ReadinessImplemented {
			continue
		}
		names = append(names, surface.Name)
	}
	return names
}

// openAPIBoolMarkerRoutes parses the served OpenAPI spec (OpenAPISpec()) and
// returns the "METHOD /path" surface name for every operation carrying
// markerKey: true. Both #5154 tenant-scope markers
// (openAPIScopedTokenSupportRoutes, openAPIBrowserSessionOnlyRoutes) share
// this walk; only the marker key differs. Each operation is decoded into
// map[string]json.RawMessage rather than map[string]bool: an operation
// object's other fields (summary, parameters, responses, ...) are not
// booleans, so only the one field named markerKey is decoded further, and
// its absence is not an error -- most operations do not carry either marker.
func openAPIBoolMarkerRoutes(t *testing.T, markerKey string) map[string]struct{} {
	t.Helper()
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(OpenAPISpec()), &doc); err != nil {
		t.Fatalf("parse OpenAPISpec(): %v", err)
	}
	routes := map[string]struct{}{}
	for path, item := range doc.Paths {
		for method, raw := range item {
			if _, ok := httpOperationMethodNames[strings.ToLower(method)]; !ok {
				continue
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatalf("parse operation %s %s: %v", method, path, err)
			}
			markerRaw, ok := fields[markerKey]
			if !ok {
				continue
			}
			var marked bool
			if err := json.Unmarshal(markerRaw, &marked); err != nil {
				t.Fatalf("parse marker %s on operation %s %s: %v", markerKey, method, path, err)
			}
			if marked {
				routes[strings.ToUpper(method)+" "+path] = struct{}{}
			}
		}
	}
	return routes
}

// openAPIScopedTokenSupportRoutes returns the "METHOD /path" surface name for
// every operation carrying the "x-scoped-token-support": true marker
// declared directly in its openapi_paths_*.go source (see, e.g., the "get"
// operation in openapi_paths_repositories_freshness.go). This -- not the
// hand-maintained scopedTokenAdvertisedRoutes ledger -- is the #5154 gate's
// actual "advertised" signal: the marker sits in the same JSON operation
// object as the prose "Scoped tokens receive ..." description a contributor
// writes, so declaring scoped support in the description without adding the
// paired marker is a same-file, same-diff-hunk omission a reviewer can
// catch, not a fact that only lives in a separately hand-typed Go map that
// can drift unnoticed. A route whose description merely says "scoped"
// without this marker is, by design, NOT counted as advertised: prose is not
// proof.
//
// This marker asserts more than "scopedHTTPRouteSupportsTenantFilter admits
// the request": it asserts a scoped BEARER token gets a working (non-403,
// non-400-for-being-a-bearer-token) response from the handler. A route whose
// handler requires an actual browser-session cookie despite clearing the
// middleware allowlist (a scoped bearer is admitted, then rejected by the
// handler itself) must use openAPIBrowserSessionOnlyRoutes's marker instead
// -- see its doc comment for the codex PR #5185 review finding that
// motivated the split.
func openAPIScopedTokenSupportRoutes(t *testing.T) map[string]struct{} {
	t.Helper()
	return openAPIBoolMarkerRoutes(t, "x-scoped-token-support")
}

// openAPIBrowserSessionOnlyRoutes returns the "METHOD /path" surface name for
// every operation carrying the "x-browser-session-only": true marker. These
// routes clear scopedHTTPRouteSupportsTenantFilter (so a browser-session
// cookie caller can reach them under the tenant-filter allowlist), but their
// handler hard-requires AuthModeBrowserSession -- a real browser-session
// cookie, not a scoped bearer token -- and rejects any other caller before
// doing any tenant-scoped work: BrowserSessionHandler.handleCurrent/
// handleLogout/handleSwitch (browser_session_handler.go) and
// BrowserSessionListHandler.handleListSessions (browser_session_list.go)
// each check auth.Mode == AuthModeBrowserSession (or the equivalent
// requestUsesBrowserSession helper) and 400/401 otherwise.
//
// codex PR #5185 review (P2, valid): GET/DELETE /api/v0/auth/browser-session
// and PATCH /api/v0/auth/browser-session/context originally carried
// "x-scoped-token-support": true even though their handlers are cookie-only
// -- a scoped bearer clears the allowlist and then fails in the handler, so
// the marker lied to OpenAPI consumers and to TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware
// would have (wrongly) asserted a 200. Auditing every other
// "x-scoped-token-support" route for the same auth.Mode-exclusivity pattern
// (grep for every auth.Mode ==/!= comparison in go/internal/query, excluding
// AuthMiddleware's own gating logic in auth.go) found one more instance,
// GET /api/v0/auth/sessions (BrowserSessionListHandler), which has the exact
// same bug shape. All four routes were moved to this marker; every other
// admin/all-scopes gate found in the same audit (admin_replay.go's
// AllScopes-only replay gate, local_identity_handler_helpers.go's
// requireSharedOperator, admin_identity_reads.go's auditScope) either gates
// on privilege level rather than auth.Mode identity, or serves a route that
// is not marked scoped-token-supported at all, so neither is a false claim.
func openAPIBrowserSessionOnlyRoutes(t *testing.T) map[string]struct{} {
	t.Helper()
	return openAPIBoolMarkerRoutes(t, "x-browser-session-only")
}

// openAPISharedKeyOnlyRoutes returns the "METHOD /path" surface name for
// every operation carrying the "x-shared-key-only": true marker (#5167 Group
// C). These routes execute caller-supplied Cypher with no bounded selector
// to intersect against a grant -- POST /api/v0/code/cypher
// (runReadOnlyCypher) and POST /api/v0/code/visualize
// (runReadOnlyCypherVisualization) -- so unlike the other two markers, a
// shared-key-only route is expected to clear scopedHTTPRouteSupportsTenantFilter
// as false: it stays off the tenant-filter allowlist entirely, reachable only
// by shared-key and all-scope callers. See IsSharedKeyOnlyRoute
// (auth_scoped_routes_shared_key_only.go) for the production accessor the
// go/internal/mcp exhaustiveness gate uses on dispatched requests.
func openAPISharedKeyOnlyRoutes(t *testing.T) map[string]struct{} {
	t.Helper()
	return openAPIBoolMarkerRoutes(t, "x-shared-key-only")
}

// surfaceNameToRequest builds an *http.Request from a "METHOD /path" surface
// name for probing scopedHTTPRouteSupportsTenantFilter or the real
// AuthMiddlewareWithScopedTokens. OpenAPI path templates (e.g. "{repo_id}")
// are passed through unchanged: every allowlist path matcher validates only
// that a path segment is present and contains no "/", never the concrete ID
// shape, so the brace-wrapped template exercises the same branch a live ID
// would.
func surfaceNameToRequest(t *testing.T, name string) *http.Request {
	t.Helper()
	method, path, ok := strings.Cut(name, " ")
	if !ok {
		t.Fatalf("surface name %q has no METHOD/path separator", name)
	}
	return httptest.NewRequest(method, path, nil)
}

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
		if _, ok := surfaceSet[name]; !ok {
			t.Errorf("%s: sharedKeyOnlyRoutes has a stale entry -- no implemented api_route surface has this name; remove the entry or fix the surface name to match capabilitycatalog.LoadSurfaceInventory()", name)
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
// parameterized Group B entry (pendingRowFilteringEvidenceRelationshipRoute,
// GET /api/v0/evidence/relationships/{id}) is intentionally not in either
// literal ledger, so it cannot collide with a literal-map entry.
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
func TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware(t *testing.T) {
	advertised := openAPIScopedTokenSupportRoutes(t)
	names := make([]string, 0, len(advertised))
	for name := range advertised {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
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
