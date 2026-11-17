// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestSupplyChainImpactWinnersResweepKeyedScopeExpansionPlanLive is the
// failing-first contract for the capacity lane (eshu-hq/eshu#10839, Decision
// 4b-e): at a scope-heavy composition the convert and applied-delete plans
// must drive the per-tick scope expansion through the tick's temp indexes,
// never through statistics-blind nested-loop sequential scans. The shim
// measured 83% of the worst-case tick in those two consumers chewing the
// admitted-window by generation-size expansion (5,071 x 3,558 row comparisons
// per delete); the temp index is what bounds that fan-out's plan.
func TestSupplyChainImpactWinnersResweepKeyedScopeExpansionPlanLive(t *testing.T) {
	h := newKeyedHarness(t, "#10839 scope-expansion plan proof")
	// Four scopes with a wide rewritten generation each: every activation
	// queues scope rows whose expansion is hundreds of identifiers, the
	// composition whose convert/delete plans went statistics-blind.
	for s := range 4 {
		scope := fmt.Sprintf("vuln-intel://10839-plan/s%d", s)
		h.seedScope(scope, scope+"/g1")
		h.write(scope, scope+"/g1", chunkedFindings(fmt.Sprintf("plan%d-a", s), 20, 10, 0)...)
		h.addGeneration(scope, scope+"/g2", "pending")
		h.write(scope, scope+"/g2", chunkedFindings(fmt.Sprintf("plan%d-b", s), 20, 10, 1000)...)
		h.activate(scope, scope+"/g1", scope+"/g2")
	}
	// Production tables carry planner statistics on a real backend, so the
	// only estimates the plans can miss are the tick's own temp tables.
	h.exec(`ANALYZE fact_records`)
	h.exec(`ANALYZE supply_chain_impact_winners_dirty_log`)

	convertPlan, deletePlan, err := postgres.ExplainWinnersKeyedSettleForTest(h.ctx, h.db)
	if err != nil {
		t.Fatalf("explain settle: %v", err)
	}
	t.Logf("convert plan:\n%s", convertPlan)
	t.Logf("applied-delete plan:\n%s", deletePlan)

	// The convert must probe the chunk through its unique index (not rescan
	// it per expansion row) and reach the expansion through the
	// (scope_id, generation_id) index.
	if !strings.Contains(convertPlan, "winners_keyed_chunk_idents_all_idx") {
		t.Error("convert plan never probes the tick chunk identifiers through winners_keyed_chunk_idents_all_idx: the scope-expansion fan-out is not index-bounded")
	}
	if !strings.Contains(convertPlan, "winners_keyed_scope_idents_scope_idx") {
		t.Error("convert plan never reaches the scope expansion through winners_keyed_scope_idents_scope_idx: the expansion join is not index-bounded")
	}
	if strings.Contains(convertPlan, "Join Filter: ((chunk.package_id = si.package_id)") {
		t.Error("convert plan filters chunk membership per row instead of probing the chunk index: the statistics-blind nested loop is back")
	}
	// The applied delete must join the dirty log to the admitted window on
	// an index in at least one direction, never as a nested-loop
	// sequential scan filtered on seq after the fact.
	if !strings.Contains(deletePlan, "supply_chain_impact_winners_dirty_log_pkey") &&
		!strings.Contains(deletePlan, "winners_keyed_consumed_seq_idx") {
		t.Error("applied-delete plan joins the dirty log to the admitted window with no index in either direction")
	}
	if strings.Contains(deletePlan, "Join Filter: (d.seq = c.seq)") {
		t.Error("applied-delete plan filters d.seq = c.seq per row instead of an index join: the statistics-blind nested loop is back")
	}
}
