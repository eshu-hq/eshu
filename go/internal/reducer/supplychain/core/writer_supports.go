// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// SupplyChainImpactSupportsWrite counts what one supports upsert did: rows
// inserted, rows updated because a derived column changed, and rows left alone
// because the stored support already matched (the write-on-change guard).
type SupplyChainImpactSupportsWrite struct {
	Inserted  int64
	Updated   int64
	Unchanged int64
}

// SupplyChainImpactSupportsMaintainer is the optional surface a write
// transaction offers for keeping the per-support table current (#10880 PR-B1,
// migration 338). postgres.SupplyChainImpactBeginner's transaction implements
// it. WriteSupplyChainImpactFindingRows refuses a transaction without it.
//
// The upsert runs in the pass's transaction, under the conflict-domain lock the
// writer already holds, so a support row is never visible without its pass and never
// outlives its retraction. It derives the rows from the writer's own in-memory rows
// (#10880 PR-C2 h), with the same column expressions the fact-derived backfill uses,
// so the support is the finding payload verbatim.
type SupplyChainImpactSupportsMaintainer interface {
	// UpsertImpactSupportRows writes one support row per distinct fact id in rows,
	// in fact id order, rewriting a row only when a derived column changed.
	UpsertImpactSupportRows(ctx context.Context, rows []factwrite.VersionedRow) (SupplyChainImpactSupportsWrite, error)
	// RecordRetractedImpactSupports counts the supports the pass's retraction
	// statement deleted (the statement deletes them itself, #10880 PR-C2 i), with
	// the pass's other support rows once the transaction commits.
	RecordRetractedImpactSupports(ctx context.Context, rows int64)
}

// WriteSupplyChainImpactFindingRows is the impact writer's own write step
// (#10880 PR-B2, arbiter-b2c decision 7; #10880 PR-C2 j): it upserts the per-support
// table from the pass's rows, in the same transaction as the retraction and the
// pass state. It no longer writes the finding fact rows: the supports table is the
// only copy of a finding. Every admission of finding rows calls it, the writer's
// pass and the advisory-group live cell's admission alike, so the finding-fact
// write stop is this one place.
//
// The caller holds the (scope, generation) conflict-domain lock
// (SupplyChainImpactConflictDomainLockQuery) and runs in a transaction opened by
// the production beginner. A transaction that cannot maintain the supports table
// is refused rather than accepted silently: with no fact write behind it, a pass
// that wrote nothing would report success. No rows write nothing.
func WriteSupplyChainImpactFindingRows(ctx context.Context, tx SupplyChainImpactTx, rows []factwrite.VersionedRow, timings *supplyChainImpactTxStatementTimings) error {
	if len(rows) == 0 {
		return nil
	}
	supports, ok := tx.(SupplyChainImpactSupportsMaintainer)
	if !ok {
		return errSupplyChainImpactNoSupportsMaintainer
	}
	supportsStarted := time.Now()
	_, err := supports.UpsertImpactSupportRows(ctx, rows)
	timings.add(supplyChainImpactTxStmtSupportsUpsert, time.Since(supportsStarted), int64(len(rows)))
	if err != nil {
		return fmt.Errorf("write supply chain impact supports: %w", err)
	}
	return nil
}

// errSupplyChainImpactNoSupportsMaintainer is returned when a write transaction
// cannot maintain the per-support table, the only store a finding row has.
var errSupplyChainImpactNoSupportsMaintainer = errors.New("supply chain impact transaction cannot maintain the supports table; finding rows would be dropped")
