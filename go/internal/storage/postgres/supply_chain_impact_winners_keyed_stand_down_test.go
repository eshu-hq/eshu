// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"testing"
)

// TestResweepWinnersKeyedStandsDownUnderTheLock pins the #11117 keyed-tick
// guard: the maintainer decides from a contract read taken before the lease
// claim, so a newer binary's full can commit in between. Under the resweep
// lock the keyed tick re-reads the stored version; above this binary's it
// consumes, applies and stamps nothing (no keyed apply and no over-cap
// rebuild with this binary's statements) and reports StoodDown.
func TestResweepWinnersKeyedStandsDownUnderTheLock(t *testing.T) {
	t.Parallel()

	tx := &phaseFakeTx{
		claimed: true, consumed: 3, identifier: 3, stored: 1,
		contract: []any{sql.NullInt64{Int64: SupplyChainImpactWinnersContractVersion + 1, Valid: true}, sql.NullString{String: "newer", Valid: true}},
	}
	got, err := SupplyChainImpactWinnersStore{DB: phaseFakeDB{tx: tx}}.ResweepWinnersKeyed(context.Background())
	if err != nil {
		t.Fatalf("ResweepWinnersKeyed() error = %v", err)
	}
	if !got.Skipped || !got.StoodDown {
		t.Fatalf("ResweepWinnersKeyed() = %+v, want Skipped and StoodDown under a newer stored contract", got)
	}
	if want := []string{tryClaimSupplyChainImpactWinnersResweepSQL, selectSupplyChainImpactWinnersContractSQL}; !slices.Equal(tx.queries, want) {
		t.Fatalf("queries = %q, want only the claim and the contract re-read", tx.queries)
	}
	if tx.rollbacks == 0 {
		t.Fatal("the stood-down tick did not roll its transaction back")
	}

	// A matching or lower stored contract keeps the tick running.
	for _, stored := range []int64{SupplyChainImpactWinnersContractVersion, SupplyChainImpactWinnersContractVersion - 1} {
		tx := &phaseFakeTx{
			claimed: true, consumed: 3, identifier: 3, stored: 1000,
			contract: []any{sql.NullInt64{Int64: stored, Valid: true}, sql.NullString{String: "h", Valid: true}},
		}
		got, err := SupplyChainImpactWinnersStore{DB: phaseFakeDB{tx: tx}}.ResweepWinnersKeyed(context.Background())
		if err != nil || got.Skipped || got.StoodDown {
			t.Fatalf("stored %d: ResweepWinnersKeyed() = %+v, %v; want a committed tick", stored, got, err)
		}
		if !slices.Contains(tx.queries, applyWinnersKeyedSQL) {
			t.Fatalf("stored %d: the keyed apply did not run", stored)
		}
	}
}
