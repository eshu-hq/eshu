// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"sort"
)

// Blast-radius queries are written to the NornicDB-safe single-clause /
// CALL{UNION}-plain-outer contract. The pinned NornicDB build routes any
// multi-clause query (a preceding MATCH/WITH/OPTIONAL MATCH before the RETURN)
// into a string-slicing interpreter that silently returns raw expression text
// as a column value (e.g. "DISTINCT a.name", "length(path)"), returns 0/false,
// or drops all rows. So each affected-set query is a single anchored MATCH with
// a plain-property or aggregate RETURN, the dependent traversal is a TYPED
// variable-length relationship (untyped `[*1..N] WHERE all(type(rel)=...)`
// matches nothing on this build), hop distance is `min(length(path))` computed
// in the same single clause, and the tier join is a SEPARATE single-clause
// query merged in Go. These shapes are also strictly more correct on Neo4j:
// `RETURN DISTINCT repo, hops` double-counts a diamond-reachable repo and
// inflates LIMIT, and the previous crossplane branch left `affected` unbound.
// See docs/public/reference/nornicdb-pitfalls.md and #5279.

// blastRadiusRepositoryCypher returns repos that transitively depend on the
// target repository, with the shortest hop distance to it. Typed DEPENDS_ON
// traversal; single clause so min(length(path)) and the implicit group-by on
// repo both evaluate correctly.
const blastRadiusRepositoryCypher = `MATCH path=(s:Repository)<-[:DEPENDS_ON*1..5]-(a:Repository)
WHERE s.name CONTAINS $target_name
RETURN a.name AS repo, a.id AS repo_id, min(length(path)) AS hops
ORDER BY hops, repo
LIMIT $limit`

// blastRadiusTerraformSourceReposCypher resolves the repositories that DEFINE
// the matched Terraform module (module -> file -> repo). Plain multi-clause
// value projection (no DISTINCT, no aggregate, no OPTIONAL MATCH) is safe on
// this build; duplicates are removed in Go. These source repos are the hop-0
// blast surface; their dependents are resolved separately.
const blastRadiusTerraformSourceReposCypher = `MATCH (mod:TerraformModule)
WHERE mod.name CONTAINS $target_name OR mod.source CONTAINS $target_name
MATCH (f:File)-[:CONTAINS]->(mod)
MATCH (repo:Repository)-[:REPO_CONTAINS]->(f)
RETURN repo.name AS repo, repo.id AS repo_id
LIMIT $limit`

// blastRadiusDependentsByIDCypher returns repos that transitively depend on any
// of the source repositories identified by concrete `id`, with shortest hop
// distance. Used to extend the terraform_module blast surface past the defining
// repos. Anchored on `s.id IN $repo_ids` (not name) so a source repo that shares
// its name with an unrelated indexed repo does not pull that repo's dependents
// into the blast radius. Typed traversal, single clause; `*1..5` (never `*0..5`,
// which projects literal text for the zero-length row on this build).
const blastRadiusDependentsByIDCypher = `MATCH path=(s:Repository)<-[:DEPENDS_ON*1..5]-(a:Repository)
WHERE s.id IN $repo_ids
RETURN a.name AS repo, a.id AS repo_id, min(length(path)) AS hops
ORDER BY hops, repo
LIMIT $limit`

// blastRadiusCrossplaneCypher resolves repositories whose claims are satisfied
// by the matched CrossplaneXRD (xrd <- claim <- file <- repo). The repo is
// bound through the REPO_CONTAINS chain (the previous shape left `affected`
// unbound, cartesian-joining every Tier on Neo4j and leaking literal text on
// NornicDB). A repo holding several matching claims is collapsed to ONE row via
// `min(claim.name)` (a representative claim) so `LIMIT` bounds the unique-repo
// count rather than the (repo, claim) pair count — the same dedup-before-LIMIT
// concern the sql_table branch handles with over-fetch, closed here in-query
// because crossplane has no `CALL {}` blocking the aggregation.
const blastRadiusCrossplaneCypher = `MATCH (xrd:CrossplaneXRD)
WHERE xrd.kind CONTAINS $target_name OR xrd.name CONTAINS $target_name
MATCH (claim:CrossplaneClaim)-[:SATISFIED_BY]->(xrd)
MATCH (f:File)-[:CONTAINS]->(claim)
MATCH (repo:Repository)-[:REPO_CONTAINS]->(f)
RETURN repo.name AS repo, repo.id AS repo_id, min(claim.name) AS claim
ORDER BY repo
LIMIT $limit`

// blastRadiusSqlTableBranches is the number of UNION branches in
// blastRadiusSqlTableCypher; a single repo can appear once per branch, so the
// caller over-fetches by this factor before the app-side min-hop dedup and
// trim, ensuring the requested unique-repo limit is met. MUST track the exact
// branch count below (guarded by TestBlastRadiusSqlTableCypherDropsDeadBranchesKeepsLiveOnes).
const blastRadiusSqlTableBranches = 5

// blastRadiusSqlTableCypher resolves repositories touching the matched SqlTable
// through any of the code/schema relationship kinds that have a real graph
// writer, with a coarse hop marker. The CALL{...UNION...} core with a PLAIN
// outer RETURN is the one multi-branch shape this build executes correctly;
// the previous version appended a trailing `OPTIONAL MATCH ... RETURN
// DISTINCT` after the CALL, which hard-errors ("unsupported clause after
// CALL {}"). Tier is joined separately; per-repo min hop is taken in Go
// across the UNION branches.
//
// #5330: this UNION only claims edge types the graph actually writes.
// CONTAINS/QUERIES_TABLE/REFERENCES_TABLE always had writers. TRIGGERS
// replaces the never-written TRIGGERS_ON name (the reducer only ever emits
// TRIGGERS — see reducer/sql_relationship_materialization.go) with an
// explicit (:SqlTrigger) endpoint-label constraint so a same-named unrelated
// label cannot inflate the count. INDEXES is newly wired
// (SqlIndex.table_name metadata -> reducer -> edge writer, #5330 Task 3),
// also endpoint-label constrained. READS_FROM, MIGRATES, and MAPS_TO_TABLE
// have no writer at all (confirmed by auditing every reducer/edge-writer
// path) and are intentionally NOT UNIONed here — reporting them as a silent
// zero would be a correctness bug. blastRadiusAffected reports their absence
// honestly via the coverage/complete response fields instead
// (sqlTableBlastRadiusEdgeTypes, sqlTableBlastRadiusCoverage).
const blastRadiusSqlTableCypher = `CALL {
	MATCH (table:SqlTable) WHERE table.name CONTAINS $target_name
	MATCH (repo:Repository)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(table) RETURN repo, 0 AS hops UNION
	MATCH (table:SqlTable) WHERE table.name CONTAINS $target_name
	MATCH (repo:Repository)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(:Function)-[:QUERIES_TABLE]->(table) RETURN repo, 1 AS hops UNION
	MATCH (table:SqlTable) WHERE table.name CONTAINS $target_name
	MATCH (repo:Repository)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(:SqlTable)-[:REFERENCES_TABLE]->(table) RETURN repo, 1 AS hops UNION
	MATCH (table:SqlTable) WHERE table.name CONTAINS $target_name
	MATCH (repo:Repository)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(:SqlTrigger)-[:TRIGGERS]->(table) RETURN repo, 1 AS hops UNION
	MATCH (table:SqlTable) WHERE table.name CONTAINS $target_name
	MATCH (repo:Repository)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(:SqlIndex)-[:INDEXES]->(table) RETURN repo, 1 AS hops
}
RETURN repo.name AS repo, repo.id AS repo_id, hops
ORDER BY hops, repo
LIMIT $limit`

// sqlTableBlastRadiusEdgeTypes lists the graph relationship types the
// sql_table blast-radius surface conceptually covers, independent of which
// ones currently have a UNION branch in blastRadiusSqlTableCypher. Each is
// checked against the materialized-edge registry (EdgeMaterializationCoverage,
// #5330 Task 1) to build the response's honest coverage/complete fields:
// CONTAINS, QUERIES_TABLE, REFERENCES_TABLE, TRIGGERS, and INDEXES are the
// live UNION branches above; READS_FROM, MIGRATES, and MAPS_TO_TABLE have no
// writer and are reported as unmaterialized rather than silently
// contributing a zero to affected_count.
var sqlTableBlastRadiusEdgeTypes = []string{
	"CONTAINS", "QUERIES_TABLE", "REFERENCES_TABLE", "TRIGGERS", "INDEXES",
	"READS_FROM", "MIGRATES", "MAPS_TO_TABLE",
}

// blastRadiusEdgeCoverage reports one graph relationship type's
// materialization status in the blast-radius response's "coverage" array
// (#5330, extended for crossplane_xrd in #5331). A target_type with no gaps
// registered against it (repository, terraform_module — not audited by
// either task) reports an empty coverage array and complete:true rather than
// a false claim of full coverage or an unaudited gap.
type blastRadiusEdgeCoverage struct {
	EdgeType     string `json:"edge_type"`
	Materialized bool   `json:"materialized"`
	Reason       string `json:"reason"`
}

// sqlTableBlastRadiusCoverage evaluates sqlTableBlastRadiusEdgeTypes against
// the materialized-edge registry and reports whether every branch the
// sql_table surface conceptually covers actually has a writer (complete),
// alongside the per-edge-type coverage detail.
func sqlTableBlastRadiusCoverage() (bool, []blastRadiusEdgeCoverage) {
	complete := true
	coverage := make([]blastRadiusEdgeCoverage, 0, len(sqlTableBlastRadiusEdgeTypes))
	for _, edgeType := range sqlTableBlastRadiusEdgeTypes {
		c := EdgeMaterializationCoverage(edgeType)
		if !c.Materialized {
			complete = false
		}
		coverage = append(coverage, blastRadiusEdgeCoverage(c))
	}
	return complete, coverage
}

// crossplaneXrdBlastRadiusEdgeTypes lists the graph relationship types the
// crossplane_xrd blast-radius surface conceptually covers, mirroring
// sqlTableBlastRadiusEdgeTypes (#5330). CONTAINS is the generic File->entity
// containment edge blastRadiusCrossplaneCypher traverses (file -> claim) and
// has a real writer (cypher.buildEntityStatementsWithContainment). SATISFIED_BY
// (claim -> xrd) has none: no emitter anywhere in the codebase MERGEs it — the
// internal/graph/edgetype.SatisfiedBy constant is orphaned, only ever read by
// blastRadiusCrossplaneCypher (#5331; wiring a writer is tracked in #5347).
// REPO_CONTAINS (repo -> file), also traversed by the query, is deliberately
// excluded here, matching sqlTableBlastRadiusEdgeTypes: it is a universal
// structural edge outside the materialized-edge registry's scope (see
// edge_materialization_coverage.go), so listing it would register a false
// "no_writer" gap instead of a real one.
var crossplaneXrdBlastRadiusEdgeTypes = []string{"CONTAINS", "SATISFIED_BY"}

// crossplaneXrdBlastRadiusCoverage evaluates crossplaneXrdBlastRadiusEdgeTypes
// against the materialized-edge registry, mirroring sqlTableBlastRadiusCoverage
// (#5330). SATISFIED_BY has no writer, so complete is always false today and
// coverage always reports it as materialized:false/reason:"no_writer" — this
// flips to true automatically once a SATISFIED_BY writer lands and registers
// itself in the materialized-edge registry (#5347); the coverage-function
// code path here needs no edit.
func crossplaneXrdBlastRadiusCoverage() (bool, []blastRadiusEdgeCoverage) {
	complete := true
	coverage := make([]blastRadiusEdgeCoverage, 0, len(crossplaneXrdBlastRadiusEdgeTypes))
	for _, edgeType := range crossplaneXrdBlastRadiusEdgeTypes {
		c := EdgeMaterializationCoverage(edgeType)
		if !c.Materialized {
			complete = false
		}
		coverage = append(coverage, blastRadiusEdgeCoverage(c))
	}
	return complete, coverage
}

// blastRadiusTierLookupCypher resolves the deployment Tier (name + risk) for a
// bounded set of affected repositories, keyed on the concrete repo `id` (not
// name — names are not unique, which is why the dependents traversal anchors on
// id) so tier/risk is never mis-attributed to a same-named unrelated repo. Kept
// a SEPARATE single-clause query: folding it into the affected query as a
// trailing OPTIONAL MATCH re-triggers the multi-clause literal-text / row-drop
// defects on this build. The IN list is bounded by the response limit.
const blastRadiusTierLookupCypher = `MATCH (a:Repository)<-[:CONTAINS]-(tier:Tier)
WHERE a.id IN $repo_ids
RETURN a.id AS repo_id, tier.name AS tier, tier.risk_level AS risk`

// findBlastRadius analyzes the blast radius for a target entity.
// POST /api/v0/impact/blast-radius
// Body: {"target": "repo-name", "target_type": "repository"}
func (h *ImpactHandler) findBlastRadius(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.blast_radius") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"blast radius analysis requires authoritative platform truth",
			"unsupported_capability",
			"platform_impact.blast_radius",
			h.profile(),
			requiredProfile("platform_impact.blast_radius"),
		)
		return
	}

	var req struct {
		Target     string `json:"target"`
		TargetType string `json:"target_type"`
		Limit      int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Target == "" {
		WriteError(w, http.StatusBadRequest, "target is required")
		return
	}
	if req.TargetType == "" {
		WriteError(w, http.StatusBadRequest, "target_type is required")
		return
	}

	limit := normalizeImpactListLimit(req.Limit)
	affected, supported, complete, coverage, err := h.blastRadiusAffected(r.Context(), req.TargetType, req.Target, limit+1)
	if !supported {
		WriteError(w, http.StatusBadRequest, "unsupported target_type: "+req.TargetType)
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	affected, truncated := trimImpactRows(affected, limit)
	h.enrichBlastRadiusTiers(r.Context(), affected)

	entries := make([]map[string]any, 0, len(affected))
	for _, row := range affected {
		entry := map[string]any{"repo": StringVal(row, "repo")}
		if v := StringVal(row, "tier"); v != "" {
			entry["tier"] = v
		}
		if v := StringVal(row, "risk"); v != "" {
			entry["risk"] = v
		}
		if v := IntVal(row, "hops"); v > 0 {
			entry["hops"] = v
		}
		if v := StringVal(row, "repo_id"); v != "" {
			entry["repo_id"] = v
		}
		if v := StringVal(row, "claim"); v != "" {
			entry["claim"] = v
		}
		entries = append(entries, entry)
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"target":         req.Target,
		"target_type":    req.TargetType,
		"affected":       entries,
		"affected_count": len(entries),
		"limit":          limit,
		"truncated":      truncated,
		"complete":       complete,
		"coverage":       coverage,
	}, BuildTruthEnvelope(h.profile(), "platform_impact.blast_radius", TruthBasisHybrid, "resolved from platform graph impact analysis"))
}

// blastRadiusAffected resolves the affected repositories (repo, repo_id, hops,
// and claim for crossplane) for the target, using NornicDB-safe queries per
// target_type. It returns (rows, supported, complete, coverage, error):
// supported is false for an unknown target_type so the caller can emit a 400.
// Rows are de-duplicated by repo with the minimum hop retained and sorted by
// (hops, repo). complete and coverage report the #5330/#5331 honesty
// contract: for sql_table and crossplane_xrd, coverage lists every edge type
// the surface conceptually covers with its materialization status, and
// complete is false whenever any of them has no writer (crossplane_xrd is
// always false today — SATISFIED_BY has no writer; see
// crossplaneXrdBlastRadiusCoverage); other target_types have no coverage gaps
// registered against them and report complete:true with empty coverage.
func (h *ImpactHandler) blastRadiusAffected(ctx context.Context, targetType, target string, limit int) ([]map[string]any, bool, bool, []blastRadiusEdgeCoverage, error) {
	params := map[string]any{"target_name": target, "limit": limit}
	emptyCoverage := []blastRadiusEdgeCoverage{}
	switch targetType {
	case "repository":
		rows, err := h.Neo4j.Run(ctx, blastRadiusRepositoryCypher, params)
		return mergeBlastRadiusRows(rows), true, true, emptyCoverage, err
	case "terraform_module":
		src, err := h.Neo4j.Run(ctx, blastRadiusTerraformSourceReposCypher, params)
		if err != nil {
			return nil, true, true, emptyCoverage, err
		}
		affected := src
		if ids := distinctRepoIDs(src); len(ids) > 0 {
			deps, err := h.Neo4j.Run(ctx, blastRadiusDependentsByIDCypher, map[string]any{"repo_ids": ids, "limit": limit})
			if err != nil {
				return nil, true, true, emptyCoverage, err
			}
			affected = append(affected, deps...)
		}
		return mergeBlastRadiusRows(affected), true, true, emptyCoverage, nil
	case "crossplane_xrd":
		rows, err := h.Neo4j.Run(ctx, blastRadiusCrossplaneCypher, params)
		complete, coverage := crossplaneXrdBlastRadiusCoverage()
		return mergeBlastRadiusRows(rows), true, complete, coverage, err
	case "sql_table":
		// A repo can reach the table through several UNION branches (up to
		// blastRadiusSqlTableBranches rows for one repo), and the query's own
		// LIMIT applies before mergeBlastRadiusRows collapses those duplicates.
		// Over-fetch by the branch multiplier so the post-dedup unique set still
		// covers the requested limit before the handler trims it.
		rows, err := h.Neo4j.Run(ctx, blastRadiusSqlTableCypher, map[string]any{"target_name": target, "limit": limit * blastRadiusSqlTableBranches})
		complete, coverage := sqlTableBlastRadiusCoverage()
		return mergeBlastRadiusRows(rows), true, complete, coverage, err
	default:
		return nil, false, true, emptyCoverage, nil
	}
}

// enrichBlastRadiusTiers looks up the deployment tier for the affected repos and
// merges tier/risk into each row in place. Tier is optional enrichment (the
// pre-rewrite query joined it via OPTIONAL MATCH), so a lookup error degrades to
// no-tier rather than failing the whole blast-radius read; the affected set is
// already correct without it.
func (h *ImpactHandler) enrichBlastRadiusTiers(ctx context.Context, affected []map[string]any) {
	ids := distinctRepoIDs(affected)
	if len(ids) == 0 {
		return
	}
	rows, err := h.Neo4j.Run(ctx, blastRadiusTierLookupCypher, map[string]any{"repo_ids": ids})
	if err != nil {
		if h.Logger != nil {
			h.Logger.Warn("blast-radius tier enrichment failed; returning affected repos without tier", "error", err)
		}
		return
	}
	tiers := make(map[string]map[string]string, len(rows))
	for _, row := range rows {
		id := StringVal(row, "repo_id")
		if id == "" {
			continue
		}
		tiers[id] = map[string]string{"tier": StringVal(row, "tier"), "risk": StringVal(row, "risk")}
	}
	for _, row := range affected {
		if t, ok := tiers[StringVal(row, "repo_id")]; ok {
			if t["tier"] != "" {
				row["tier"] = t["tier"]
			}
			if t["risk"] != "" {
				row["risk"] = t["risk"]
			}
		}
	}
}

// distinctRepoIDs returns the unique non-empty repo ids from the rows. Used to
// anchor the terraform_module dependents traversal on concrete source-repo ids
// rather than names, so same-named-but-unrelated repos are not pulled in.
func distinctRepoIDs(rows []map[string]any) []string {
	return distinctFieldValues(rows, "repo_id")
}

// distinctFieldValues returns the unique non-empty values of key across rows,
// preserving first-seen order.
func distinctFieldValues(rows []map[string]any, key string) []string {
	seen := make(map[string]bool, len(rows))
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		v := StringVal(row, key)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// mergeBlastRadiusRows de-duplicates affected rows by repo name, keeping the
// minimum hop distance (so a repo reachable both directly and transitively, or
// a source repo that is also a dependent, is reported at its shortest path) and
// preserving the first-seen repo_id/claim. Results are sorted by (hops asc,
// repo asc) to match the removed Cypher ORDER BY. This is where per-repo
// min-hop lives now that the affected queries can no longer fold it across the
// UNION/two-query merge.
func mergeBlastRadiusRows(rows []map[string]any) []map[string]any {
	byRepo := make(map[string]map[string]any, len(rows))
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		name := StringVal(row, "repo")
		if name == "" {
			continue
		}
		existing, ok := byRepo[name]
		if !ok {
			merged := map[string]any{"repo": name}
			if v := StringVal(row, "repo_id"); v != "" {
				merged["repo_id"] = v
			}
			if v := StringVal(row, "claim"); v != "" {
				merged["claim"] = v
			}
			merged["hops"] = IntVal(row, "hops")
			byRepo[name] = merged
			order = append(order, name)
			continue
		}
		if hops := IntVal(row, "hops"); hops < IntVal(existing, "hops") {
			existing["hops"] = hops
		}
		if StringVal(existing, "repo_id") == "" {
			if v := StringVal(row, "repo_id"); v != "" {
				existing["repo_id"] = v
			}
		}
		if StringVal(existing, "claim") == "" {
			if v := StringVal(row, "claim"); v != "" {
				existing["claim"] = v
			}
		}
	}
	merged := make([]map[string]any, 0, len(order))
	for _, name := range order {
		merged = append(merged, byRepo[name])
	}
	sortBlastRadiusRows(merged)
	return merged
}

// sortBlastRadiusRows orders affected rows by ascending hop distance then repo
// name, matching the ORDER BY the affected Cypher applied before it was moved to
// Go (min-hop dedup can no longer rely on the query's ordering).
func sortBlastRadiusRows(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		hi, hj := IntVal(rows[i], "hops"), IntVal(rows[j], "hops")
		if hi != hj {
			return hi < hj
		}
		return StringVal(rows[i], "repo") < StringVal(rows[j], "repo")
	})
}
