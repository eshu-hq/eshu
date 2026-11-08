// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// This file bridges the D13 (#9669) row-2 leaf-2b drain: the Group C auth
// ledgers (the shared-key-only and pending-row-filtering ledgers, the
// scoped-token allowlist decision and its MCP surfaces, the
// browser-session/all-scope route policy, and the route-denial signal) live
// in query/auth now. Every name below delegates to that single home, so the
// in-tree middleware, the root tests, and the MCP exhaustiveness gate keep
// compiling and behaving exactly as before with no duplicated ledger.

// BrowserSessionRoutePolicy is the auth-owned tenant-bound all-scopes route
// policy. Its zero value is fail-closed.
type BrowserSessionRoutePolicy = auth.BrowserSessionRoutePolicy

// sharedKeyOnlyRoutes is the auth-owned #5167 Group C ledger.
var sharedKeyOnlyRoutes = auth.SharedKeyOnlyRoutes

// pendingRowFilteringRoutes is the auth-owned #5167 Group B ledger.
var pendingRowFilteringRoutes = auth.PendingRowFilteringRoutes

// httpOnlySharedKeyOnlyRoutes is the auth-owned transport-ownership split.
var httpOnlySharedKeyOnlyRoutes = auth.HttpOnlySharedKeyOnlyRoutes

// Governance-audit reason codes for the two route-admission refusals. Single
// home: auth.ScopedRouteNotEnabledReason and
// auth.ScopedRouteAllScopeGrantRequiredReason.
const (
	scopedRouteNotEnabledReason            = auth.ScopedRouteNotEnabledReason
	scopedRouteAllScopeGrantRequiredReason = auth.ScopedRouteAllScopeGrantRequiredReason
)

// IsSharedKeyOnlyRoute reports whether r targets a shared-key/all-scope-only
// route. Single home: auth.IsSharedKeyOnlyRoute.
func IsSharedKeyOnlyRoute(r *http.Request) bool {
	return auth.IsSharedKeyOnlyRoute(r)
}

// IsPendingRowFilteringRoute reports whether r targets a route tracked as a
// pending row-filtering gap. Single home:
// auth.IsPendingRowFilteringRoute.
func IsPendingRowFilteringRoute(r *http.Request) bool {
	return auth.IsPendingRowFilteringRoute(r)
}

// ScopedHTTPRouteSupportsTenantFilter reports whether r is on the
// scoped-token tenant-filter allowlist. Single home:
// auth.ScopedHTTPRouteSupportsTenantFilter.
func ScopedHTTPRouteSupportsTenantFilter(r *http.Request) bool {
	return auth.ScopedHTTPRouteSupportsTenantFilter(r)
}

// SharedKeyOnlyRouteSurfaces lists the MCP-reachable shared-key-only
// surfaces. Single home: auth.SharedKeyOnlyRouteSurfaces.
func SharedKeyOnlyRouteSurfaces() []string {
	return auth.SharedKeyOnlyRouteSurfaces()
}

// PendingRowFilteringRouteSurfaces lists the pending row-filtering surfaces.
// Single home: auth.PendingRowFilteringRouteSurfaces.
func PendingRowFilteringRouteSurfaces() []string {
	return auth.PendingRowFilteringRouteSurfaces()
}

// scopedHTTPRouteSupportsTenantFilter is the unexported allowlist decision
// the root tests pin directly. Single home:
// auth.ScopedHTTPRouteSupportsTenantFilter.
func scopedHTTPRouteSupportsTenantFilter(r *http.Request) bool {
	return auth.ScopedHTTPRouteSupportsTenantFilter(r)
}

// scopedRouteNeedsNoCallerGrant reports whether r is identity-bound or
// tenant-data-free. Single home: auth.ScopedRouteNeedsNoCallerGrant.
func scopedRouteNeedsNoCallerGrant(r *http.Request) bool {
	return auth.ScopedRouteNeedsNoCallerGrant(r)
}

// browserSessionRouteDenialReason decides whether a browser-session request
// may reach its handler. Single home:
// auth.BrowserSessionRouteDenialReason.
func browserSessionRouteDenialReason(r *http.Request, authCtx AuthContext, policy BrowserSessionRoutePolicy) string {
	return auth.BrowserSessionRouteDenialReason(r, authCtx, policy)
}

// scopedBearerRouteDenialReason is the scoped-bearer sibling admission
// decision. Single home: auth.ScopedBearerRouteDenialReason.
func scopedBearerRouteDenialReason(r *http.Request, authCtx AuthContext, policy BrowserSessionRoutePolicy) string {
	return auth.ScopedBearerRouteDenialReason(r, authCtx, policy)
}

// markScopedRouteDenied marks the route-denial signal
// WithScopedRouteDenialSignal installed, if any. Single home:
// auth.MarkScopedRouteDenied.
func markScopedRouteDenied(ctx context.Context) {
	auth.MarkScopedRouteDenied(ctx)
}

// The per-family matcher forwarders below exist for the root matcher unit
// tests, which pin each predicate directly. Every one delegates to the
// auth-owned predicate of the same name.

// scopedAuthAdminMutationRoute reports whether r targets an auth admin
// mutation route. Single home: auth package (unexported there).
func scopedAuthAdminMutationRoute(r *http.Request) bool {
	return auth.ScopedAuthAdminMutationRoute(r)
}

// scopedAuthAdminReadRoute reports whether r targets an auth admin read
// route. Single home: auth package (unexported there).
func scopedAuthAdminReadRoute(r *http.Request) bool {
	return auth.ScopedAuthAdminReadRoute(r)
}

// scopedAuthProfileReadRoute reports whether r targets the auth profile
// route. Single home: auth package (unexported there).
func scopedAuthProfileReadRoute(r *http.Request) bool {
	return auth.ScopedAuthProfileReadRoute(r)
}

// scopedCollectorExtractionReadinessRoute reports whether r targets a
// collector-extraction-readiness route. Single home: auth package
// (unexported there).
func scopedCollectorExtractionReadinessRoute(r *http.Request) bool {
	return auth.ScopedCollectorExtractionReadinessRoute(r)
}

// scopedImpactCompareRoute reports whether r targets an impact/compare
// route. Single home: auth package (unexported there).
func scopedImpactCompareRoute(r *http.Request) bool {
	return auth.ScopedImpactCompareRoute(r)
}

// scopedProviderConfigReadRoute reports whether path is a provider-config
// read route. Single home: auth package (unexported there).
func scopedProviderConfigReadRoute(path string) bool {
	return auth.ScopedProviderConfigReadRoute(path)
}

// scopedProviderConfigMutationRoute reports whether path is a
// provider-config mutation route. Single home: auth package (unexported
// there).
func scopedProviderConfigMutationRoute(path string) bool {
	return auth.ScopedProviderConfigMutationRoute(path)
}

// scopedServiceIntelligenceReportRoute reports whether path is the service
// intelligence report route. Single home: auth package (unexported there).
func scopedServiceIntelligenceReportRoute(path string) bool {
	return auth.ScopedServiceIntelligenceReportRoute(path)
}
