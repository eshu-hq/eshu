// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
// The upsert runs in the transaction that writes the finding facts, under the
// conflict-domain lock the writer already holds, so a support row is never visible
// without its pass and never outlives its retraction. It derives the rows from the
// writer's own in-memory rows (#10880 PR-C2 h), with the same column expressions
// the fact-derived backfill uses, so the support is the fact payload verbatim.
type SupplyChainImpactSupportsMaintainer interface {
	// UpsertImpactSupportRows writes one support row per distinct fact id in rows,
	// in fact id order, rewriting a row only when a derived column changed.
	UpsertImpactSupportRows(ctx context.Context, rows []factwrite.VersionedRow) (SupplyChainImpactSupportsWrite, error)
	// RecordRetractedImpactSupports counts the supports the pass's retraction
	// statement deleted (the statement deletes them itself, #10880 PR-C2 i), with
	// the pass's other support rows once the transaction commits.
	RecordRetractedImpactSupports(ctx context.Context, rows int64)
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
func WriteSupplyChainImpactFindingRows(ctx context.Context, tx SupplyChainImpactTx, rows []factwrite.VersionedRow, timings *supplyChainImpactTxStatementTimings) error {
	// Bounded chunked bulk insert: findings are upserted in O(N/batchSize)
	// round-trips rather than one ExecContext per finding. When timings is set
	// each chunk folds its own wall time into the pass's fact_insert
	// accumulation, so one pass emits one fact_insert sample with the chunk
	// count and row total, never one series per chunk; a nil timings leaves
	// the statements untouched and records nothing.
	db := factwrite.Execer(tx)
	if timings != nil {
		db = &timedChunkExecer{inner: tx, timings: timings}
	}
	if err := factwrite.BatchInsertVersionedFacts(ctx, db, rows); err != nil {
		return fmt.Errorf("write supply chain impact fact: %w", err)
	}
	supports, hasSupports := tx.(SupplyChainImpactSupportsMaintainer)
	if !hasSupports || len(rows) == 0 {
		return nil
	}
	supportsStarted := time.Now()
	_, err := supports.UpsertImpactSupportRows(ctx, rows)
	timings.add(supplyChainImpactTxStmtSupportsUpsert, time.Since(supportsStarted), int64(len(rows)))
	if err != nil {
		return fmt.Errorf("write supply chain impact supports: %w", err)
	}
	return nil
}

// timedChunkExecer delegates to one bulk-insert chunk at a time while folding
// each chunk's wall time into the pass's fact_insert accumulation. Only clock
// reads and struct stores run here: no logging, no metric IO, and the
// statements, order and round-trips are the delegate's own.
type timedChunkExecer struct {
	inner   factwrite.Execer
	timings *supplyChainImpactTxStatementTimings
}

func (e *timedChunkExecer) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	started := time.Now()
	result, err := e.inner.ExecContext(ctx, query, args...)
	var rows int64
	if len(args) > 0 {
		if factIDs, ok := args[0].([]string); ok {
			rows = int64(len(factIDs))
		}
	}
	e.timings.add(supplyChainImpactTxStmtFactInsert, time.Since(started), rows)
	return result, err
}
