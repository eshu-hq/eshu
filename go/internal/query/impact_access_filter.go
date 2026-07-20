// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// impact_access_filter.go is the shared #5167 W3 row-filtering surface for the
// impact/* and compare/* handlers (ImpactHandler, CompareHandler). Every
// handler in this family resolves a start entity (workload, cloud resource,
// repository, ...) and then walks or joins outward, so a single anchor check
// is not enough: every returned row that names a repository must also be
// bound to the caller's grant before it leaves the handler. These helpers
// implement the #5137 pattern -- deny-by-default when scoped, empty grant
// short-circuits to zero rows -- for the row shapes this family returns.
//
// A resolved candidate or impact/path row whose repo_id is empty is dropped
// (not admitted) whenever the caller is scoped: several of the underlying
// graph reads intentionally match resources with no repository tie (for
// example a config-derived or "uncorrelated" CloudResource candidate), and
// those have no property this filter -- or the caller's grant -- can bind to,
// so the safe default is to withhold them from a scoped caller rather than
// guess. An all-scopes or shared-key caller is unaffected (impactRepoIDAllowed
// returns true unconditionally).

// impactRepoIDAllowed reports whether repoID is visible under access. An
// all-scopes/shared caller sees everything; a scoped caller only sees a
// repoID present in its grant, and an empty repoID is always denied when
// scoped (deny-by-default -- see file doc comment).
func impactRepoIDAllowed(repoID string, access repositoryAccessFilter) bool {
	if !access.scoped() {
		return true
	}
	if repoID == "" {
		return false
	}
	return access.allowsRepositoryID(repoID)
}

// filterChangeSurfaceCandidatesForAccess drops resolved change-surface target
// candidates (impact_change_surface_resolvers.go) whose RepoID is outside the
// caller's grant. Every changeSurfaceTargetCandidate resolver query
// (Workload, WorkloadInstance, Repository, CloudResource, TerraformModule,
// DataAsset) already projects repo_id (or, for Repository, its own id) into
// the candidate, so this filter needs no additional graph read.
func filterChangeSurfaceCandidatesForAccess(
	candidates []changeSurfaceTargetCandidate,
	access repositoryAccessFilter,
) []changeSurfaceTargetCandidate {
	if !access.scoped() {
		return candidates
	}
	filtered := make([]changeSurfaceTargetCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if impactRepoIDAllowed(candidate.RepoID, access) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

// filterResourceInvestigationCandidatesForAccess drops resolved resource
// investigation candidates (impact_resource_investigation.go) whose RepoID is
// outside the caller's grant.
func filterResourceInvestigationCandidatesForAccess(
	candidates []resourceInvestigationCandidate,
	access repositoryAccessFilter,
) []resourceInvestigationCandidate {
	if !access.scoped() {
		return candidates
	}
	filtered := make([]resourceInvestigationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if impactRepoIDAllowed(candidate.RepoID, access) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

// filterCodeTopicRowsForAccess drops codeTopicEvidenceRow entries whose
// RepoID is outside the caller's grant. investigateCodeTopic (the "code/*"
// #5167 family) has no grant filtering of its own, so change-surface callers
// that fold topic evidence into their response bind it here independently
// (see changeSurfaceCodeSurface).
func filterCodeTopicRowsForAccess(rows []codeTopicEvidenceRow, access repositoryAccessFilter) []codeTopicEvidenceRow {
	if !access.scoped() {
		return rows
	}
	filtered := make([]codeTopicEvidenceRow, 0, len(rows))
	for _, row := range rows {
		if impactRepoIDAllowed(row.RepoID, access) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// filterRowsByRepoIDForAccess drops rows carrying a "repo_id" string field
// outside the caller's grant. It is the generic form of
// filterChangeSurfaceCandidatesForAccess/filterResourceInvestigationCandidatesForAccess
// for the raw map[string]any impact/path rows several handlers in this family
// project directly from Cypher (change-surface impact rows, blast-radius
// affected repos, resource-investigation repository paths, deployment
// sources).
//
// It is deny-by-default when scoped: a row whose "repo_id" is empty (missing,
// or an entity the graph read matched with no repository tie) is DROPPED for a
// scoped caller, because there is no repository id to check against the grant
// and admitting an unbindable row would leak it (impactRepoIDAllowed returns
// false for an empty repo_id when scoped -- see its godoc and the file-level
// doc comment). A non-scoped (all-scopes/shared/admin/local) caller is
// unaffected and every row is returned unchanged.
func filterRowsByRepoIDForAccess(rows []map[string]any, access repositoryAccessFilter) []map[string]any {
	if !access.scoped() {
		return rows
	}
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if impactRepoIDAllowed(StringVal(row, "repo_id"), access) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// filterProvisioningRepositoryCandidatesForAccess drops provisioning/consuming
// repository candidates whose RepoID is outside the caller's grant (#5167 W3 P0,
// fifth vector). queryProvisioningRepositoryCandidates anchors on the service's
// own grant-verified repo and returns the FAR related repository with no grant
// predicate, so a scoped caller must not see a cross-tenant candidate. Every
// downstream field service_query_enrichment.go derives from these candidates
// (dependents, consumer_repositories, provisioning_source_chains) is bound once
// here, covering all three and every route that runs the enrichment
// (service/workload context and story, /investigations/services/{name}, and
// /impact/trace-deployment-chain). Deny-by-default when scoped (empty RepoID
// dropped, matching impactRepoIDAllowed and the rest of the W3 row filters); an
// all-scopes or shared-key caller is unaffected.
func filterProvisioningRepositoryCandidatesForAccess(
	candidates []provisioningRepositoryCandidate,
	access repositoryAccessFilter,
) []provisioningRepositoryCandidate {
	if !access.scoped() {
		return candidates
	}
	filtered := make([]provisioningRepositoryCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if impactRepoIDAllowed(candidate.RepoID, access) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}
