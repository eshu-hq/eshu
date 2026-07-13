// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

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

// TestScopedTokenAllowlistCompleteness is the #5154 CI gate: it fails when the
// hand-maintained scopedTokenAdvertisedRoutes ledger (auth_scoped_routes_completeness.go)
// and the derived behavior of scopedHTTPRouteSupportsTenantFilter disagree in
// either direction. The #5150 review retro P1 was exactly the first
// direction below -- GET /api/v0/repositories/{repo_id}/freshness advertised
// scoped-token support in its handler doc, OpenAPI description, and the
// HTTP-API reference, but scopedHTTPRouteSupportsTenantFilter had no matcher
// for it, so every scoped and browser-session caller got a middleware 403
// before the handler's own grant filtering ever ran.
func TestScopedTokenAllowlistCompleteness(t *testing.T) {
	surfaces := implementedAPIRouteSurfaces(t)
	surfaceSet := make(map[string]struct{}, len(surfaces))

	for _, name := range surfaces {
		surfaceSet[name] = struct{}{}
		req := surfaceNameToRequest(t, name)
		wired := scopedHTTPRouteSupportsTenantFilter(req)
		_, advertised := scopedTokenAdvertisedRoutes[name]
		switch {
		case advertised && !wired:
			t.Errorf("%s: scopedTokenAdvertisedRoutes declares this route scoped, but scopedHTTPRouteSupportsTenantFilter(r) returns false -- wire a matcher for it (this is the #5150 P1 shape: an advertised-but-unwired route 403s every scoped/browser-session caller before the handler's own grant filtering runs)", name)
		case wired && !advertised:
			t.Errorf("%s: scopedHTTPRouteSupportsTenantFilter(r) returns true for this route, but it is missing from scopedTokenAdvertisedRoutes -- add it to the ledger (auth_scoped_routes_completeness.go) so the route's scoped-token support is an intentional, reviewed declaration rather than an accidental side effect of a shared matcher", name)
		}
	}

	for name := range scopedTokenAdvertisedRoutes {
		if _, ok := surfaceSet[name]; !ok {
			t.Errorf("%s: scopedTokenAdvertisedRoutes has a stale entry -- no implemented api_route surface has this name; remove the entry or fix the surface name to match capabilitycatalog.LoadSurfaceInventory()", name)
		}
	}
}

// TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware is the
// #5154 convention-check gate: it proves every entry in
// scopedTokenAdvertisedRoutes actually clears a real
// AuthMiddlewareWithScopedTokens round trip under an all-scopes scoped
// token, instead of relying on a per-route bare-mux handler test. The #5150
// incident's handler-level tests mounted a bare http.NewServeMux(), which
// never runs AuthMiddlewareWithScopedTokens at all, so those tests stayed
// green while every real scoped/browser-session caller was rejected with 403
// ahead of the handler. This test exercises the actual middleware for every
// advertised route so that false-green pattern cannot recur.
func TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware(t *testing.T) {
	names := make([]string, 0, len(scopedTokenAdvertisedRoutes))
	for name := range scopedTokenAdvertisedRoutes {
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
				t.Fatalf("next handler not called; AuthMiddlewareWithScopedTokens rejected an advertised scoped route with status %d, body = %s", rec.Code, rec.Body.String())
			}
			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}
