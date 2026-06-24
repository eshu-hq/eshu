// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// ecosystemOverviewCounts lists the labels summarized by the ecosystem overview
// and the response field each populates. Each is counted with its own bounded
// single-label count query (see getEcosystemOverview for why).
var ecosystemOverviewCounts = []struct {
	field  string
	cypher string
}{
	{"repo_count", "MATCH (r:Repository) RETURN count(r) AS c"},
	{"workload_count", "MATCH (w:Workload) RETURN count(w) AS c"},
	{"platform_count", "MATCH (p:Platform) RETURN count(p) AS c"},
	{"instance_count", "MATCH (i:WorkloadInstance) RETURN count(i) AS c"},
}

// getEcosystemOverview returns high-level counts of graph entities.
// GET /api/v0/ecosystem/overview
//
// Each label is counted with its own single-label count query rather than a
// single chained `MATCH ... WITH count() MATCH ... WITH count()` statement.
// Chained cross-MATCH aggregation is not portable on the NornicDB backend: an
// empty intermediate label collapses the whole result (zeroing repo_count), and
// the chained form otherwise returns multiple all-null rows. A bare
// `MATCH (x:Label) RETURN count(x)` is supported on both backends and returns a
// single 0 row when the label is empty, so per-label counts are correct and the
// repository count never disappears because workloads/platforms are not yet
// materialized. Each query is one bounded label-count scan, the same scan work
// as the original single statement.
func (h *InfraHandler) getEcosystemOverview(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"ecosystem overview requires authoritative platform context truth",
			"unsupported_capability",
			"platform_impact.context_overview",
			h.profile(),
			requiredProfile("platform_impact.context_overview"),
		)
		return
	}

	counts := make(map[string]any, len(ecosystemOverviewCounts))
	for _, entry := range ecosystemOverviewCounts {
		row, err := h.Neo4j.RunSingle(r.Context(), entry.cypher, nil)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		counts[entry.field] = IntVal(row, "c")
	}

	WriteSuccess(w, r, http.StatusOK, counts, BuildTruthEnvelope(
		h.profile(),
		"platform_impact.context_overview",
		TruthBasisHybrid,
		"resolved from per-label ecosystem summary counters",
	))
}
