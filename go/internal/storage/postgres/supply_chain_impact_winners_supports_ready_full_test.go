// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"testing"
)

// TestResweepWinnersUnlessNewerStandsDownOnlyUnderANewerContract pins the
// supports-ready full's guard (#10880 PR-C1): under the resweep lock it re-reads
// the stored contract and, above this binary's version, runs nothing past that
// read, commits nothing and stamps no contract, reporting StoodDown. A current,
// lower, NULL or missing contract runs the full and stamps the binary's
// contract: unlike ResweepWinnersUnlessCurrent, a current contract does not stop
// it, because the supports-ready full rebuilds the current contract.
func TestResweepWinnersUnlessNewerStandsDownOnlyUnderANewerContract(t *testing.T) {
	t.Parallel()

	tx := &winnersFakeTx{
		claim: winnersClaimed(true), counts: &winnersCountRows{},
		storedContract: []any{sql.NullInt64{Int64: SupplyChainImpactWinnersContractVersion + 1, Valid: true}, sql.NullString{String: "newer", Valid: true}},
	}
	got, err := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}.ResweepWinnersUnlessNewer(context.Background())
	if err != nil || !got.Skipped || !got.StoodDown || got.ContractCurrent {
		t.Fatalf("newer stored contract: full = %+v, %v; want Skipped with StoodDown", got, err)
	}
	if want := []string{tryClaimSupplyChainImpactWinnersResweepSQL, selectSupplyChainImpactWinnersContractSQL}; !slices.Equal(tx.queries, want) {
		t.Fatalf("newer stored contract: queries = %q, want only the claim and the contract read", tx.queries)
	}
	if tx.committed || len(tx.contractStamps) != 0 {
		t.Fatalf("newer stored contract: committed %v, stamps %v; want nothing written", tx.committed, tx.contractStamps)
	}

	for name, row := range map[string][]any{
		"no row":  nil,
		"null":    {sql.NullInt64{}, sql.NullString{}},
		"lower":   {sql.NullInt64{Int64: SupplyChainImpactWinnersContractVersion - 1, Valid: true}, sql.NullString{String: "old", Valid: true}},
		"current": {sql.NullInt64{Int64: SupplyChainImpactWinnersContractVersion, Valid: true}, sql.NullString{String: supplyChainImpactWinnersContractHash, Valid: true}},
	} {
		tx := &winnersFakeTx{
			claim: winnersClaimed(true), counts: winnersCounts(int64(1), int64(0), int64(0), int64(0), sql.NullTime{}),
			storedContract: row,
		}
		got, err := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}.ResweepWinnersUnlessNewer(context.Background())
		if err != nil || got.Skipped || got.StoodDown || got.Inserted != 1 {
			t.Fatalf("%s: full = %+v, %v; want the full to run", name, got, err)
		}
		if !tx.committed || len(tx.contractStamps) != 1 {
			t.Fatalf("%s: committed %v, stamps %v; want a commit with one contract stamp", name, tx.committed, tx.contractStamps)
		}
	}
}
