// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestClaimBatchGatePrecomputeCTEs is the query-shape gate for issue #10565
// step 1: the batch claim must compute every admission gate once per claim as
// materialized sets/scalars (projector-live scope set, semantic scalars, the
// semantic successor rank over semantic rows only, the readiness-missing set)
// and read them through LEFT JOINs in `base`, instead of evaluating per-row
// correlated SubPlan gate expressions for every candidate row. Everything from
// `reps_ranked` onward stays byte-identical (proved set- and order-identical
// by TestClaimBatchGatePrecomputeMatchesPreChangeCandidateSetAndOrder).
func TestClaimBatchGatePrecomputeCTEs(t *testing.T) {
	t.Parallel()

	query := claimReducerWorkBatchQuery
	for _, want := range []string{
		"projector_live_scopes AS MATERIALIZED (",
		"semantic_scalars AS MATERIALIZED (",
		"semantic_ranked AS MATERIALIZED (",
		"readiness_missing AS MATERIALIZED (",
		"(readiness_missing.work_item_id IS NULL) AS readiness_ok",
		"($5 = false OR projector_live_scopes.scope_id IS NULL) AS projector_ok",
		"CROSS JOIN semantic_scalars",
		"LEFT JOIN projector_live_scopes",
		"LEFT JOIN readiness_missing",
		"LEFT JOIN semantic_ranked",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing precomputed-gate construct %q", want)
		}
	}
	// The per-row correlated gate re-scans this rewrite exists to remove must
	// not come back: projector drain, semantic scalars/successor counts, and
	// the readiness gate evaluated once per candidate row.
	for _, forbidden := range []string{
		"FROM fact_work_items AS projector_work",
		"FROM fact_work_items AS projector_any",
		"FROM fact_work_items AS projector_done",
		"FROM fact_work_items AS semantic_inflight",
		"FROM fact_work_items AS semantic_inflight2",
		"FROM fact_work_items AS semantic_next",
	} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("batch claim query still evaluates per-row correlated gate %q in base", forbidden)
		}
	}
}

// TestClaimBatchGatePrecomputeDerivedFromShippedSources pins the C-4 drift
// guard: the readiness-missing CTE must carry the shipped readiness gate
// predicate (reducerClaimReadinessGateSQL for the CTE's own row alias, with
// the outer NOT EXISTS flipped to EXISTS — the De Morgan inversion), so a
// future gate edit flows into both the sweep and the precomputed set instead
// of silently diverging from it. Comparison is whitespace-normalized because
// the CTE embeds the gate at its own indentation.
func TestClaimBatchGatePrecomputeDerivedFromShippedSources(t *testing.T) {
	t.Parallel()

	precompute := reducerClaimBatchGatePrecomputeCTEs()
	if !strings.Contains(claimReducerWorkBatchQuery, precompute) {
		t.Fatal("batch claim query does not contain reducerClaimBatchGatePrecomputeCTEs() verbatim: " +
			"the precomputed gates must be generated from the same Go source as the shipped query")
	}
	gate := reducerClaimReadinessGateSQL("w", "readiness_req", "readiness_phase")
	if strings.Count(gate, "NOT EXISTS (") != 2 {
		t.Fatalf("shipped readiness gate no longer has the (outer, inner) NOT EXISTS pair: %q", gate)
	}
	flipped := strings.Replace(gate, "NOT EXISTS (", "EXISTS (", 1)
	if !strings.Contains(normClaimGateWS(precompute), normClaimGateWS(flipped)) {
		t.Fatalf("precomputed CTEs do not carry the shipped readiness gate text:\n%s", flipped)
	}
}

func normClaimGateWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// TestClaimBatchBaseExcludesLiveLeases pins the self-exclusion dependency the
// correctness lane named (06-review-disposition.md C-4): the precomputed
// semantic scalars drop the per-row `work_item_id <> ...` self-exclusion
// because `base` pre-filters live leases, so a base row can never count
// itself in the live semantic-inflight tally. If a future filter change lets
// live-leased rows into `base`, the precomputed scalar silently diverges by
// one — this assertion must fail loudly first.
func TestClaimBatchBaseExcludesLiveLeases(t *testing.T) {
	t.Parallel()

	base := claimReducerWorkBatchQuery
	for _, want := range []string{
		"AND (claim_until IS NULL OR claim_until <= $1)",
		"AND lock_target.status IN ('pending', 'retrying', 'claimed', 'running')",
		"AND (lock_target.claim_until IS NULL OR lock_target.claim_until <= $1)",
	} {
		if !strings.Contains(base, want) {
			t.Fatalf("batch claim query missing live-lease exclusion %q "+
				"(the precomputed semantic scalar relies on it)", want)
		}
	}
}
