// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
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
// (W2-W6) lands the #5137 row-filtering pattern for a route in this set, the
// route MUST make all five of these moves in the same change, exactly like
// every Group A route in this one:
//
//  1. a scopedHTTPRouteSupportsTenantFilter matcher that matches it;
//  2. a scopedTokenAdvertisedRoutes entry carrying its scopedRouteClass;
//  3. an "x-scoped-token-support" marker on its OpenAPI operation;
//  4. a "403": {"$ref": "#/components/responses/Forbidden"} response on that
//     same operation. TestPolicyGatedRoutesDeclareForbiddenResponse (#6497,
//     auth_scoped_routes_forbidden_response_test.go) requires one of every
//     advertised route whose class answers false to
//     scopedRouteClass.admitsAllScopesSessionWithoutPolicy -- which is every
//     grant-bound promotion out of this ledger -- so doing steps 1-3 without
//     this one reds the build;
//  5. removal from this map.
//
// This ledger shrinking to empty is the #5167 exit criterion.
//
// Reference implementation for the real fix: status_operations.go (#5137) --
// ReadLiveActivity(ctx, limit, allScopes=false, allowedRepositoryIDs,
// allowedScopeIDs) returns zero rows on an empty grant without querying and
// redacts source_key/source_display/lease_owner per row.
var pendingRowFilteringRoutes = map[string]struct{}{
	"GET /api/v0/index-status": {},
	// #6475 service lineage ownership. The #5167 freshness workstream landed
	// this route's handler fence (serviceChangedSinceGrantAdmits), but
	// service_materialization_generations has no column naming the tenant a
	// lineage row belongs to, so the fence can only probe correlations live in
	// their own scope's active generation and an aged-out correlation stops
	// contesting the service_id. Promote it once #6475 gives the lineage rows
	// an ownership column the grant can bind; see scopedFreshnessDeltaRoute.
	"GET /api/v0/freshness/services/changed-since": {},
	"POST /api/v0/code/bundles":                    {},
	"POST /api/v0/code/call-chain":                 {},
	"POST /api/v0/code/imports/investigate":        {},
	"POST /api/v0/code/relationships":              {},
	"POST /api/v0/code/relationships/story":        {},
	// #5167 W3 flagged (NOT allowlisted, still pending): each of the three
	// routes below resolves an arbitrary graph node across many labels
	// (impactAnchorLabelDisjunction) or an unbounded cross-repo CALLS chain,
	// and most of those reachable node types carry no repo_id property at all
	// (repo ownership requires a CONTAINS->REPO_CONTAINS traversal per node),
	// so binding every traversal endpoint to a grant needs a live-graph schema
	// check and a NornicDB-safe Cypher rewrite before it is safe to allowlist.
	// See auth_scoped_routes_impact.go's doc comment.
	"POST /api/v0/impact/explain-dependency-path": {},
	"POST /api/v0/impact/trace-exposure-path":     {},
	"POST /api/v0/impact/trace-resource-to-code":  {},
	// #5459 tag/digest mutation-history read. The ContainerImageTagObservation
	// nodes it returns are keyed by the OCI registry repository_id
	// (oci-registry://...), not a code repository_id, and carry no edge to the
	// source code repo that the grant model (AllowedRepositoryIDs /
	// repositoryAccessFilterFromContext) filters on. That OCI-to-source-repo
	// linkage is exactly the PUBLISHES/BUILT_FROM provenance epic child #5457
	// builds; until it lands there is no bindable grant selector, so this route
	// fails closed (scoped/browser-session callers 403) and is tracked here
	// rather than allowlisted. Move it to scopedTokenAdvertisedRoutes with a
	// real filter once #5457 provides the source-repo edge.
	"GET /api/v0/images/tag-history": {},
}

// IsPendingRowFilteringRoute reports whether r targets a #5167 Group B route:
// MCP-reachable, known to lack tenant-grant filtering, and tracked in the
// closed pendingRowFilteringRoutes ledger as a family-workstream follow-up
// rather than a silent gap. It is exported for the same cross-package reason
// as ScopedHTTPRouteSupportsTenantFilter and IsSharedKeyOnlyRoute: the
// MCP-reachability exhaustiveness gate lives in go/internal/mcp, which
// imports this package, not the reverse.
//
// The one parameterized Group B entry, GET /api/v0/evidence/relationships/{id},
// was cleared by the #5167 F-6 W6 cloud/aws family workstream
// (scopedRelationshipEvidenceRoute, auth_scoped_routes_cloud.go) and no
// longer needs the path-parameter matcher this function used to delegate to.
func IsPendingRowFilteringRoute(r *http.Request) bool {
	_, ok := pendingRowFilteringRoutes[r.Method+" "+r.URL.Path]
	return ok
}
