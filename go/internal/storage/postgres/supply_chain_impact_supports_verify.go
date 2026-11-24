// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// supplyChainImpactSupportsBackfillDefaultVerifySlice bounds the supports and
// the live findings one verification slice reads. On QA (11.6M live findings)
// the shipped statement took 4.6 to 6.0 s cold for a 20,000-row slice starting at
// a pair boundary, and 20.3 s for one starting inside the largest pair, all on
// index range scans (docs/internal/evidence/10880-verify-ranged/README.md).
const supplyChainImpactSupportsBackfillDefaultVerifySlice = 20000

// supplyChainImpactSupportsBackfillVerifySliceSQL checks one bounded key
// interval of the verification in one snapshot. Keys are (scope_id,
// generation_id, fact_id), ordered as the active-finding index orders them, and
// the interval starts after the cursor ($1, $2, $3).
//
// It reads up to $4 supports after the cursor (the (scope_id, generation_id)
// index plus an incremental sort of each pair's fact ids) and up to $4 live
// findings after it (an index-only range scan of the partial
// (scope_id, generation_id, fact_id) index). The interval ends at the smaller
// last key of a side that came back full; when neither side is full it runs to
// the end of the key space and the slice is the last. Both sides then hold every
// row of the interval, so consecutive slices tile the key space with nothing
// between them.
//
// In the interval it counts the live findings with no support of the same
// fact_id (the predicate of the unsliced count it replaces) and finds the first
// support whose fact is tombstoned (the predicate of the unsliced orphan probe):
// such a support has no live finding of its key, so the probe of fact_records
// runs only for those unmatched supports, never once per support. The bound
// columns are empty on the last slice.
const supplyChainImpactSupportsBackfillVerifySliceSQL = `
WITH supports_slice AS MATERIALIZED (
    SELECT s.scope_id, s.generation_id, s.fact_id
    FROM supply_chain_impact_finding_supports AS s
    WHERE (s.scope_id, s.generation_id) >= ($1::text, $2::text)
      AND (s.scope_id, s.generation_id, s.fact_id) > ($1::text, $2::text, $3::text)
    ORDER BY s.scope_id, s.generation_id, s.fact_id
    LIMIT $4::bigint
), live_slice AS MATERIALIZED (
    SELECT f.scope_id, f.generation_id, f.fact_id
    FROM fact_records AS f
    WHERE f.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
      AND f.is_tombstone = FALSE
      AND (f.scope_id, f.generation_id, f.fact_id) > ($1::text, $2::text, $3::text)
    ORDER BY f.scope_id, f.generation_id, f.fact_id
    LIMIT $4::bigint
), ends AS (
    SELECT e.scope_id, e.generation_id, e.fact_id
    FROM (SELECT scope_id, generation_id, fact_id FROM supports_slice
          ORDER BY scope_id DESC, generation_id DESC, fact_id DESC LIMIT 1) AS e
    WHERE (SELECT count(*) FROM supports_slice) = $4::bigint
    UNION ALL
    SELECT e.scope_id, e.generation_id, e.fact_id
    FROM (SELECT scope_id, generation_id, fact_id FROM live_slice
          ORDER BY scope_id DESC, generation_id DESC, fact_id DESC LIMIT 1) AS e
    WHERE (SELECT count(*) FROM live_slice) = $4::bigint
), bound AS (
    SELECT scope_id, generation_id, fact_id FROM ends
    ORDER BY scope_id, generation_id, fact_id
    LIMIT 1
), supports_in AS MATERIALIZED (
    SELECT s.scope_id, s.generation_id, s.fact_id FROM supports_slice AS s
    WHERE NOT EXISTS (SELECT 1 FROM bound)
       OR (s.scope_id, s.generation_id, s.fact_id) <= (SELECT scope_id, generation_id, fact_id FROM bound)
), live_in AS MATERIALIZED (
    SELECT l.scope_id, l.generation_id, l.fact_id FROM live_slice AS l
    WHERE NOT EXISTS (SELECT 1 FROM bound)
       OR (l.scope_id, l.generation_id, l.fact_id) <= (SELECT scope_id, generation_id, fact_id FROM bound)
), unmatched AS MATERIALIZED (
    SELECT s.scope_id, s.generation_id, s.fact_id FROM supports_in AS s
    WHERE NOT EXISTS (
        SELECT 1 FROM live_in AS l
        WHERE l.scope_id = s.scope_id AND l.generation_id = s.generation_id AND l.fact_id = s.fact_id)
), orphan AS (
    SELECT u.scope_id, u.generation_id
    FROM unmatched AS u
    JOIN fact_records AS f ON f.fact_id = u.fact_id
    WHERE f.is_tombstone = TRUE
    ORDER BY u.scope_id, u.generation_id, u.fact_id
    LIMIT 1
)
SELECT (SELECT count(*) FROM supports_in),
       (SELECT count(*) FROM live_in),
       (SELECT count(*) FROM live_in AS l
        WHERE NOT EXISTS (SELECT 1 FROM supply_chain_impact_finding_supports AS s WHERE s.fact_id = l.fact_id)),
       EXISTS (SELECT 1 FROM orphan),
       COALESCE((SELECT scope_id FROM orphan), ''),
       COALESCE((SELECT generation_id FROM orphan), ''),
       NOT EXISTS (SELECT 1 FROM bound),
       COALESCE((SELECT scope_id FROM bound), ''),
       COALESCE((SELECT generation_id FROM bound), ''),
       COALESCE((SELECT fact_id FROM bound), '')`

// supplyChainImpactSupportsBackfillVerifyAdvanceSQL records the end of a clean
// slice as the verification cursor. It is guarded by the sweep the step read: a
// restart in between (another runner, or an older binary) leaves the row alone.
const supplyChainImpactSupportsBackfillVerifyAdvanceSQL = `
UPDATE supply_chain_impact_finding_supports_backfill
SET verify_cursor_scope_id = $2, verify_cursor_generation_id = $3, verify_cursor_fact_id = $4,
    verify_sweep = sweeps, updated_at = now()
WHERE marker_name = $1 AND completed_at IS NULL AND sweeps = $5`

// supplyChainImpactSupportsBackfillVerifyClearSQL drops the verification cursor
// in the completion transaction, before the completion statement, and locks the
// marker row. It is guarded by the sweep the step read: zero rows means another
// runner restarted the sweep (or set the marker), and the step does not complete.
// Clearing it means a marker whose completed_at is later reset without a sweep
// bump verifies from the start of the key space, never from a stale cursor. The
// completion statement itself stays as pinned by the supports contract.
const supplyChainImpactSupportsBackfillVerifyClearSQL = `
UPDATE supply_chain_impact_finding_supports_backfill
SET verify_cursor_scope_id = NULL, verify_cursor_generation_id = NULL,
    verify_cursor_fact_id = NULL, verify_sweep = NULL, updated_at = now()
WHERE marker_name = $1 AND completed_at IS NULL AND sweeps = $2`

// supportsBackfillMarker is the marker row one step acts on.
type supportsBackfillMarker struct {
	scope, generation, fact string
	complete                bool
	sweeps                  int64
	// verify is the verification cursor; it is the start of the key space
	// unless the stored cursor belongs to the current sweep.
	verify [3]string
}

func (b SupplyChainImpactSupportsBackfill) readMarker(ctx context.Context) (supportsBackfillMarker, error) {
	var m supportsBackfillMarker
	rows, err := b.DB.QueryContext(ctx, supplyChainImpactSupportsBackfillReadMarkerSQL, SupplyChainImpactSupportsBackfillMarker)
	if err != nil {
		return m, fmt.Errorf("read supply chain impact supports backfill marker: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return m, fmt.Errorf("read supply chain impact supports backfill marker: %w", err)
		}
		return m, errors.New("supply chain impact supports backfill marker row is missing")
	}
	var verifyCurrent bool
	if err := rows.Scan(&m.scope, &m.generation, &m.fact, &m.complete, &m.sweeps,
		&m.verify[0], &m.verify[1], &m.verify[2], &verifyCurrent); err != nil {
		return m, fmt.Errorf("scan supply chain impact supports backfill marker: %w", err)
	}
	if !verifyCurrent {
		m.verify = [3]string{}
	}
	return m, nil
}

// verifySliceResult is one answer of the verification slice statement.
type verifySliceResult struct {
	supports, live, missing       int64
	orphanFound                   bool
	orphanScope, orphanGeneration string
	last                          bool
	bound                         [3]string
}

// verify runs one verification slice when the sweep has passed the last
// finding fact. A failed step reports the slice as failed with its duration. A
// live finding without a support restarts the sweep; a support of a tombstoned
// fact has that pair's orphans removed under its conflict-domain lock and the
// cursor stays, so the slice runs again; a clean slice moves the cursor to its
// end; and the clean last slice runs the completion statement,
// whose fence recheck keeps the marker unset while an old writer's scope waits.
func (b SupplyChainImpactSupportsBackfill) verify(
	ctx context.Context,
	beginner db.Beginner,
	marker supportsBackfillMarker,
	report SupplyChainImpactSupportsBackfillReport,
) (out SupplyChainImpactSupportsBackfillReport, err error) {
	// Any failure after the slice started is a failed slice, so its duration
	// (a statement timeout among them) still reaches the slice histogram.
	defer func() {
		if err != nil {
			out.VerifySlice = telemetry.SupplyChainImpactSupportsVerifySliceFailed
		}
	}()
	size := b.VerifySliceSize
	if size <= 0 {
		size = supplyChainImpactSupportsBackfillDefaultVerifySlice
	}
	started := time.Now()
	slice, err := b.verifySlice(ctx, marker.verify, size)
	report.VerifySliceDuration = time.Since(started)
	if err != nil {
		return report, err
	}
	report.Verified = true
	report.Missing = slice.missing
	switch {
	case slice.missing > 0:
		report.VerifySlice = telemetry.SupplyChainImpactSupportsVerifySliceGap
		if _, err := b.DB.ExecContext(ctx, supplyChainImpactSupportsBackfillRestartSQL, SupplyChainImpactSupportsBackfillMarker); err != nil {
			return report, fmt.Errorf("restart supply chain impact supports backfill: %w", err)
		}
		return report, nil
	case slice.orphanFound:
		report.VerifySlice = telemetry.SupplyChainImpactSupportsVerifySliceOrphan
		removed, err := b.removeOrphans(ctx, beginner, slice.orphanScope, slice.orphanGeneration)
		report.Removed += removed
		return report, err
	case slice.last:
		report.VerifySlice = telemetry.SupplyChainImpactSupportsVerifySliceLast
		complete, err := b.complete(ctx, beginner, marker.sweeps)
		report.Complete = complete
		return report, err
	default:
		report.VerifySlice = telemetry.SupplyChainImpactSupportsVerifySliceAdvance
		if _, err := b.DB.ExecContext(ctx, supplyChainImpactSupportsBackfillVerifyAdvanceSQL,
			SupplyChainImpactSupportsBackfillMarker, slice.bound[0], slice.bound[1], slice.bound[2], marker.sweeps); err != nil {
			return report, fmt.Errorf("advance supply chain impact supports verification cursor: %w", err)
		}
		return report, nil
	}
}

// complete sets the readiness marker after the clean last slice. One
// transaction first drops the verification cursor, guarded by the sweep the step
// read (which also locks the marker row), then runs the pinned completion
// statement with its old-writer fence recheck. Zero rows from either statement
// rolls the transaction back: a restarted sweep keeps its own state, and a dirty
// fence keeps the cursor so the next step drains it and reruns this last slice.
func (b SupplyChainImpactSupportsBackfill) complete(ctx context.Context, beginner db.Beginner, sweeps int64) (bool, error) {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin supply chain impact supports completion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []struct {
		query string
		args  []any
		what  string
	}{
		{supplyChainImpactSupportsBackfillVerifyClearSQL, []any{SupplyChainImpactSupportsBackfillMarker, sweeps}, "clear supply chain impact supports verification cursor"},
		{supplyChainImpactSupportsBackfillCompleteSQL, []any{SupplyChainImpactSupportsBackfillMarker}, "complete supply chain impact supports backfill"},
	} {
		result, err := tx.ExecContext(ctx, stmt.query, stmt.args...)
		if err != nil {
			return false, fmt.Errorf("%s: %w", stmt.what, err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("count rows of %s: %w", stmt.what, err)
		}
		if n == 0 {
			return false, nil
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit supply chain impact supports completion: %w", err)
	}
	return true, nil
}

func (b SupplyChainImpactSupportsBackfill) verifySlice(ctx context.Context, cursor [3]string, size int) (verifySliceResult, error) {
	var r verifySliceResult
	rows, err := b.DB.QueryContext(ctx, supplyChainImpactSupportsBackfillVerifySliceSQL, cursor[0], cursor[1], cursor[2], size)
	if err != nil {
		return r, fmt.Errorf("verify supply chain impact supports slice: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return r, fmt.Errorf("verify supply chain impact supports slice: %w", err)
		}
		return r, errors.New("verify supply chain impact supports slice: no row")
	}
	if err := rows.Scan(&r.supports, &r.live, &r.missing, &r.orphanFound, &r.orphanScope, &r.orphanGeneration,
		&r.last, &r.bound[0], &r.bound[1], &r.bound[2]); err != nil {
		return r, fmt.Errorf("scan supply chain impact supports slice: %w", err)
	}
	return r, rows.Err()
}

// removeOrphans deletes the supports of tombstoned facts of one pair under the
// pair's conflict-domain lock, the lock the impact writer holds around its
// retraction.
func (b SupplyChainImpactSupportsBackfill) removeOrphans(ctx context.Context, beginner db.Beginner, scope, generation string) (int64, error) {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin supply chain impact supports orphan cleanup: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, supplychaincore.SupplyChainImpactConflictDomainLockQuery,
		supplychaincore.SupplyChainImpactConflictDomainKey(scope, generation)); err != nil {
		return 0, fmt.Errorf("lock supply chain impact conflict domain: %w", err)
	}
	removed, err := deleteRetractedSupports(ctx, tx, scope, generation)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit supply chain impact supports orphan cleanup: %w", err)
	}
	return removed, nil
}
