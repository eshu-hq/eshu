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

// supplyChainImpactSupportsBackfillReadMarkerSQL reads the cursor and the state.
const supplyChainImpactSupportsBackfillReadMarkerSQL = `
SELECT cursor_scope_id, cursor_generation_id, cursor_fact_id, completed_at IS NOT NULL
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

// supplyChainImpactSupportsBackfillOrphanPairSQL finds one (scope, generation)
// holding a support whose fact a retraction tombstoned, which only a writer
// without the maintainer leaves behind.
const supplyChainImpactSupportsBackfillOrphanPairSQL = `
SELECT s.scope_id, s.generation_id
FROM supply_chain_impact_finding_supports AS s
JOIN fact_records AS f ON f.fact_id = s.fact_id
WHERE f.is_tombstone = TRUE
LIMIT 1`

// supplyChainImpactSupportsBackfillMissingSQL counts, up to 1000, live finding
// facts with no support. The cap keeps the verification's report bounded; any
// count above zero restarts the sweep.
const supplyChainImpactSupportsBackfillMissingSQL = `
SELECT count(*) FROM (
    SELECT 1
    FROM fact_records AS f
    WHERE f.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
      AND f.is_tombstone = FALSE
      AND NOT EXISTS (SELECT 1 FROM supply_chain_impact_finding_supports AS s WHERE s.fact_id = f.fact_id)
    LIMIT 1000
) AS missing`

// supplyChainImpactSupportsBackfillRestartSQL moves the cursor back to the start
// after a verification found a gap.
const supplyChainImpactSupportsBackfillRestartSQL = `
UPDATE supply_chain_impact_finding_supports_backfill
SET cursor_scope_id = '', cursor_generation_id = '', cursor_fact_id = '',
    sweeps = sweeps + 1, updated_at = now()
WHERE marker_name = $1 AND completed_at IS NULL`

// supplyChainImpactSupportsBackfillCompleteSQL sets the readiness marker in one
// statement that rechecks the old-writer fence: when a scope is dirty the update
// touches no row and the marker stays unset until the next step drains it.
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
// found no live finding fact without a support.
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
}

// SupplyChainImpactSupportsBackfillReport describes one RunStep.
type SupplyChainImpactSupportsBackfillReport struct {
	// Written counts supports the step inserted or changed; Unchanged counts the
	// ones it found already current.
	Written, Unchanged int64
	// Removed counts supports of tombstoned facts the step deleted.
	Removed int64
	// Verified is true when the step ran the verification pass.
	Verified bool
	// Missing is the verification's count of live facts without a support,
	// capped at 1000; nonzero restarts the sweep.
	Missing int64
	// Drained counts old-writer dirty scopes the step re-read.
	Drained int
	// DirtyScopes is the old-writer fence's depth after the step, capped at 1000.
	DirtyScopes int64
	// Complete is true when the readiness marker is set.
	Complete bool
}

// RunStep performs one bounded unit of work: a chunk, an orphan cleanup, or the
// verification. Call it until Complete is true.
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
	cursorScope, cursorGeneration, cursorFact, complete, err := b.readMarker(ctx)
	if err != nil {
		return report, err
	}
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
			return b.verify(ctx, beginner, report)
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
			return b.verify(ctx, beginner, report)
		}
		scope, generation, after, entering = next, nextGen, "", true
		if report.Removed > 0 || report.Written > 0 {
			// A step does one pair's work; the next step enters the next pair.
			return report, nil
		}
	}
}

func (b SupplyChainImpactSupportsBackfill) readMarker(ctx context.Context) (scope, generation, fact string, complete bool, err error) {
	rows, err := b.DB.QueryContext(ctx, supplyChainImpactSupportsBackfillReadMarkerSQL, SupplyChainImpactSupportsBackfillMarker)
	if err != nil {
		return "", "", "", false, fmt.Errorf("read supply chain impact supports backfill marker: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", "", "", false, fmt.Errorf("read supply chain impact supports backfill marker: %w", err)
		}
		return "", "", "", false, errors.New("supply chain impact supports backfill marker row is missing")
	}
	if err := rows.Scan(&scope, &generation, &fact, &complete); err != nil {
		return "", "", "", false, fmt.Errorf("scan supply chain impact supports backfill marker: %w", err)
	}
	return scope, generation, fact, complete, nil
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

// verify runs when the sweep passed the last finding fact. It first cleans one
// pair of orphaned supports, then counts live facts without a support: a gap
// restarts the sweep, none sets the readiness marker.
func (b SupplyChainImpactSupportsBackfill) verify(ctx context.Context, beginner db.Beginner, report SupplyChainImpactSupportsBackfillReport) (SupplyChainImpactSupportsBackfillReport, error) {
	scope, generation, found, err := b.orphanPair(ctx)
	if err != nil {
		return report, err
	}
	if found {
		tx, err := beginner.Begin(ctx)
		if err != nil {
			return report, fmt.Errorf("begin supply chain impact supports orphan cleanup: %w", err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, supplychaincore.SupplyChainImpactConflictDomainLockQuery,
			supplychaincore.SupplyChainImpactConflictDomainKey(scope, generation)); err != nil {
			return report, fmt.Errorf("lock supply chain impact conflict domain: %w", err)
		}
		if report.Removed, err = deleteRetractedSupports(ctx, tx, scope, generation); err != nil {
			return report, err
		}
		if err := tx.Commit(); err != nil {
			return report, fmt.Errorf("commit supply chain impact supports orphan cleanup: %w", err)
		}
		return report, nil
	}
	report.Verified = true
	rows, err := b.DB.QueryContext(ctx, supplyChainImpactSupportsBackfillMissingSQL)
	if err != nil {
		return report, fmt.Errorf("verify supply chain impact supports: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		if err := rows.Scan(&report.Missing); err != nil {
			return report, fmt.Errorf("scan supply chain impact supports verification: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return report, fmt.Errorf("verify supply chain impact supports: %w", err)
	}
	_ = rows.Close()
	if report.Missing > 0 {
		if _, err := b.DB.ExecContext(ctx, supplyChainImpactSupportsBackfillRestartSQL, SupplyChainImpactSupportsBackfillMarker); err != nil {
			return report, fmt.Errorf("restart supply chain impact supports backfill: %w", err)
		}
		return report, nil
	}
	result, err := b.DB.ExecContext(ctx, supplyChainImpactSupportsBackfillCompleteSQL, SupplyChainImpactSupportsBackfillMarker)
	if err != nil {
		return report, fmt.Errorf("complete supply chain impact supports backfill: %w", err)
	}
	// Zero rows means the fence was not empty at the recheck (or another runner
	// set the marker): the next step drains it, then completes.
	if marked, err := result.RowsAffected(); err != nil {
		return report, fmt.Errorf("count supply chain impact supports marker update: %w", err)
	} else if marked > 0 {
		report.Complete = true
	}
	return report, nil
}

func (b SupplyChainImpactSupportsBackfill) orphanPair(ctx context.Context) (scope, generation string, found bool, err error) {
	rows, err := b.DB.QueryContext(ctx, supplyChainImpactSupportsBackfillOrphanPairSQL)
	if err != nil {
		return "", "", false, fmt.Errorf("find orphaned supply chain impact supports: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", "", false, fmt.Errorf("find orphaned supply chain impact supports: %w", err)
		}
		return "", "", false, nil
	}
	if err := rows.Scan(&scope, &generation); err != nil {
		return "", "", false, fmt.Errorf("scan orphaned supply chain impact support: %w", err)
	}
	return scope, generation, true, nil
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
