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
// use the same canonical_key, public finding_id fallback, source-owned
// tiebreak, severity bucket, and independently ranked operator suppression
// overlay that the read-time dedup uses. If these drift, the materialized
// winner stops matching what the read would have picked.
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
		// Source truth and operator decisions rank independently. The source
		// owns row identity and filter dimensions; an operator-only key cannot
		// fabricate a finding.
		"source_candidates AS (",
		"fact.scope_id <> 'operator:vulnerability_suppressions'",
		"source_ranked AS (",
		"COUNT(*) OVER (PARTITION BY canonical_key) AS source_count",
		"source_winners AS (",
		"SELECT DISTINCT ON (canonical_key) *",
		"operator_candidates AS (",
		"fact.scope_id = 'operator:vulnerability_suppressions'",
		"COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') <> 'active'",
		"operator_overrides AS (",
		"FROM source_winners AS source",
		"LEFT JOIN operator_overrides AS override",
		"ON override.canonical_key = source.canonical_key",
		// Exact source and operator dedup tiebreak.
		"ORDER BY canonical_key,",
		"priority_score DESC,",
		"has_payload_finding_id DESC,",
		"fact_id ASC",
		// severity bucket + suppression default, same thresholds/strings as read.
		"THEN 'critical'",
		"COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active')",
		// Only operator decisions carry a parsed read-time expiry. Invalid
		// non-empty values fail closed as already expired.
		"WHEN override.fact_id IS NULL",
		"pg_input_is_valid",
		"ELSE '-infinity'::timestamptz",
		"END AS suppression_expires_at",
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
		"suppression_state, suppression_expires_at",
		"ON CONFLICT (canonical_key) DO UPDATE SET",
		"suppression_expires_at = EXCLUDED.suppression_expires_at",
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
