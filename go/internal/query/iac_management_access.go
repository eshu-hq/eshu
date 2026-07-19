// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

// bindIaCManagementFilterAccess binds an IaCManagementFilter to the caller's
// exact AWS collector-scope grant (#5167 W4). Every handler on the shared
// IaCManagementStore.{List,Count}UnmanagedCloudResources choke point --
// handleUnmanagedCloudResources, readExactIaCManagementFilter (management
// status + explain), handleTerraformImportPlanCandidates,
// handleReplatformingPlan, handleReplatformingRollups, and
// handleReplatformingOwnershipPackets -- MUST call this immediately after
// normalizeIaCManagementRequest, before any store read: it is the one place
// Scoped/AllowedScopeIDs get set, and PostgresIaCManagementStore carries them
// through to postgres.AWSCloudRuntimeDriftFindingFilter, which intersects
// every row with the grant (or returns zero rows without querying, for an
// empty grant) exactly like handleReplatformingSelectors already does for
// GET /api/v0/replatforming/selectors. Split out of iac_management.go to
// keep that file under the repository's file-size cap.
//
// An all-scopes caller (no AuthContext, admin, or shared-key token) is
// unaffected: repositoryAccessFilterFromContext returns allScopes true, so
// filter.Scoped stays false and the request-supplied scope_id/account_id
// alone bounds the read exactly as before this field existed.
//
// A scoped caller's AllowedScopeIDs is never the raw grant: only entries
// shaped like a real AWS collector scope id (aws:<account>:<region>:<service>,
// replatformingAWSSelectorScopeIDPattern) survive
// replatformingAWSSelectorScopeIDs, so a repository-only or non-AWS ingestion
// scope grant can never accidentally satisfy the ANY(allowed_scope_ids) SQL
// predicate -- there is no authoritative repository-to-AWS-scope mapping on
// this path, matching replatformingSelectorScopedEmptyResponse's documented
// fail-closed behavior for the sibling selector route.
func bindIaCManagementFilterAccess(ctx context.Context, filter IaCManagementFilter) IaCManagementFilter {
	access := repositoryAccessFilterFromContext(ctx)
	filter.Scoped = access.scoped()
	if filter.Scoped {
		filter.AllowedScopeIDs = replatformingAWSSelectorScopeIDs(access.grantedScopeIDs())
	}
	return filter
}
