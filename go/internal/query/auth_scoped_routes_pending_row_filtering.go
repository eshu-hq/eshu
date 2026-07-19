// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

// pendingRowFilteringRoutes is the #5167 Group B ledger: the closed,
// hand-maintained set of every HTTP API route that is MCP-reachable today
// but has no AllowedRepositoryIDs/AllowedScopeIDs grant filtering anywhere in
// its handler, so it cannot be safely allowlisted yet -- doing so as-is would
// turn a scoped/browser-session caller's current 403 into a cross-tenant
// read. This is the "known gap, tracked, not yet safe to allowlist" third
// state IsPendingRowFilteringRoute exists for, alongside the scoped-token
// allowlist (scopedHTTPRouteSupportsTenantFilter) and the shared-key-only
// ledger (sharedKeyOnlyRoutes): every route in this set was verified via
// `rg AllowedRepositoryIDs|AllowedScopeIDs|repositoryAccessFilterFromContext`
// returning zero hits in its handler as of #5167's W1 gate landing.
//
// This ledger is CLOSED, not a wildcard: TestEveryMCPReachableRouteIsScopedOrAnnotated
// (go/internal/mcp) fails the build for any MCP-reachable route that is
// neither wired, shared-key-only, nor listed here, so a brand new route added
// after this gate lands cannot silently reach this state -- a contributor
// must explicitly add it here (a reviewable, diffable action) or wire real
// filtering and allowlist it properly. As each #5167 family workstream
// (W2-W6) lands the #5137 row-filtering pattern for a route in this set, it
// MUST be removed from here and added to scopedTokenAdvertisedRoutes plus a
// scopedHTTPRouteSupportsTenantFilter matcher and an
// "x-scoped-token-support" marker, exactly like every Group A route in this
// same change. This ledger shrinking to empty is the #5167 exit criterion.
//
// Reference implementation for the real fix: status_operations.go (#5137) --
// ReadLiveActivity(ctx, limit, allScopes=false, allowedRepositoryIDs,
// allowedScopeIDs) returns zero rows on an empty grant without querying and
// redacts source_key/source_display/lease_owner per row.
var pendingRowFilteringRoutes = map[string]struct{}{
	"GET /api/v0/cloud/inventory":                     {},
	"GET /api/v0/ecosystem/overview":                  {},
	"GET /api/v0/freshness/changed-since":             {},
	"GET /api/v0/freshness/generations":               {},
	"GET /api/v0/freshness/services/changed-since":    {},
	"GET /api/v0/index-status":                        {},
	"GET /api/v0/investigations/drift/packet":         {},
	"GET /api/v0/kubernetes/correlations":             {},
	"GET /api/v0/observability/coverage/correlations": {},
	"GET /api/v0/package-registry/dependencies":       {},
	"GET /api/v0/package-registry/packages":           {},
	"GET /api/v0/package-registry/packages/count":     {},
	"GET /api/v0/package-registry/packages/inventory": {},
	"GET /api/v0/package-registry/versions":           {},
	"GET /api/v0/repositories/by-language":            {},
	"GET /api/v0/repositories/language-inventory":     {},
	"GET /api/v0/supply-chain/impact/explain":         {},
	"POST /api/v0/aws/runtime-drift/findings":         {},
	"POST /api/v0/cloud/runtime-drift/findings":       {},
	"POST /api/v0/code/bundles":                       {},
	"POST /api/v0/code/call-chain":                    {},
	"POST /api/v0/code/call-graph/metrics":            {},
	"POST /api/v0/code/complexity":                    {},
	"POST /api/v0/code/dead-code":                     {},
	"POST /api/v0/code/dead-code/cross-repo":          {},
	"POST /api/v0/code/dead-code/investigate":         {},
	"POST /api/v0/code/imports/investigate":           {},
	"POST /api/v0/code/language-query":                {},
	"POST /api/v0/code/quality/inspect":               {},
	"POST /api/v0/code/relationships":                 {},
	"POST /api/v0/code/relationships/story":           {},
	"POST /api/v0/code/security/secrets/investigate":  {},
	"POST /api/v0/code/structure/inventory":           {},
	"POST /api/v0/code/symbols/search":                {},
	"POST /api/v0/code/topics/investigate":            {},
	"POST /api/v0/compare/environments":               {},
	"POST /api/v0/ecosystem/graph-summary":            {},
	"POST /api/v0/impact/blast-radius":                {},
	"POST /api/v0/impact/change-surface":              {},
	"POST /api/v0/impact/change-surface/investigate":  {},
	"POST /api/v0/impact/contracts":                   {},
	"POST /api/v0/impact/deployment-config-influence": {},
	"POST /api/v0/impact/developer-change-plan":       {},
	"POST /api/v0/impact/explain-dependency-path":     {},
	"POST /api/v0/impact/pre-change":                  {},
	"POST /api/v0/impact/resource-investigation":      {},
	"POST /api/v0/impact/trace-deployment-chain":      {},
	"POST /api/v0/impact/trace-exposure-path":         {},
	"POST /api/v0/impact/trace-resource-to-code":      {},
	"POST /api/v0/relationships/edges":                {},
}

// pendingEvidenceRelationshipRoutePrefix anchors
// pendingRowFilteringEvidenceRelationshipRoute: GET
// /api/v0/evidence/relationships/{id} (get_relationship_evidence) carries a
// path parameter, so it cannot sit in the literal pendingRowFilteringRoutes
// map like its path-param-free Group B siblings.
const pendingEvidenceRelationshipRoutePrefix = "/api/v0/evidence/relationships/"

// pendingRowFilteringEvidenceRelationshipRoute matches GET
// /api/v0/evidence/relationships/{id}, the one Group B route with a path
// parameter (see pendingRowFilteringRoutes's doc comment for the ledger this
// belongs to).
func pendingRowFilteringEvidenceRelationshipRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if !strings.HasPrefix(r.URL.Path, pendingEvidenceRelationshipRoutePrefix) {
		return false
	}
	id := strings.TrimPrefix(r.URL.Path, pendingEvidenceRelationshipRoutePrefix)
	return id != "" && !strings.Contains(id, "/")
}

// IsPendingRowFilteringRoute reports whether r targets a #5167 Group B route:
// MCP-reachable, known to lack tenant-grant filtering, and tracked in the
// closed pendingRowFilteringRoutes ledger (or matched by
// pendingRowFilteringEvidenceRelationshipRoute for the one parameterized
// entry) as a family-workstream follow-up rather than a silent gap. It is
// exported for the same cross-package reason as ScopedHTTPRouteSupportsTenantFilter
// and IsSharedKeyOnlyRoute: the MCP-reachability exhaustiveness gate lives in
// go/internal/mcp, which imports this package, not the reverse.
func IsPendingRowFilteringRoute(r *http.Request) bool {
	if pendingRowFilteringEvidenceRelationshipRoute(r) {
		return true
	}
	_, ok := pendingRowFilteringRoutes[r.Method+" "+r.URL.Path]
	return ok
}
