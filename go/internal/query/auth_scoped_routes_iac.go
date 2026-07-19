// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// This file holds the scoped-token route predicates for the #5167 W4
// iac/* and replatforming/* POST-body-filtered family: find_dead_iac,
// find_unmanaged_resources, get_iac_management_status,
// explain_iac_management_status, propose_terraform_import_plan,
// compose_replatforming_plan, get_replatforming_rollups, and
// find_unmanaged_resource_owners. They are split out of
// auth_scoped_routes.go to keep that file under the repository's file-size
// cap. Each predicate reports whether a request targets a route whose
// handler is proven to intersect its reads with the scoped-token grant set
// (see each function's contract); routes not listed here stay deny-by-default
// for scoped tokens.
//
// scopedIaCManagementRoute and scopedReplatformingPlanFamilyRoute each cover
// several routes: every one of those handlers calls
// normalizeIaCManagementRequest followed immediately by
// bindIaCManagementFilterAccess (iac_management.go) before its one shared
// store choke point, IaCManagementStore.{List,Count}UnmanagedCloudResources,
// so they share one grant-filtering proof.

// scopedIaCDeadRoute reports whether the request targets the dead-IaC
// candidate finder. handleDeadIaC (iac.go) resolves every repo_id/repo_ids
// selector through resolveRepositorySelectorExactForAccess bound to
// repositoryAccessFilterFromContext (the same access-filtered chain the
// #5167 Group A single-repository routes use), so a selector naming a
// repository outside a scoped caller's grant fails closed with a 400 before
// either the reducer-materialized IaCReachabilityStore read or the
// content-derived fallback analysis runs.
func scopedIaCDeadRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v0/iac/dead"
}

// scopedIaCManagementRoute reports whether the request targets one of the
// four AWS IaC management read routes bound to the caller's exact AWS
// collector-scope grant via bindIaCManagementFilterAccess: unmanaged-resource
// discovery, exact management status, its evidence explanation, and
// Terraform import-plan candidate generation. Each intersects
// PostgresIaCManagementStore's reads with AllowedScopeIDs
// (postgres.AWSCloudRuntimeDriftFindingFilter's `fact.scope_id = ANY(...)`
// predicate) and returns zero rows without querying Postgres when the
// caller's grant contains no AWS collector scope, exactly like
// handleReplatformingSelectors already does for the sibling
// GET /api/v0/replatforming/selectors route.
func scopedIaCManagementRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/iac/unmanaged-resources",
		"/api/v0/iac/management-status",
		"/api/v0/iac/management-status/explain",
		"/api/v0/iac/terraform-import-plan/candidates":
		return true
	default:
		return false
	}
}

// scopedReplatformingPlanFamilyRoute reports whether the request targets one
// of the three replatforming composition routes bound to the caller's exact
// AWS collector-scope grant via the same bindIaCManagementFilterAccess ->
// PostgresIaCManagementStore -> postgres.AWSCloudRuntimeDriftFindingFilter
// chain scopedIaCManagementRoute's routes use: the service-scoped migration
// plan, the account/environment/service readiness rollups, and the
// unmanaged-resource ownership packets. None of the three mutate cloud or
// repository state; all three only compose read-only, provider-neutral
// summaries over the same grant-filtered AWS drift findings.
func scopedReplatformingPlanFamilyRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case replatformingPlanRoute,
		"/api/v0/replatforming/rollups",
		"/api/v0/replatforming/ownership-packets":
		return true
	default:
		return false
	}
}
