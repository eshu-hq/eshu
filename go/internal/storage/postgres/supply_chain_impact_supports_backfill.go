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
)

const (
	// SupplyChainImpactSupportsBackfillMarker names the readiness row of the
	// supports backfill in supply_chain_impact_finding_supports_backfill. The
	// suffix is SupplyChainImpactSupportsContractVersion: a version bump is a new
	// marker, so the sweep runs again and a binary that finds a marker above its
	// own version reads the table as not ready.
	SupplyChainImpactSupportsBackfillMarker = "supply_chain_impact_finding_supports_v1"

	// supplyChainImpactSupportsBackfillDefaultChunk bounds the facts one backfill
	// transaction writes under one conflict-domain lock.
	supplyChainImpactSupportsBackfillDefaultChunk = 500
)

// supplyChainImpactSupportsBackfillEnsureMarkerSQL creates the marker row on the
// first step.
const supplyChainImpactSupportsBackfillEnsureMarkerSQL = `
INSERT INTO supply_chain_impact_finding_supports_backfill (marker_name, contract_version)
VALUES ($1, $2)
ON CONFLICT (marker_name) DO NOTHING`

// supplyChainImpactSupportsBackfillReadMarkerSQL reads the sweep cursor, the
// state, the sweep count, and the verification cursor with whether it belongs
// to the current sweep (migration 348).
const supplyChainImpactSupportsBackfillReadMarkerSQL = `
SELECT cursor_scope_id, cursor_generation_id, cursor_fact_id, completed_at IS NOT NULL, sweeps::bigint,
       COALESCE(verify_cursor_scope_id, ''), COALESCE(verify_cursor_generation_id, ''),
       COALESCE(verify_cursor_fact_id, ''), COALESCE(verify_sweep = sweeps, FALSE)
FROM supply_chain_impact_finding_supports_backfill
WHERE marker_name = $1`

// supplyChainImpactSupportsBackfillNextPairSQL finds the next (scope, generation)
// holding a live finding fact after the pair ($1, $2) in (scope, generation)
// order. The (scope_id, generation_id, fact_kind) index serves the ordered walk,
// so the sweep never sorts the table: the facts of one pair are ordered inside
// the chunk read, bounded by the pair.
const supplyChainImpactSupportsBackfillNextPairSQL = `
SELECT scope_id, generation_id
FROM fact_records
WHERE fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
  AND is_tombstone = FALSE
  AND (scope_id, generation_id) > ($1::text, $2::text)
ORDER BY scope_id, generation_id
LIMIT 1`

// supplyChainImpactSupportsBackfillChunkSQL lists up to $4 live finding fact ids
// of one (scope, generation) after fact id $3, in fact id order.
const supplyChainImpactSupportsBackfillChunkSQL = `
SELECT fact_id
FROM fact_records
WHERE scope_id = $1 AND generation_id = $2
  AND fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
  AND is_tombstone = FALSE
  AND fact_id > $3
ORDER BY fact_id
LIMIT $4`

// supplyChainImpactSupportsBackfillAdvanceSQL records the cursor.
const supplyChainImpactSupportsBackfillAdvanceSQL = `
UPDATE supply_chain_impact_finding_supports_backfill
SET cursor_scope_id = $2, cursor_generation_id = $3, cursor_fact_id = $4, updated_at = now()
WHERE marker_name = $1 AND completed_at IS NULL`

// supplyChainImpactSupportsBackfillRestartSQL moves the sweep cursor back to the
// start after a verification found a gap and drops the verification cursor. The
// sweeps bump alone already voids that cursor (it counts only while verify_sweep
// equals sweeps), which is what keeps an older binary's restart, which does not
// clear these columns, safe.
const supplyChainImpactSupportsBackfillRestartSQL = `
UPDATE supply_chain_impact_finding_supports_backfill
SET cursor_scope_id = '', cursor_generation_id = '', cursor_fact_id = '',
    verify_cursor_scope_id = NULL, verify_cursor_generation_id = NULL,
    verify_cursor_fact_id = NULL, verify_sweep = NULL,
    sweeps = sweeps + 1, updated_at = now()
WHERE marker_name = $1 AND completed_at IS NULL`

// supplyChainImpactSupportsBackfillCompleteSQL sets the readiness marker in one
// statement that rechecks the old-writer fence: when a scope is dirty the update
// touches no row and the marker stays unset until the next step drains it. The
// recheck is defense in depth: every step drains first, so a dirty fence reaches
// completion only through a race, and the readiness predicate's own fence check
// is the guard a reader relies on (a mutant without this recheck survives the live
// suite; the readiness mutants do not).
const supplyChainImpactSupportsBackfillCompleteSQL = `
UPDATE supply_chain_impact_finding_supports_backfill
SET completed_at = now(), updated_at = now()
WHERE marker_name = $1 AND completed_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM supply_chain_impact_finding_supports_dirty)`

// supplyChainImpactSupportsReadySQL reads the readiness predicate: the marker
// for this binary's version is complete, no marker for a higher version exists
// (a newer binary ran the sweep, so this binary's reads must refuse), and the
// old-writer fence is empty.
const supplyChainImpactSupportsReadySQL = `
SELECT EXISTS (
        SELECT 1 FROM supply_chain_impact_finding_supports_backfill
        WHERE marker_name = $1 AND completed_at IS NOT NULL AND contract_version = $2
    )
    AND NOT EXISTS (
        SELECT 1 FROM supply_chain_impact_finding_supports_backfill WHERE contract_version > $2
    )
    AND NOT EXISTS (SELECT 1 FROM supply_chain_impact_finding_supports_dirty)`

// SupplyChainImpactSupportsReady reports whether the supports backfill finished
// and verified at this contract version and the old-writer fence is empty. A reader that serves a grant-scoped
// call from the supports table must refuse the call while it is false. A
// missing marker table reads as an error, which the same reader treats as not
// ready (the table ships in the same migration as the supports table).
func SupplyChainImpactSupportsReady(ctx context.Context, queryer db.Queryer) (bool, error) {
	rows, err := queryer.QueryContext(ctx, supplyChainImpactSupportsReadySQL,
		SupplyChainImpactSupportsBackfillMarker, SupplyChainImpactSupportsContractVersion)
	if err != nil {
		return false, fmt.Errorf("read supply chain impact supports readiness: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ready bool
	if rows.Next() {
		if err := rows.Scan(&ready); err != nil {
			return false, fmt.Errorf("scan supply chain impact supports readiness: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read supply chain impact supports readiness: %w", err)
	}
	return ready, nil
}

// SupplyChainImpactSupportsBackfill fills supply_chain_impact_finding_supports
// for the finding facts written before the same-transaction maintainer shipped,
// in resumable steps, and sets the readiness marker only after a verification
// found no live finding fact without a support and no support of a tombstoned
// fact. The verification runs in bounded slices, one per step, and resumes from
// the cursor stored in the marker row (supply_chain_impact_supports_verify.go).
//
// Each step writes at most ChunkSize supports of one (scope, generation) in one
// transaction under that pair's conflict-domain advisory lock, the lock the
// impact writer holds around its fact and support writes and its retraction, so
// a step cannot interleave with a writer or resurrect a support a retraction
// removed. The statements are the maintainer's, so a backfilled support equals a
// maintained one. A step also removes the supports of tombstoned facts of the
// pair it enters, which a writer without the maintainer leaves behind. One
// reducer runs the sweep at a time (the caller holds a cluster-wide lock); the
// marker update is guarded so a second runner cannot complete it twice.
type SupplyChainImpactSupportsBackfill struct {
	// DB must support transactions (db.Beginner).
	DB db.ExecQueryer
	// ChunkSize bounds the facts one step writes; zero means the default (500).
	ChunkSize int
	// VerifySliceSize bounds the supports and the live findings one
	// verification slice reads; zero means the default (20,000).
	VerifySliceSize int
}

// SupplyChainImpactSupportsBackfillReport describes one RunStep.
type SupplyChainImpactSupportsBackfillReport struct {
	// Written counts supports the step inserted or changed; Unchanged counts the
	// ones it found already current.
	Written, Unchanged int64
	// Removed counts supports of tombstoned facts the step deleted.
	Removed int64
	// Verified is true when the step ran a verification slice.
	Verified bool
	// Missing is the slice's count of live facts without a support, at most
	// the slice size; nonzero restarts the sweep.
	Missing int64
	// VerifySlice is the outcome of the verification slice the step ran (one of
	// the telemetry.SupplyChainImpactSupportsVerifySlice* values), empty when the
	// step ran none.
	VerifySlice string
	// VerifySliceDuration is the wall time of that slice statement.
	VerifySliceDuration time.Duration
	// Drained counts old-writer dirty scopes the step re-read.
	Drained int
	// DirtyScopes is the old-writer fence's depth after the step, capped at 1000.
	DirtyScopes int64
	// Complete is true when the readiness marker is set.
	Complete bool
}

// RunStep performs one bounded unit of work: a chunk, a drain, or one
// verification slice. Call it until Complete is true.
func (b SupplyChainImpactSupportsBackfill) RunStep(ctx context.Context) (SupplyChainImpactSupportsBackfillReport, error) {
	var report SupplyChainImpactSupportsBackfillReport
	beginner, ok := b.DB.(db.Beginner)
	if !ok {
		return report, errors.New("supply chain impact supports backfill requires a transaction beginner")
	}
	chunk := b.ChunkSize
	if chunk <= 0 {
		chunk = supplyChainImpactSupportsBackfillDefaultChunk
	}
	if _, err := b.DB.ExecContext(ctx, supplyChainImpactSupportsBackfillEnsureMarkerSQL,
		SupplyChainImpactSupportsBackfillMarker, SupplyChainImpactSupportsContractVersion); err != nil {
		return report, fmt.Errorf("ensure supply chain impact supports backfill marker: %w", err)
	}
	marker, err := b.readMarker(ctx)
	if err != nil {
		return report, err
	}
	cursorScope, cursorGeneration, cursorFact, complete := marker.scope, marker.generation, marker.fact, marker.complete
	drained, err := b.drainOneDirtyScope(ctx, beginner, chunk, &report)
	if err != nil {
		return report, err
	}
	if report.DirtyScopes, err = b.dirtyScopes(ctx); err != nil {
		return report, err
	}
	if drained || complete {
		report.Complete = complete
		return report, nil
	}
	// Resume inside the cursor's pair; when it has nothing left, walk to the next
	// pair that holds a live finding fact. A pair another writer emptied between
	// the locate read and the locked chunk read is skipped, not recorded.
	scope, generation, after, entering := cursorScope, cursorGeneration, cursorFact, false
	if scope == "" {
		var found bool
		if scope, generation, found, err = b.nextPair(ctx, "", ""); err != nil {
			return report, err
		}
		if !found {
			return b.verify(ctx, beginner, marker, report)
		}
		after, entering = "", true
	}
	for {
		written, unchanged, removed, last, err := b.writeChunk(ctx, beginner, chunkRequest{
			scope: scope, generation: generation, after: after, limit: chunk, cleanOrphans: entering,
		})
		report.Written += written
		report.Unchanged += unchanged
		report.Removed += removed
		if err != nil || last != "" {
			return report, err
		}
		// The pair has no live finding fact after the cursor: it is done.
		next, nextGen, found, err := b.nextPair(ctx, scope, generation)
		if err != nil {
			return report, err
		}
		if !found {
			return b.verify(ctx, beginner, marker, report)
		}
		scope, generation, after, entering = next, nextGen, "", true
		if report.Removed > 0 || report.Written > 0 {
			// A step does one pair's work; the next step enters the next pair.
			return report, nil
		}
	}
}

func (b SupplyChainImpactSupportsBackfill) nextPair(ctx context.Context, scope, generation string) (nextScope, nextGeneration string, found bool, err error) {
	rows, err := b.DB.QueryContext(ctx, supplyChainImpactSupportsBackfillNextPairSQL, scope, generation)
	if err != nil {
		return "", "", false, fmt.Errorf("find the next scope and generation to back up: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", "", false, fmt.Errorf("find the next scope and generation to back up: %w", err)
		}
		return "", "", false, nil
	}
	if err := rows.Scan(&nextScope, &nextGeneration); err != nil {
		return "", "", false, fmt.Errorf("scan the next scope and generation to back up: %w", err)
	}
	return nextScope, nextGeneration, true, nil
}

// chunkRequest names one backfill chunk.
type chunkRequest struct {
	scope, generation string
	// after is the last fact id already backed up in this pair ("" when the
	// sweep just entered it).
	after string
	limit int
	// cleanOrphans removes the pair's supports of tombstoned facts first.
	cleanOrphans bool
	// skipCursor leaves the sweep cursor alone: a drain of a dirty scope is not
	// part of the sweep.
	skipCursor bool
}

// writeChunk backs up one chunk of the pair under its conflict-domain lock and
// returns the last fact id it covered; an empty last means the pair has no live
// finding fact after req.after (the orphan cleanup still ran when requested).
func (b SupplyChainImpactSupportsBackfill) writeChunk(ctx context.Context, beginner db.Beginner, req chunkRequest) (written, unchanged, removed int64, last string, err error) {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return 0, 0, 0, "", fmt.Errorf("begin supply chain impact supports backfill transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, supplychaincore.SupplyChainImpactConflictDomainLockQuery,
		supplychaincore.SupplyChainImpactConflictDomainKey(req.scope, req.generation)); err != nil {
		return 0, 0, 0, "", fmt.Errorf("lock supply chain impact conflict domain: %w", err)
	}
	if req.cleanOrphans {
		if removed, err = deleteRetractedSupports(ctx, tx, req.scope, req.generation); err != nil {
			return 0, 0, 0, "", err
		}
	}
	ids, err := queryBackfillFactIDs(ctx, tx, supplyChainImpactSupportsBackfillChunkSQL, req.scope, req.generation, req.after, req.limit)
	if err != nil {
		return 0, 0, 0, "", err
	}
	if len(ids) > 0 {
		last = ids[len(ids)-1]
		result, err := upsertSupports(ctx, tx, ids)
		if err != nil {
			return 0, 0, 0, "", err
		}
		written, unchanged = result.Inserted+result.Updated, result.Unchanged
		if !req.skipCursor {
			if _, err := tx.ExecContext(ctx, supplyChainImpactSupportsBackfillAdvanceSQL,
				SupplyChainImpactSupportsBackfillMarker, req.scope, req.generation, last); err != nil {
				return 0, 0, 0, "", fmt.Errorf("advance supply chain impact supports backfill cursor: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, 0, "", fmt.Errorf("commit supply chain impact supports backfill chunk: %w", err)
	}
	return written, unchanged, removed, last, nil
}

// queryBackfillFactIDs runs a statement that selects one fact_id column.
func queryBackfillFactIDs(ctx context.Context, tx db.Transaction, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list finding facts to back up: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan finding fact to back up: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list finding facts to back up: %w", err)
	}
	return ids, nil
}

// upsertSupports runs the maintainer's statement over a transaction.
func upsertSupports(ctx context.Context, tx db.Transaction, factIDs []string) (supplychaincore.SupplyChainImpactSupportsWrite, error) {
	return (&supplyChainImpactTx{tx: tx}).UpsertImpactSupports(ctx, factIDs)
}

// deleteRetractedSupports runs the maintainer's retraction delete over a transaction.
func deleteRetractedSupports(ctx context.Context, tx db.Transaction, scopeID, generationID string) (int64, error) {
	return (&supplyChainImpactTx{tx: tx}).DeleteRetractedImpactSupports(ctx, scopeID, generationID)
}
