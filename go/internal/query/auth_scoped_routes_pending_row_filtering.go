// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // #6818 move 4b: root auth surface (aliases, forwarders, route policies, handler wiring) stays in package query; moving it into auth/ strands root handler callers and turns this rename into a root-surface relocation, which is a separate follow-up.

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
// `rg AllowedRepositoryIDs|AllowedScopeIDs|querycontract.RepositoryAccessFilterFromContext`
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
// An empty ledger is NOT the #5167 exit criterion, and stating it that way
// here misread the issue. Its Scope text allows a route that genuinely cannot
// be tenant-filtered yet to keep refusing scoped callers, on two conditions:
// the annotation here says why, and the route's MCP tool description tells a
// caller holding a scoped token the same thing. No silent 403s. So the
// terminal state is that every MCP-reachable route is either allowlisted with
// real row filtering or annotated here with the reason it is not and
// disclosed in its tool description -- which some entries below will reach by
// promotion and others by staying here with an honest reason.
//
// GET /api/v0/index-status and GET /api/v0/status/index left this ledger by
// promotion (scopedIndexStatusRoute, auth_scoped_routes_status.go). Neither
// grant-filtered the deployment-wide report, so the promotion follows the
// #5137 withhold shape rather than a "keep aggregates, redact ids" one: a
// scoped caller receives a grant-bound repository_count and a
// withheld_sections list, and the queue, coordinator, scope-activity, AWS,
// semantic and Terraform-state sections are never read for it
// (status_scoped.go).
//
// Reference implementation for the real fix: status_operations.go (#5137) --
// ReadLiveActivity(ctx, limit, allScopes=false, allowedRepositoryIDs,
// allowedScopeIDs) returns zero rows on an empty grant without querying and
// redacts source_key/source_display/lease_owner per row.
var pendingRowFilteringRoutes = map[string]struct{}{
	// #6475 service lineage ownership. The #5167 freshness workstream landed
	// this route's handler fence (serviceChangedSinceGrantAdmits), but
	// service_materialization_generations has no column naming the tenant a
	// lineage row belongs to, so the fence can only probe correlations live in
	// their own scope's active generation and an aged-out correlation stops
	// contesting the service_id. Promote it once #6475 gives the lineage rows
	// an ownership column the grant can bind; see scopedFreshnessDeltaRoute.
	"GET /api/v0/freshness/services/changed-since": {},
	// #5167 package-registry catalog read. handleSearchBundles
	// (code_registry_bundles.go) never reads the caller's grant, and the
	// Package nodes it answers from carry visibility and scope_id but no
	// repository key, so AllowedRepositoryIDs has nothing to bind to. The
	// promotion shape is the package-registry visibility gate that already
	// ships for the ecosystem browse route (packageRegistryPackagesGate
	// short-circuits an empty grant and
	// packageRegistryPackagesScopedEcosystemCypher forces visibility =
	// 'public' for a scoped caller, both in the registry family), plus a
	// scope_id IN $allowed_scope_ids disjunct if tenants ever get registry
	// scopes. Promoting it means disclosing that a package whose fact carries
	// no visibility stays hidden from a scoped caller.
	"POST /api/v0/code/bundles": {},
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
