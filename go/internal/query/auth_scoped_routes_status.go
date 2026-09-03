// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

// scopedCollectorStatusRoute allows scoped tokens to reach the collector status
// list; the handler collapses per-instance rows into aggregate readback.
func scopedCollectorStatusRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/collectors"
}

// scopedCollectorReadinessRoute allows scoped tokens to reach the collector
// readiness read model. The handler redacts the per-instance identifier for
// scoped callers, so the route must be reachable for that redaction to apply.
func scopedCollectorReadinessRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	return r.URL.Path == "/api/v0/status/collector-readiness" ||
		r.URL.Path == "/api/v0/collector-readiness"
}

// scopedIngesterStatusRoute allows scoped tokens to reach the ingester status
// list and per-ingester detail routes.
func scopedIngesterStatusRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if r.URL.Path == "/api/v0/status/ingesters" {
		return true
	}
	const prefix = "/api/v0/status/ingesters/"
	ingester := strings.TrimPrefix(r.URL.Path, prefix)
	return ingester != r.URL.Path && ingester != "" && !strings.Contains(ingester, "/")
}

// scopedFreshnessDeltaRoute allows scoped tokens to reach the two freshness
// delta reads whose rows can be bound to the caller's grant in SQL:
// GET /api/v0/freshness/changed-since and GET /api/v0/freshness/generations.
// Both were #5167 Group B entries in pendingRowFilteringRoutes until their
// handlers gained the #5137 pattern. They bind the caller's grant in the
// shipped SQL rather than in the handler:
//
//   - resolveChangedSinceScopeQuery (changed_since_sql.go) --
//     ($3::boolean = false OR (scope.scope_kind = 'repository' AND
//     scope.source_key = ANY($4)) OR scope.scope_id = ANY($5)).
//   - listGenerationLifecycleQuery (generation_lifecycle_sql.go) --
//     ($8::boolean = false OR (scope.scope_kind = 'repository' AND
//     scope.source_key = ANY($9)) OR generation.scope_id = ANY($10)).
//
// The binding is on the resolved ROW, not on the selector the caller typed,
// because a repository grant authorizes a repository-kind scope through
// source_key and the raw scope_id normally differs from that key. An ungranted
// selector therefore resolves to no row and is served as the route's ordinary
// not-found, byte-identical to a selector that names nothing at all, so the
// route is not an existence oracle for another tenant's scopes or generations
// (TestChangedSinceTwoTenantGrantBoundary,
// TestGenerationLifecycleTwoTenantGrantBoundary).
//
// GET /api/v0/freshness/services/changed-since is deliberately NOT here. Its
// lineage tables (service_materialization_generations,
// service_evidence_snapshots) carry only service_id, with no column naming the
// tenant a row belongs to, so the only available grant check is
// FreshnessHandler.serviceChangedSinceGrantAdmits probing the
// reducer_service_catalog_correlation facts. Those probes see a correlation
// only while it is live in its own scope's active generation, so a tenant
// whose correlation has aged out stops contesting the service_id even though
// its lineage generation is still active, and the other tenant reads that
// lineage. Promoting the route on that fence alone would turn a scoped
// caller's 403 into a cross-tenant read, which is exactly what
// pendingRowFilteringRoutes' header forbids, so the route stays on that ledger
// until #6475 puts an ownership column on the lineage rows. The fence itself
// ships and is tested (TestServiceChangedSinceTwoTenantGrantBoundary) as the
// first half of that promotion.
//
// Both promoted routes are classified scopedRouteGrantBound in
// scopedTokenAdvertisedRoutes, and both all-scope caller shapes now read that
// class through their own denial function:
//
//   - An all-scope BROWSER SESSION goes through browserSessionRouteDenialReason,
//     which auth.go reaches under the `auth.Mode == AuthModeBrowserSession`
//     branch, so a hosted fail-closed BrowserSessionRoutePolicy refuses it
//     (#6450).
//   - An all-scope BEARER goes through scopedBearerRouteDenialReason, under
//     the scoped-resolver branch, and is refused by the same policy (#6450
//     residual item 1). Both minters produce this shape: an OIDC bearer
//     resolved with an admin group grant carries AllScopes onto its
//     AuthContext (internal/oidcbearer/resolver.go), and a file-backed
//     registry token can carry the same flag (internal/scopedtoken/registry.go).
//     Without the refusal, RepositoryAccessFilter.Scoped() is false for such a
//     caller, so $3/$8 short-circuit the two SQL predicates above and the read
//     is served across the whole corpus -- which is what these routes did for
//     an all-scope bearer between their promotion and that fix, both landing
//     in the same change.
//
// A restricted session or token, of either kind, is admitted and its grant
// binds normally: the refusal is for callers whose grant the predicate cannot
// bind, not for scoped callers generally.
func scopedFreshnessDeltaRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	return r.URL.Path == "/api/v0/freshness/changed-since" ||
		r.URL.Path == "/api/v0/freshness/generations"
}
