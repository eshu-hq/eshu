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

// scopedFreshnessDeltaRoute allows scoped tokens to reach the three freshness
// delta reads: GET /api/v0/freshness/changed-since,
// GET /api/v0/freshness/generations, and
// GET /api/v0/freshness/services/changed-since. All three were #5167 Group B
// entries in pendingRowFilteringRoutes until their handlers gained the #5137
// pattern. The two repository-scope reads bind the caller's grant in the
// shipped SQL rather than in the handler:
//
//   - changed_since_sql.go:49-51 -- resolveChangedSinceScopeQuery's
//     ($3::boolean = false OR (scope.scope_kind = 'repository' AND
//     scope.source_key = ANY($4)) OR scope.scope_id = ANY($5)).
//   - generation_lifecycle_sql.go:114-116 -- listGenerationLifecycleQuery's
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
// The service read cannot use that shape. Its tables
// (service_materialization_generations, service_evidence_snapshots) carry only
// service_id, so there is no repository or scope column for a predicate to
// bind. FreshnessHandler.serviceChangedSinceGrantAdmits instead resolves the
// catalog service_id through the reducer_service_catalog_correlation facts --
// written from the same decision set that produced the generation -- under the
// caller's grant, and refuses BEFORE the lineage read. The refusal is the same
// service-not-found an absent service gets, so the route is not an existence
// oracle either (TestServiceChangedSinceTwoTenantGrantBoundary).
//
// All three routes are classified scopedRouteGrantBound in
// scopedTokenAdvertisedRoutes: an all-scope caller has no grant for those
// predicates to bind, so an all-scope browser session stays behind the
// BrowserSessionRoutePolicy mode check (#6450).
func scopedFreshnessDeltaRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	return r.URL.Path == "/api/v0/freshness/changed-since" ||
		r.URL.Path == "/api/v0/freshness/generations" ||
		r.URL.Path == "/api/v0/freshness/services/changed-since"
}
