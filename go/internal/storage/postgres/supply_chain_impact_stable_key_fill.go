// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// SupplyChainImpactStableKeyFill fills supply_chain_impact_finding_supports.stable_fact_key
// for the supports no rewrite has keyed, in resumable steps (#10880 PR-C1k). Each
// step updates at most ChunkSize (never more than 500) supports of one (scope,
// generation) in one transaction under that pair's conflict-domain advisory lock,
// the lock the impact writer holds around its support writes, so a step cannot
// interleave with a writer's rewrite of the same rows. The cursor advances in the
// chunk's own transaction. The marker is set only after a sliced verification finds
// no support without a key whose twin has one. It never runs as one statement over
// the table. One reducer runs it at a time (the caller holds an election lock); the
// marker updates are guarded so a second runner cannot complete it twice.
type SupplyChainImpactStableKeyFill struct {
	// DB must support transactions (db.Beginner).
	DB db.ExecQueryer
	// ChunkSize bounds the supports one step updates; zero or above the ceiling
	// means 500.
	ChunkSize int
	// VerifySliceSize bounds the supports one verification slice reads; zero means
	// the supports backfill's default (20,000).
	VerifySliceSize int
}

// SupplyChainImpactStableKeyFillReport describes one RunStep.
type SupplyChainImpactStableKeyFillReport struct {
	// Filled counts supports the step gave a key. Skipped counts the chunk's
	// supports it left without one because their twin has none.
	Filled, Skipped int64
	// Verified is true when the step ran a verification slice; Missing is that
	// slice's count of supports still keyless although their twin has a key
	// (nonzero restarts the sweep).
	Verified bool
	Missing  int64
	// VerifySliceDuration is the wall time of that slice statement.
	VerifySliceDuration time.Duration
	// Complete is true when the marker is set.
	Complete bool
}

type stableKeyFillMarker struct {
	scope, generation, fact string
	complete                bool
	sweeps                  int64
	verify                  [3]string
}

func (f SupplyChainImpactStableKeyFill) chunk() int {
	if f.ChunkSize <= 0 || f.ChunkSize > supplyChainImpactStableKeyFillMaxChunk {
		return supplyChainImpactStableKeyFillMaxChunk
	}
	return f.ChunkSize
}

// RunStep performs one bounded unit of work: a chunk, a pair walk, or one
// verification slice. Call it until Complete is true.
func (f SupplyChainImpactStableKeyFill) RunStep(ctx context.Context) (SupplyChainImpactStableKeyFillReport, error) {
	var report SupplyChainImpactStableKeyFillReport
	beginner, ok := f.DB.(db.Beginner)
	if !ok {
		return report, errors.New("supply chain impact stable key fill requires a transaction beginner")
	}
	if _, err := f.DB.ExecContext(ctx, supplyChainImpactSupportsBackfillEnsureMarkerSQL,
		SupplyChainImpactStableKeyFillMarker, SupplyChainImpactStableKeyFillContractVersion); err != nil {
		return report, fmt.Errorf("ensure supply chain impact stable key fill marker: %w", err)
	}
	marker, err := f.readMarker(ctx)
	if err != nil {
		return report, err
	}
	if marker.complete {
		report.Complete = true
		return report, nil
	}
	scope, generation, after := marker.scope, marker.generation, marker.fact
	if scope == "" {
		var found bool
		if scope, generation, found, err = f.nextPair(ctx, "", ""); err != nil {
			return report, err
		}
		if !found {
			return f.verify(ctx, beginner, marker, report)
		}
		after = ""
	}
	for pairs := 0; ; pairs++ {
		filled, skipped, last, err := f.fillChunk(ctx, beginner, scope, generation, after)
		report.Filled += filled
		report.Skipped += skipped
		if err != nil || last != "" {
			return report, err
		}
		next, nextGeneration, found, err := f.nextPair(ctx, scope, generation)
		if err != nil {
			return report, err
		}
		if !found {
			return f.verify(ctx, beginner, marker, report)
		}
		scope, generation, after = next, nextGeneration, ""
		if pairs+1 >= supplyChainImpactStableKeyFillMaxPairsPerStep {
			// Remember the pair the walk reached so the next step resumes there.
			if _, err := f.DB.ExecContext(ctx, supplyChainImpactSupportsBackfillAdvanceSQL,
				SupplyChainImpactStableKeyFillMarker, scope, generation, ""); err != nil {
				return report, fmt.Errorf("advance supply chain impact stable key fill cursor: %w", err)
			}
			return report, nil
		}
	}
}

func (f SupplyChainImpactStableKeyFill) readMarker(ctx context.Context) (stableKeyFillMarker, error) {
	var m stableKeyFillMarker
	rows, err := f.DB.QueryContext(ctx, supplyChainImpactSupportsBackfillReadMarkerSQL, SupplyChainImpactStableKeyFillMarker)
	if err != nil {
		return m, fmt.Errorf("read supply chain impact stable key fill marker: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return m, fmt.Errorf("read supply chain impact stable key fill marker: %w", err)
		}
		return m, errors.New("supply chain impact stable key fill marker row is missing")
	}
	var verifyCurrent bool
	if err := rows.Scan(&m.scope, &m.generation, &m.fact, &m.complete, &m.sweeps,
		&m.verify[0], &m.verify[1], &m.verify[2], &verifyCurrent); err != nil {
		return m, fmt.Errorf("scan supply chain impact stable key fill marker: %w", err)
	}
	if !verifyCurrent {
		m.verify = [3]string{}
	}
	return m, nil
}

func (f SupplyChainImpactStableKeyFill) nextPair(ctx context.Context, scope, generation string) (nextScope, nextGeneration string, found bool, err error) {
	rows, err := f.DB.QueryContext(ctx, supplyChainImpactStableKeyFillNextPairSQL, scope, generation)
	if err != nil {
		return "", "", false, fmt.Errorf("find the next scope and generation to fill: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", "", false, fmt.Errorf("find the next scope and generation to fill: %w", err)
		}
		return "", "", false, nil
	}
	if err := rows.Scan(&nextScope, &nextGeneration); err != nil {
		return "", "", false, fmt.Errorf("scan the next scope and generation to fill: %w", err)
	}
	return nextScope, nextGeneration, true, nil
}

// fillChunk fills one chunk of the pair under its conflict-domain lock and
// advances the cursor in the same transaction. An empty last means the pair has no
// keyless support after the cursor.
func (f SupplyChainImpactStableKeyFill) fillChunk(ctx context.Context, beginner db.Beginner, scope, generation, after string) (filled, skipped int64, last string, err error) {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return 0, 0, "", fmt.Errorf("begin supply chain impact stable key fill transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, supplychaincore.SupplyChainImpactConflictDomainLockQuery,
		supplychaincore.SupplyChainImpactConflictDomainKey(scope, generation)); err != nil {
		return 0, 0, "", fmt.Errorf("lock supply chain impact conflict domain: %w", err)
	}
	ids, err := queryBackfillFactIDs(ctx, tx, supplyChainImpactStableKeyFillChunkSQL, scope, generation, after, f.chunk())
	if err != nil {
		return 0, 0, "", err
	}
	if len(ids) > 0 {
		last = ids[len(ids)-1]
		result, err := tx.ExecContext(ctx, supplyChainImpactStableKeyFillUpdateSQL, array.Of(ids))
		if err != nil {
			return 0, 0, "", fmt.Errorf("fill supply chain impact stable keys: %w", err)
		}
		if filled, err = result.RowsAffected(); err != nil {
			return 0, 0, "", fmt.Errorf("count filled supply chain impact stable keys: %w", err)
		}
		skipped = int64(len(ids)) - filled
		if _, err := tx.ExecContext(ctx, supplyChainImpactSupportsBackfillAdvanceSQL,
			SupplyChainImpactStableKeyFillMarker, scope, generation, last); err != nil {
			return 0, 0, "", fmt.Errorf("advance supply chain impact stable key fill cursor: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, "", fmt.Errorf("commit supply chain impact stable key fill chunk: %w", err)
	}
	return filled, skipped, last, nil
}

// verify runs one verification slice once the sweep has walked every pair. A
// keyless support whose twin has a key restarts the sweep; a clean slice moves the
// verification cursor; the clean last slice sets the marker.
func (f SupplyChainImpactStableKeyFill) verify(
	ctx context.Context, beginner db.Beginner, marker stableKeyFillMarker, report SupplyChainImpactStableKeyFillReport,
) (SupplyChainImpactStableKeyFillReport, error) {
	size := f.VerifySliceSize
	if size <= 0 {
		size = supplyChainImpactSupportsBackfillDefaultVerifySlice
	}
	started := time.Now()
	rows, err := f.DB.QueryContext(ctx, supplyChainImpactStableKeyFillVerifySliceSQL,
		marker.verify[0], marker.verify[1], marker.verify[2], size)
	report.VerifySliceDuration = time.Since(started)
	if err != nil {
		return report, fmt.Errorf("verify supply chain impact stable key fill slice: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return report, fmt.Errorf("verify supply chain impact stable key fill slice: %w", err)
		}
		return report, errors.New("verify supply chain impact stable key fill slice: no row")
	}
	var supports, missing int64
	var last bool
	var bound [3]string
	if err := rows.Scan(&supports, &missing, &last, &bound[0], &bound[1], &bound[2]); err != nil {
		return report, fmt.Errorf("scan supply chain impact stable key fill slice: %w", err)
	}
	report.Verified, report.Missing = true, missing
	switch {
	case missing > 0:
		if _, err := f.DB.ExecContext(ctx, supplyChainImpactSupportsBackfillRestartSQL, SupplyChainImpactStableKeyFillMarker); err != nil {
			return report, fmt.Errorf("restart supply chain impact stable key fill: %w", err)
		}
	case last:
		complete, err := f.complete(ctx, beginner, marker.sweeps)
		report.Complete = complete
		return report, err
	default:
		if _, err := f.DB.ExecContext(ctx, supplyChainImpactSupportsBackfillVerifyAdvanceSQL,
			SupplyChainImpactStableKeyFillMarker, bound[0], bound[1], bound[2], marker.sweeps); err != nil {
			return report, fmt.Errorf("advance supply chain impact stable key fill verification cursor: %w", err)
		}
	}
	return report, nil
}

// complete sets the marker after the clean last slice: one transaction drops the
// verification cursor, guarded by the sweep the step read (which also locks the marker
// row), then stamps completion. Zero rows from either statement rolls back: a
// restarted sweep keeps its own state.
func (f SupplyChainImpactStableKeyFill) complete(ctx context.Context, beginner db.Beginner, sweeps int64) (bool, error) {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin supply chain impact stable key fill completion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{supplyChainImpactSupportsBackfillVerifyClearSQL, []any{SupplyChainImpactStableKeyFillMarker, sweeps}},
		{supplyChainImpactStableKeyFillCompleteSQL, []any{SupplyChainImpactStableKeyFillMarker}},
	} {
		result, err := tx.ExecContext(ctx, stmt.query, stmt.args...)
		if err != nil {
			return false, fmt.Errorf("complete supply chain impact stable key fill: %w", err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("count rows of the stable key fill completion: %w", err)
		}
		if n == 0 {
			return false, nil
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit supply chain impact stable key fill completion: %w", err)
	}
	return true, nil
}
