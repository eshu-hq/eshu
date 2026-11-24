// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Proof seams (#10880 PR-C1) for the live proofs in this package's external test
// package. They live in a _test.go file so no binary carries them: a write-capable
// function outside the test build would be a way to bypass the writer.

// supplyChainImpactSupportsProofPairsSQL lists every (scope, generation) that holds
// a finding fact or a support.
const supplyChainImpactSupportsProofPairsSQL = `
SELECT scope_id, generation_id FROM fact_records
WHERE fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
UNION
SELECT scope_id, generation_id FROM supply_chain_impact_finding_supports
ORDER BY 1, 2`

// ProofSyncSupportsFromFacts brings the supports table to what the writer's
// maintainer would hold for the finding facts as they are now. In production the
// impact writer changes the facts and the supports in one transaction. A live proof
// that seeds or mutates finding facts with raw SQL, as the winners proofs do to
// simulate a change no capture saw, calls this afterwards so the supports follow the
// facts exactly as the writer would have left them. It runs the writer's own
// statements, not a copy: per (scope, generation), under the pair's conflict-domain
// lock, the retraction delete of the supports of tombstoned facts, then the support
// upsert of every live finding fact in chunks of 500. It stages no winners capture and
// moves no marker.
func ProofSyncSupportsFromFacts(ctx context.Context, database db.ExecQueryer) error {
	beginner, ok := database.(db.Beginner)
	if !ok {
		return errors.New("proof support sync requires a transaction beginner")
	}
	rows, err := database.QueryContext(ctx, supplyChainImpactSupportsProofPairsSQL)
	if err != nil {
		return fmt.Errorf("list scope generations to sync supports for: %w", err)
	}
	type pair struct{ scope, generation string }
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.scope, &p.generation); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan scope generation: %w", err)
		}
		pairs = append(pairs, p)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range pairs {
		if err := proofSyncPair(ctx, beginner, p.scope, p.generation); err != nil {
			return err
		}
	}
	return nil
}

func proofSyncPair(ctx context.Context, beginner db.Beginner, scope, generation string) error {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin proof support sync: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, supplychaincore.SupplyChainImpactConflictDomainLockQuery,
		supplychaincore.SupplyChainImpactConflictDomainKey(scope, generation)); err != nil {
		return fmt.Errorf("lock supply chain impact conflict domain: %w", err)
	}
	if _, err := deleteRetractedSupports(ctx, tx, scope, generation); err != nil {
		return err
	}
	after := ""
	for {
		ids, err := queryBackfillFactIDs(ctx, tx, supplyChainImpactSupportsBackfillChunkSQL, scope, generation, after, supplyChainImpactSupportsBackfillDefaultChunk)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}
		if _, err := upsertSupports(ctx, tx, ids); err != nil {
			return err
		}
		after = ids[len(ids)-1]
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit proof support sync: %w", err)
	}
	return nil
}

// WinnersProofSelects returns the winners select over the finding facts (the audit's
// statement) and the same select over the supports table (the maintainer's), both
// with no arguments.
func WinnersProofSelects() (fromFacts, fromSupports string) {
	return supplyChainImpactWinnerSelectSQL, supplyChainImpactWinnerSelectFromSupportsSQL
}

// WinnersProofDataColumns returns the winners table's data columns in order, the
// columns after canonical_key that the select produces.
func WinnersProofDataColumns() []string {
	return append([]string(nil), supplyChainImpactWinnerDataColumns...)
}

// WinnersProofStagingTable returns the winners dirty-capture staging table name.
func WinnersProofStagingTable() string { return winnersStagingTable }

// WinnersProofKeyedProbeSQL returns the keyed active_facts probe statement the winners
// maintainer runs over the supports table.
func WinnersProofKeyedProbeSQL() string { return buildWinnersKeyedActiveFactsFromSupportsSQL }

// WinnersProofKeyedIdentsTable returns the session table the keyed probe reads its
// identifiers from.
func WinnersProofKeyedIdentsTable() string { return winnersKeyedIdentsTable }
