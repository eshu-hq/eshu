// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// Scoped-token authorization for the graph-backed IaC resource browse read
// (GET /api/v0/iac/resources).
//
// The route scans one canonical Terraform/IaC label per call
// (TerraformResource, TerraformModule, TerraformDataSource). Every node in
// those labels carries a durable `repo_id` property written by the canonical
// entity projector, which is the same durable repository anchor that the infra
// resource aggregate / search / relationship routes bind against (disjunct 1 of
// infraResourceScopePredicate). Scoped tokens therefore bound the list to nodes
// whose `repo_id` is in the granted repository or ingestion-scope set.
//
// Fail-closed semantics:
//
//   - A node whose `repo_id` is not granted (or empty) matches nothing and stays
//     invisible to the scoped token, so counts, the limit+1 truncation flag, and
//     the keyset cursor are all computed over only granted rows.
//   - An empty-grant scoped token (granted neither a repository nor an ingestion
//     scope) is short-circuited before any graph read and returns a bounded empty
//     page, so it never touches the authoritative graph.
//
// No-observability-change / no-regression: the clause and parameters render only
// in scoped mode, so the Cypher and bound parameters for shared / admin / local
// callers are byte-identical to the pre-scoped query.

// iacResourceScopeClause returns the repository-anchored predicate appended to
// the IaC resource list WHERE chain for scoped tokens, or the empty string for
// shared / admin / local callers (no-regression: the unscoped Cypher is
// unchanged). The list node alias is `n`.
//
// Unlike infraResourceScopePredicate, this clause omits the CloudResource USES
// EXISTS disjunct: the three scanned Terraform labels always carry a durable
// `repo_id`, so the direct property compare is the complete durable join and the
// hot list read avoids an unnecessary subquery traversal.
func iacResourceScopeClause(access repositoryAccessFilter) string {
	if !access.scoped() {
		return ""
	}
	return "(n.repo_id IN $allowed_repository_ids OR n.repo_id IN $allowed_scope_ids)"
}

// iacResourceScopeParams binds the granted repository and ingestion-scope id
// arrays referenced by iacResourceScopeClause. Both arrays are bound
// unconditionally in scoped mode so the parameters always resolve even when one
// side is empty (for example a token granted only ingestion scopes). For
// shared / admin / local callers it leaves params untouched so the unscoped
// parameter set is unchanged.
func iacResourceScopeParams(access repositoryAccessFilter, params map[string]any) {
	if !access.scoped() {
		return
	}
	params["allowed_repository_ids"] = access.grantedRepositoryIDs()
	params["allowed_scope_ids"] = access.grantedScopeIDs()
}

// writeIaCResourceEmptyPage writes the bounded empty IaC resource list page for
// an empty-grant scoped token. It mirrors the success-shape of listResources
// (kind, empty resources, zero count, the requested limit, not truncated, no
// next_cursor) so an empty grant is indistinguishable from a normal empty
// result and discloses nothing, while skipping the authoritative graph read.
func writeIaCResourceEmptyPage(
	w http.ResponseWriter,
	r *http.Request,
	profile QueryProfile,
	kind iacResourceKind,
	limit int,
) {
	body := map[string]any{
		"kind":      string(kind),
		"resources": []iacResourceRow{},
		"count":     0,
		"limit":     limit,
		"truncated": false,
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		profile,
		iacResourcesCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from the authoritative Terraform/IaC graph projection; bounded list ordered by name then id",
	))
}
