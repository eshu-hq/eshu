// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

// TestSupplyChainImpactWinnerSelectMirrorsReadDedup pins the read/write parity
// the materialization depends on (#3389): the recompute winner selection must
// use the same canonical_key, public finding_id fallback, has_payload_finding_id
// tiebreak, severity bucket, and suppression default the read-time dedup in
// go/internal/query/supply_chain_impact_findings_queries.go uses. If these drift,
// the materialized winner stops matching what the read would have picked.
func TestSupplyChainImpactWinnerSelectMirrorsReadDedup(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		// fact_kind + active-generation scope, same as the read.
		"fact.fact_kind = 'reducer_supply_chain_impact_finding'",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		// canonical_key first component is the cve/advisory COALESCE.
		"COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), '')",
		// public finding_id fallback to canonical_key.
		"COALESCE(\n            NULLIF(fact.payload->>'finding_id', ''),",
		// exact dedup tiebreak.
		"ORDER BY\n                COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) DESC,",
		"CASE WHEN NULLIF(fact.payload->>'finding_id', '') IS NULL THEN 0 ELSE 1 END DESC,",
		"fact.fact_id ASC",
		"WHERE ranked.canonical_rank = 1",
		// severity bucket + suppression default, same thresholds/strings as read.
		"THEN 'critical'",
		"COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active')",
	} {
		if !strings.Contains(supplyChainImpactWinnerSelectSQL, want) {
			t.Fatalf("winner select SQL missing read-parity marker %q:\n%s", want, supplyChainImpactWinnerSelectSQL)
		}
	}
}

// TestRebuildSupplyChainImpactWinnersSQLIsAtomicReconcile pins that the rebuild
// upserts current winners, deletes winners no longer in the active set, and
// stamps the maintainer watermark in one statement (no torn rebuild visible to
// readers). The winners upsert and delete are data-modifying CTEs; the final,
// unconditional watermark upsert runs even on a zero-winner resweep so the read
// can tell "never populated" from "reswept to zero findings".
func TestRebuildSupplyChainImpactWinnersSQLIsAtomicReconcile(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"WITH winners_now AS (",
		"INSERT INTO supply_chain_impact_canonical_winners",
		"ON CONFLICT (canonical_key) DO UPDATE SET",
		"deleted AS (\n    DELETE FROM supply_chain_impact_canonical_winners w\n    WHERE NOT EXISTS (SELECT 1 FROM winners_now n WHERE n.canonical_key = w.canonical_key)\n)",
		// Unconditional watermark upsert is the final statement so it stamps even
		// a zero-winner resweep.
		"INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)",
		"ON CONFLICT (singleton) DO UPDATE SET materialized_at = EXCLUDED.materialized_at",
	} {
		if !strings.Contains(rebuildSupplyChainImpactWinnersSQL, want) {
			t.Fatalf("rebuild SQL missing %q:\n%s", want, rebuildSupplyChainImpactWinnersSQL)
		}
	}
}

func TestRebuildAllWinnersRequiresDB(t *testing.T) {
	t.Parallel()

	store := SupplyChainImpactWinnersStore{}
	if err := store.RebuildAllWinners(context.Background(), nil); err == nil {
		t.Fatal("RebuildAllWinners() error = nil, want missing-db error")
	}
}
