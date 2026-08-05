// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// openAPIKnownDriftRoutes parses .github/openapi-known-drift.txt and returns
// the "METHOD /path" surface name for every route intentionally excluded from
// the public OpenAPI surface. scripts/verify-openapi.sh subtracts these from
// the HandleFunc side so a route with a live handler but no openapi_paths_*.go
// entry stays green instead of tripping the drift gate. A shared-key-only
// route could in principle be OpenAPI-excluded this way: it would have a real
// handler and a sharedKeyOnlyRoutes Go ledger entry, but must NOT carry an
// x-shared-key-only OpenAPI marker or appear in the served surface inventory,
// because it would not be part of the public OpenAPI at all. (POST
// /api/v0/code/visualize was exactly this case, #3781, until #5762 gave it a
// real openapi_paths_code_graph.go entry and removed it from known-drift.)
// This set lets TestScopedTokenAllowlistCompleteness validate such a route by
// Go-ledger membership alone rather than demanding a marker it deliberately
// lacks.
func openAPIKnownDriftRoutes(t *testing.T) map[string]struct{} {
	t.Helper()
	path := filepath.Join("..", "..", "..", ".github", "openapi-known-drift.txt")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read known-drift file %s: %v", path, err)
	}
	routes := map[string]struct{}{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		routes[line] = struct{}{}
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
