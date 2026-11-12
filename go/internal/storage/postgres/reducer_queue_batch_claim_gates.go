// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "strings"

// Batch-claim step-1 precomputed gate CTEs (#10565).
//
// The batch claim's `base` CTE used to evaluate every admission gate as a
// correlated per-row SubPlan: the readiness NOT EXISTS, the projector-drain
// NOT EXISTS, and four semantic count / NOT EXISTS subqueries. Each claim
// materialized every live reducer row and re-ran those gates once per row,
// which cost ~1.9 s on a 22k-row fixture (base alone ~1.25 s) and ~3.95 s
// per claim in production. The cost is the presence of per-row subplans,
// not any single predicate: disabling all three gate expressions drops base
// to ~120 ms.
//
// This file computes every gate once per claim as materialized sets/scalars
// and lets `base` read them through JOINs:
//
//   - projector_live_scopes: DISTINCT scope_ids with live projector work.
//   - semantic_scalars: one-row global counts (projector any-live,
//     projector-done, semantic inflight).
//   - semantic_ranked: window rank over semantic rows only, replacing the
//     per-row semantic successor count (rank N = count of rows <= self).
//   - readiness_missing: work items failing the readiness gate, computed
//     with the gate's own predicate text (outer NOT EXISTS flipped to
//     EXISTS). Equivalence to the shipped gate is De Morgan exact.
//
// Everything downstream of `base` (reps_ranked onward) is byte-identical,
// so the fairness contract (#3385 interleave, #8024 idle-domain boost,
// search-doc last) and the EvalPlanQual `locked` recheck (invariant I5)
// are untouched. Step 2 (representatives-first, sweep precompute) is
// explicitly out of scope and not bundled here.
//
// Self-exclusion dependency (C-4): the first semantic inflight check in the
// shipped query excludes the candidate row itself
// (work_item_id <> fact_work_items.work_item_id), while semantic_scalars
// counts every live semantic lease. The two agree iff `base` excludes
// live-leased rows, which its
// (claim_until IS NULL OR claim_until <= $1) filter does: a base row can
// never be a member of the inflight count. TestClaimBatchBaseExcludesLiveLeases
// pins that filter so a future change fails loudly instead of silently
// breaking equivalence.
func reducerClaimProjectorLiveScopesCTE() string {
	return `projector_live_scopes AS MATERIALIZED (
    SELECT DISTINCT scope_id
    FROM fact_work_items
    WHERE stage = 'projector'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
)`
}

func reducerClaimSemanticScalarsCTE() string {
	return `semantic_scalars AS MATERIALIZED (
    SELECT
        EXISTS (
            SELECT 1 FROM fact_work_items
            WHERE stage = 'projector' AND domain = 'source_local'
              AND status IN ('pending', 'retrying', 'claimed', 'running')
        ) AS proj_any_live,
        (SELECT count(*) FROM fact_work_items
          WHERE stage = 'projector' AND domain = 'source_local'
            AND status = 'succeeded') AS proj_done,
        (SELECT count(*) FROM fact_work_items
          WHERE stage = 'reducer' AND domain = 'semantic_entity_materialization'
            AND status IN ('claimed', 'running') AND claim_until > $1) AS sem_inflight
)`
}

func reducerClaimSemanticRankedCTE() string {
	return `semantic_ranked AS MATERIALIZED (
    SELECT work_item_id,
           row_number() OVER (ORDER BY updated_at ASC, work_item_id ASC) AS sem_rank
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND domain = 'semantic_entity_materialization'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND (visible_at IS NULL OR visible_at <= $1 OR (status = 'pending' AND visible_at <= clock_timestamp()))
      AND (claim_until IS NULL OR claim_until <= $1)
)`
}

// reducerClaimReadinessMissingCTE renders the readiness-missing set from the
// same Go source as the shipped gate: reducerClaimReadinessGateSQL with the
// work alias rebound to the CTE's own "w" alias and the outer NOT EXISTS
// flipped to EXISTS (missing = some requirement has no committed phase). The
// flip is De Morgan exact, and gate-text drift propagates here automatically
// instead of silently diverging; the derivability guard pins both.
func reducerClaimReadinessMissingCTE() string {
	return `readiness_missing AS MATERIALIZED (
    SELECT w.work_item_id
    FROM fact_work_items AS w
    WHERE w.stage = 'reducer'
      AND w.status IN ('pending', 'retrying', 'claimed', 'running')
      AND ` + reducerClaimReadinessMissingPredicateSQL() + `
)`
}

// flipReadinessGateToExists flips the outer NOT EXISTS of the shipped
// readiness-gate text to EXISTS (missing = some requirement has no committed
// phase). The gate starts with its outer NOT EXISTS, so the single
// replacement targets exactly that occurrence; the inner per-requirement NOT
// EXISTS keeps its negation. ok is false when the gate no longer opens with
// NOT EXISTS, so a future gate reshape fails loudly instead of shipping a
// silently wrong missing set.
func flipReadinessGateToExists(gate string) (string, bool) {
	const needle = "NOT EXISTS ("
	if !strings.HasPrefix(strings.TrimSpace(gate), needle) {
		return "", false
	}
	flipped := strings.Replace(gate, needle, "EXISTS (", 1)
	if !strings.Contains(flipped, "EXISTS (") {
		return "", false
	}
	return flipped, true
}

// reducerClaimReadinessMissingPredicateSQL derives the missing-set predicate
// from the shipped gate text via flipReadinessGateToExists. The work alias is
// the CTE's own "w" with the same requirement/phase aliases the shipped gate
// uses, so the derivability guard compares identical text; the predicate logic
// still comes from reducerClaimReadinessGateSQL, never from a hand copy.
func reducerClaimReadinessMissingPredicateSQL() string {
	gate := reducerClaimReadinessGateSQL("w", "readiness_req", "readiness_phase")
	flipped, ok := flipReadinessGateToExists(gate)
	if !ok {
		panic("reducerClaimReadinessMissingPredicateSQL: shipped readiness gate no longer starts with NOT EXISTS; update the flip")
	}
	return flipped
}

// reducerClaimBatchGatePrecomputeCTEs joins the four precompute CTEs in WITH
// order: after reducer_claim_readiness_requirements (which readiness_missing
// references) and before base (which reads all four through JOINs).
func reducerClaimBatchGatePrecomputeCTEs() string {
	return reducerClaimProjectorLiveScopesCTE() + ",\n" +
		reducerClaimSemanticScalarsCTE() + ",\n" +
		reducerClaimSemanticRankedCTE() + ",\n" +
		reducerClaimReadinessMissingCTE()
}
