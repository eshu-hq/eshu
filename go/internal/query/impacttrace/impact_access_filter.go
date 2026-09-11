// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

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
// guess. An all-scopes or shared-key caller is unaffected (ImpactRepoIDAllowed
// returns true unconditionally).

// ImpactRepoIDAllowed reports whether repoID is visible under access. An
// all-scopes/shared caller sees everything; a scoped caller only sees a
// repoID present in its grant, and an empty repoID is always denied when
// scoped (deny-by-default -- see file doc comment).
func ImpactRepoIDAllowed(repoID string, access querycontract.RepositoryAccessFilter) bool {
	if !access.Scoped() {
		return true
	}
	if repoID == "" {
		return false
	}
	return access.AllowsRepositoryID(repoID)
}

// FilterRowsByRepoIDForAccess drops rows carrying a "repo_id" string field
// outside the caller's grant. It is the generic form of the candidate filters
// (see impact/impact_candidate_access_filter.go) for the raw map[string]any
// impact/path rows several handlers in this family project directly from
// Cypher (change-surface impact rows, blast-radius affected repos,
// resource-investigation repository paths, deployment sources).
//
// It is deny-by-default when scoped: a row whose "repo_id" is empty (missing,
// or an entity the graph read matched with no repository tie) is DROPPED for a
// scoped caller, because there is no repository id to check against the grant
// and admitting an unbindable row would leak it (ImpactRepoIDAllowed returns
// false for an empty repo_id when scoped -- see its godoc and the file-level
// doc comment). A non-scoped (all-scopes/shared/admin/local) caller is
// unaffected and every row is returned unchanged.
//
// Exported because the impact package and the query root call it from outside
// this package. See #6060.
func FilterRowsByRepoIDForAccess(rows []map[string]any, access querycontract.RepositoryAccessFilter) []map[string]any {
	return filterRowsByRepoIDForAccess(rows, access)
}

func filterRowsByRepoIDForAccess(rows []map[string]any, access querycontract.RepositoryAccessFilter) []map[string]any {
	if !access.Scoped() {
		return rows
	}
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if ImpactRepoIDAllowed(querycontract.StringVal(row, "repo_id"), access) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// ProvisioningRepositoryCandidate names one repository related to a service
// through a provisioning or consuming edge. It moved here from the query root
// (deployment_trace_support_helpers.go) with lane B2 of #6060: the grant
// filter below runs in this package, and an impacttrace signature cannot name
// a root-defined type (package query imports this package, so the reverse
// import would be a cycle). The root producer and its readers keep working
// unchanged through the query-package alias. See #6060.
type ProvisioningRepositoryCandidate struct {
	RepoID              string
	RepoName            string
	RelationshipTypes   []string
	RelationshipReasons []string
}

// FilterProvisioningRepositoryCandidatesForAccess drops provisioning/consuming
// repository candidates whose RepoID is outside the caller's grant (#5167 W3 P0,
// fifth vector). queryProvisioningRepositoryCandidates anchors on the service's
// own grant-verified repo and returns the FAR related repository with no grant
// predicate, so a scoped caller must not see a cross-tenant candidate. Every
// downstream field query_enrichment.go derives from these candidates
// (dependents, consumer_repositories, provisioning_source_chains) is bound once
// here, covering all three and every route that runs the enrichment
// (service/workload context and story, /investigations/services/{name}, and
// /impact/trace-deployment-chain). Deny-by-default when scoped (empty RepoID
// dropped, matching ImpactRepoIDAllowed and the rest of the W3 row filters); an
// all-scopes or shared-key caller is unaffected.
//
// Exported because the query root and the impact package call it from outside
// this package. See #6060.
func FilterProvisioningRepositoryCandidatesForAccess(
	candidates []ProvisioningRepositoryCandidate,
	access querycontract.RepositoryAccessFilter,
) []ProvisioningRepositoryCandidate {
	if !access.Scoped() {
		return candidates
	}
	filtered := make([]ProvisioningRepositoryCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if ImpactRepoIDAllowed(candidate.RepoID, access) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}
