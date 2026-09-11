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
// Reference implementation for the real fix: status_operations.go (#5137) --
// ReadLiveActivity(ctx, limit, allScopes=false, allowedRepositoryIDs,
// allowedScopeIDs) returns zero rows on an empty grant without querying and
// redacts source_key/source_display/lease_owner per row.
var pendingRowFilteringRoutes = map[string]struct{}{
	// #5167 deployment-wide status report. getIndexStatus (status.go) reads no
	// caller grant: repository_count is an unfiltered MATCH (r:Repository)
	// RETURN count(r), and the queue, coordinator, scope-activity and AWS
	// materialization sections are stack-wide aggregates with nothing to
	// intersect a grant with. A queue_blockages row reports conflict_key as
	// COALESCE(conflict_key, scope_id), so a raw scope id can appear in the
	// payload. Promotion waits on a settled scoped payload shape (redact the
	// identifiers the way scopedCoordinatorToMap does, or drop the aggregates
	// and grant-filter the repository count); the tool description and the
	// OpenAPI operation disclose the 403 meanwhile. GET /api/v0/status/index
	// shares the handler and is not MCP-reachable.
	"GET /api/v0/index-status": {},
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
	// #5167 compatibility fallback for the analyze_code_relationships MCP
	// tool. That tool sends its relationship-story and call-chain query types
	// to the two already-allowlisted routes; who_modifies, module_deps,
	// variable_scope, find_complexity, find_functions_by_argument and
	// find_functions_by_decorator fall through
	// resolveAnalyzeCodeRelationshipsRequest
	// (go/internal/mcp/relationships/code_routes.go) to this route instead,
	// whose handler expands a resolved entity's neighbours with no grant to
	// intersect. The promotion shape is the #6553 one -- the grant in each
	// statement's own anchoring MATCH, on the entity node's repo_id, as
	// scopedCodeGraphGrantRoute describes for /code/relationships/story --
	// and it waits on the #6060 lane A move of code_relationships.go out of
	// this package so the rewrite is not written twice.
	"POST /api/v0/code/relationships": {},
	// #5167 W3 flagged (NOT allowlisted, still pending). All three walks are
	// bounded today, contrary to what this comment used to claim:
	// traceResourceToCode (impact.go) clamps max_depth to 1..20 (default 8)
	// and caps rows through normalizeImpactListLimit (impact_bounds.go,
	// default 50, max 200); explainDependencyPath (impact.go) is one
	// shortestPath of at most 8 hops; trace-exposure-path clamps depth through
	// clampExposureDepth (exposure_path.go, default 5, max 10) and returns at
	// most exposurePathResultLimit (25) paths. What keeps them pending is the
	// grant, not the bound. Each route resolves an arbitrary anchor across
	// many labels (impactAnchorLabelDisjunction) and walks through
	// infrastructure hops that carry no repo_id property --
	// cloud_resource_node_writer.go sets none on a CloudResource node -- so
	// the anchoring-MATCH grant shape #6553 landed for
	// /code/relationships/story and /code/call-chain has nothing to bind to
	// here. Promotion needs a Go-side per-node ownership check and a product
	// decision about a node that cannot be bound at all, and the #6060 lane B
	// move of impact.go, impact_anchor_resolve.go and exposure_path.go out of
	// this package is in flight over the same files. See
	// auth_scoped_routes_impact.go's doc comment.
	"POST /api/v0/impact/explain-dependency-path": {},
	"POST /api/v0/impact/trace-exposure-path":     {},
	"POST /api/v0/impact/trace-resource-to-code":  {},
	// #5459 tag/digest mutation-history read. The ContainerImageTagObservation
	// nodes it returns are keyed by the OCI registry repository_id
	// (oci-registry://...), not a code repository_id, and carry no edge to the
	// source code repo that the grant model (AllowedRepositoryIDs /
	// repositoryAccessFilterFromContext) filters on. This used to point at
	// #5457 for that linkage, which is stale: #5457 closed on 2026-07-23, and
	// the PUBLISHES/BUILT_FROM edges it built do not by themselves hand this
	// route a grant selector, because BUILT_FROM hangs off ContainerImage
	// rather than the observation node and only some images carry one. #6564
	// is the open design issue that replaces the pointer: decide whether
	// joining the observation's resolved_digest to ContainerImage.digest and
	// following BUILT_FROM covers enough images to bind a grant honestly, or
	// record the route as shared-key-only instead. Until that is settled the
	// route fails closed (scoped and browser-session callers 403, disclosed in
	// list_container_image_tag_history's tool description) and is tracked here
	// rather than allowlisted.
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
