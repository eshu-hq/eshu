// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
)

// ecosystemOverviewCounts lists the labels summarized by the ecosystem overview
// and the response field each populates. Each is counted with its own bounded
// single-label count query (see getEcosystemOverview for why). repoAlias names
// the node each entry's scoped variant anchors its grant predicate on.
//
// durableProvenance selects which grant predicate runEcosystemOverviewCounts
// binds on repoAlias:
//
//   - false (repo_count, workload_count): repoAlias is a Repository (or a
//     Workload reached one DEFINES hop from a granted Repository), bound via
//     repositoryAccessFilter.graphWhereClause's flat id membership check.
//   - true (platform_count, instance_count): repoAlias is the WorkloadInstance
//     itself, bound via infraResourceScopePredicate so the grant check lands on
//     the instance's OWN durable repo_id rather than on repo->DEFINES->Workload
//     reachability. This distinction is load-bearing (#5167 F-6 W6 review, P1
//     "keep colliding workload instances tenant-scoped"): a Workload
//     name-collision (defined by two repositories, materializing only ONE
//     durable repo_id) is reachable from EITHER tenant's Repository via DEFINES,
//     so counting "every WorkloadInstance/Platform reachable through it" leaked
//     the other tenant's instances and platforms to any caller granted either
//     colliding repository. Binding the grant check directly on the instance's
//     own repo_id -- its durable per-instance provenance -- excludes the other
//     tenant's instances regardless of the shared Workload identity.
var ecosystemOverviewCounts = []struct {
	field             string
	cypher            string
	scopedCypher      string
	repoAlias         string
	durableProvenance bool
}{
	{
		field:        "repo_count",
		cypher:       "MATCH (r:Repository) RETURN count(r) AS c",
		scopedCypher: "MATCH (r:Repository) %s RETURN count(r) AS c",
		repoAlias:    "r",
	},
	{
		field:  "workload_count",
		cypher: "MATCH (w:Workload) RETURN count(w) AS c",
		scopedCypher: "MATCH (repo:Repository)-[:DEFINES]->(w:Workload) %s " +
			"RETURN count(DISTINCT w) AS c",
		repoAlias: "repo",
	},
	{
		field:  "platform_count",
		cypher: "MATCH (p:Platform) RETURN count(p) AS c",
		scopedCypher: "MATCH (i:WorkloadInstance)-[:RUNS_ON]->(p:Platform) %s " +
			"RETURN count(DISTINCT p) AS c",
		repoAlias:         "i",
		durableProvenance: true,
	},
	{
		field:             "instance_count",
		cypher:            "MATCH (i:WorkloadInstance) RETURN count(i) AS c",
		scopedCypher:      "MATCH (i:WorkloadInstance) %s RETURN count(DISTINCT i) AS c",
		repoAlias:         "i",
		durableProvenance: true,
	},
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
//
// Access scoping (#5167 Group B): these counts are otherwise a whole-corpus
// aggregate with no grant of its own. A scoped caller with no granted
// repository or ingestion scope gets all-zero counts without a graph read; a
// granted scoped caller's counts are restricted to entities reachable from its
// granted repositories (see runEcosystemOverviewCounts).
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

	access := repositoryAccessFilterFromContext(r.Context())
	counts, err := runEcosystemOverviewCounts(r.Context(), h.Neo4j, access)
	if err != nil {
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteSuccess(w, r, http.StatusOK, counts, ecosystemOverviewTruth(h.profile(), access))
}

// ecosystemOverviewTruth builds the truth envelope for the ecosystem overview
// and the ecosystem-wide graph-summary packet, downgrading to derived for a
// scoped caller (#5167 Group B) since the counts are then repo-grant-bound
// rather than the raw whole-corpus aggregate.
func ecosystemOverviewTruth(profile QueryProfile, access repositoryAccessFilter) *TruthEnvelope {
	if !access.scoped() {
		return BuildTruthEnvelope(
			profile,
			"platform_impact.context_overview",
			TruthBasisHybrid,
			"resolved from per-label ecosystem summary counters",
		)
	}
	truth := BuildTruthEnvelope(
		profile,
		"platform_impact.context_overview",
		TruthBasisHybrid,
		"resolved from per-label ecosystem summary counters restricted to the caller's granted repositories",
	)
	truth.Level = TruthLevelDerived
	return truth
}

// runEcosystemOverviewCounts runs each ecosystemOverviewCounts entry and
// returns the field->count map. A scoped caller with no granted repository or
// ingestion scope gets all-zero counts without a graph read (#5137
// LiveActivityStore precedent); a granted scoped caller's scopedCypher variant
// binds repositoryAccessFilter.graphWhereClause on each entry's repoAlias so
// counts are restricted to entities reachable from a granted Repository via
// DEFINES/INSTANCE_OF/RUNS_ON, matching the #5167 Group B accuracy bound.
func runEcosystemOverviewCounts(ctx context.Context, neo4j GraphQuery, access repositoryAccessFilter) (map[string]any, error) {
	counts := make(map[string]any, len(ecosystemOverviewCounts))
	if access.empty() {
		for _, entry := range ecosystemOverviewCounts {
			counts[entry.field] = 0
		}
		return counts, nil
	}
	for _, entry := range ecosystemOverviewCounts {
		cypher := entry.cypher
		var params map[string]any
		if access.scoped() {
			where := access.graphWhereClause(entry.repoAlias)
			if entry.durableProvenance {
				scalars, _ := access.scopeGrantInlineScalars()
				where = "WHERE " + infraResourceScopePredicate(entry.repoAlias, scalars)
			}
			cypher = fmt.Sprintf(entry.scopedCypher, where)
			params = access.graphParams(nil)
		}
		row, err := neo4j.RunSingle(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		counts[entry.field] = IntVal(row, "c")
	}
	return counts, nil
}
