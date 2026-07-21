// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// Scoped-token authorization for the current, graph-hydrated IaC browse read
// (GET /api/v0/iac/resources).
//
// The current-inventory Postgres read applies repository and ingestion-scope
// grants before search, counts, facets, keyset pagination, or graph hydration.
// The graph therefore receives only the already-authorized candidate uids and
// never sees grant arrays.
//
// Fail-closed semantics:
//
//   - A fact whose `repo_id` and scope are not granted matches nothing, so
//     counts, facets, the limit+1 truncation flag, and the keyset cursor are all
//     computed over only granted current rows.
//   - An empty-grant scoped token (granted neither a repository nor an ingestion
//     scope) is short-circuited before any graph read and returns a bounded empty
//     page, so it never touches the authoritative graph.
//
// Shared, admin, and local callers use the same CTE with its authorization
// predicate disabled; every caller still carries the active-generation and
// tombstone predicates owned by the current inventory contract.

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
	if QueryParam(r, "include_facets") == "true" {
		body["summary"] = newIaCInventorySummary(iacInventoryFacetLimit)
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		profile,
		iacResourcesCapability,
		TruthBasisHybrid,
		"empty caller-authorized current IaC inventory; no graph hydration required",
	))
}
