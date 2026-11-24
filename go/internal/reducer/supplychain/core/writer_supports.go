// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// SupplyChainImpactSupportsWrite counts what one supports upsert did: rows
// inserted, rows updated because a derived column changed, and rows left alone
// because the fact row already matched (the write-on-change guard).
type SupplyChainImpactSupportsWrite struct {
	Inserted  int64
	Updated   int64
	Unchanged int64
}

// SupplyChainImpactSupportsMaintainer is the optional surface a write
// transaction offers for keeping the per-support table current (#10880 PR-B1,
// migration 338). postgres.SupplyChainImpactBeginner's transaction implements
// it. A transaction without it maintains nothing, which is only valid in a test:
// the marker-gated backfill and the drift audit repair a missed write in
// production.
//
// Both calls run in the transaction that writes the finding facts, under the
// conflict-domain lock the writer already holds, so a support row is never
// visible without its fact and never outlives its retraction. Each derives its
// rows from fact_records, never from the in-memory findings, so the fact payload
// stays the one source of truth for the canonical key and the filter columns.
type SupplyChainImpactSupportsMaintainer interface {
	// UpsertImpactSupports writes one support row per live (non-tombstoned) fact
	// id, in fact id order, rewriting a row only when a derived column changed.
	UpsertImpactSupports(ctx context.Context, factIDs []string) (SupplyChainImpactSupportsWrite, error)
	// DeleteRetractedImpactSupports removes the supports of the (scope,
	// generation) whose fact the retraction tombstoned, and returns the rows
	// removed.
	DeleteRetractedImpactSupports(ctx context.Context, scopeID, generationID string) (int64, error)
}

// WriteSupplyChainImpactFindingRows is the impact writer's own facts-plus-supports
// step (#10880 PR-B2, arbiter-b2c decision 7): it upserts the finding fact rows
// in bounded chunks, then keeps the per-support table current in the same
// transaction by upserting the supports of those fact ids. Every admission of
// finding rows calls it, the writer's pass and the advisory-group live cell's
// admission alike, so the finding-fact write stop changes this one place.
//
// The caller holds the (scope, generation) conflict-domain lock
// (SupplyChainImpactConflictDomainLockQuery) and runs in a transaction opened by
// the production beginner, which sets the old-writer fence setting so these rows
// never mark their scope dirty. The supports are read back from the stored fact
// rows, so a row a fresher writer's fencing token kept is mirrored as stored, not
// as offered. A transaction without the supports maintainer writes the facts only
// (valid in a test; production repairs it through the backfill). No rows write
// nothing.
func WriteSupplyChainImpactFindingRows(ctx context.Context, tx SupplyChainImpactTx, rows []factwrite.VersionedRow) error {
	// Bounded chunked bulk insert: findings are upserted in O(N/batchSize)
	// round-trips rather than one ExecContext per finding.
	if err := factwrite.BatchInsertVersionedFacts(ctx, tx, rows); err != nil {
		return fmt.Errorf("write supply chain impact fact: %w", err)
	}
	supports, hasSupports := tx.(SupplyChainImpactSupportsMaintainer)
	if !hasSupports || len(rows) == 0 {
		return nil
	}
	factIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		factIDs = append(factIDs, row.FactID)
	}
	if _, err := supports.UpsertImpactSupports(ctx, factIDs); err != nil {
		return fmt.Errorf("write supply chain impact supports: %w", err)
	}
	return nil
}
