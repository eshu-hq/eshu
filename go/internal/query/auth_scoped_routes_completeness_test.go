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

// openAPIScopedTokenSupportRoutes parses the served OpenAPI spec
// (OpenAPISpec()) and returns the "METHOD /path" surface name for every
// operation carrying the "x-scoped-token-support": true marker declared
// directly in its openapi_paths_*.go source (see, e.g., the "get" operation
// in openapi_paths_repositories_freshness.go). This -- not the hand-maintained
// scopedTokenAdvertisedRoutes ledger -- is the #5154 gate's actual "advertised"
// signal: the marker sits in the same JSON operation object as the prose
// "Scoped tokens receive ..." description a contributor writes, so declaring
// scoped support in the description without adding the paired marker is a
// same-file, same-diff-hunk omission a reviewer can catch, not a fact that
// only lives in a separately hand-typed Go map that can drift unnoticed. A
// route whose description merely says "scoped" without this marker is, by
// design, NOT counted as advertised: prose is not proof.
func openAPIScopedTokenSupportRoutes(t *testing.T) map[string]struct{} {
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
			var op struct {
				ScopedTokenSupport bool `json:"x-scoped-token-support"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				t.Fatalf("parse operation %s %s: %v", method, path, err)
			}
			if op.ScopedTokenSupport {
				routes[strings.ToUpper(method)+" "+path] = struct{}{}
			}
		}
	}
	return routes
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
// "advertised scoped-token support" to the "x-scoped-token-support": true
// OpenAPI marker (openAPIScopedTokenSupportRoutes) -- the structured,
// machine-checkable fact issue #5154 requirement #1 demands -- and fails when
// that marker disagrees in either direction with the derived behavior of
// scopedHTTPRouteSupportsTenantFilter. It also cross-checks the
// hand-maintained scopedTokenAdvertisedRoutes ledger (auth_scoped_routes_completeness.go)
// against the same marker, so the ledger stays a reliable secondary audit
// trail rather than an independent, driftable source of truth.
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
func TestScopedTokenAllowlistCompleteness(t *testing.T) {
	surfaces := implementedAPIRouteSurfaces(t)
	surfaceSet := make(map[string]struct{}, len(surfaces))
	advertised := openAPIScopedTokenSupportRoutes(t)

	for _, name := range surfaces {
		surfaceSet[name] = struct{}{}
		req := surfaceNameToRequest(t, name)
		wired := scopedHTTPRouteSupportsTenantFilter(req)
		_, marked := advertised[name]
		_, ledgered := scopedTokenAdvertisedRoutes[name]

		switch {
		case marked && !wired:
			t.Errorf("%s: OpenAPI path entry carries \"x-scoped-token-support\": true, but scopedHTTPRouteSupportsTenantFilter(r) returns false -- wire a matcher for it (this is the #5150 P1 shape: an advertised-but-unwired route 403s every scoped/browser-session caller before the handler's own grant filtering runs)", name)
		case wired && !marked:
			t.Errorf("%s: scopedHTTPRouteSupportsTenantFilter(r) returns true for this route, but its OpenAPI path entry has no \"x-scoped-token-support\": true marker -- add the marker next to the route's operation in its openapi_paths_*.go source so the served contract matches the wired behavior", name)
		}
		switch {
		case marked && !ledgered:
			t.Errorf("%s: OpenAPI path entry carries \"x-scoped-token-support\": true, but the route is missing from scopedTokenAdvertisedRoutes -- add it to the ledger (auth_scoped_routes_completeness.go)", name)
		case ledgered && !marked:
			t.Errorf("%s: scopedTokenAdvertisedRoutes declares this route scoped, but its OpenAPI path entry has no \"x-scoped-token-support\": true marker -- add the marker in its openapi_paths_*.go source, or remove the stale ledger entry", name)
		}
	}

	for name := range scopedTokenAdvertisedRoutes {
		if _, ok := surfaceSet[name]; !ok {
			t.Errorf("%s: scopedTokenAdvertisedRoutes has a stale entry -- no implemented api_route surface has this name; remove the entry or fix the surface name to match capabilitycatalog.LoadSurfaceInventory()", name)
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
