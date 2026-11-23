// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The commit-skew fence (eshu-hq/eshu#10055) retracts one active go-reachability
// generation with no successor, and tells the scopes holding findings for that
// repository to re-derive. Both updates name the generation the page read, so a
// successor activated after that read makes the fence a no-op.
//
// Lock order, in one transaction: the derived scope's ingestion_scopes row, then the
// fenced generation's scope_generations row, then each holder's fact_work_items rows,
// holders in scope_id order. Every blocking path takes the scope row first (#7389, see
// ProjectorQueue.Ack): the ack, the failure path, the ingestion commit, and the
// repository-retirement fence. A fence that took the generation first could deadlock
// against an ack holding the scope row and wanting that generation. The fence never
// takes a holder's scope row, a repository scope row, or any row of the successor's
// work.
//
// Against an ack of a successor, both orders end on the successor:
//   - The ack commits first. The pointer predicate rechecks against the new pointer
//     under READ COMMITTED, matches nothing, and the fence stops holding nothing.
//   - The fence commits first. The ack finds the old generation already superseded,
//     activates the successor, and sets the pointer.
//
// The pointer change fires ingestion_scopes_winners_dirty_capture, which logs the
// retracted generation for the keyed winners tick, which re-picks among stored findings and
// re-derives nothing. Each holder must re-derive its own copy, so holders are reopened here.
// No pass reads Go evidence across scopes until eshu-hq/eshu#10095 lands, so today the reopen
// changes no verdict. It is here so the verdict cannot go stale once that loader merges.
const (
	// fenceTimeoutsQuery bounds the waits, so a scope or work item held by another
	// transaction cannot stall the reconcile loop, and a regressed holder read cannot
	// hold the fence's locks. Both values are transaction-local. The lock value matches
	// the projector ack's default.
	fenceTimeoutsQuery = `SELECT set_config('lock_timeout', '2000ms', true), set_config('statement_timeout', '10000ms', true)`
	// fenceFindHoldersQuery finds the scopes holding an active finding support for
	// the repository (#11197), on supply_chain_impact_finding_supports_repository_idx.
	// The table holds live finding facts only, so no tombstone predicate applies, and
	// the empty repository id it stores for a finding without one never matches a
	// repository id. Holders are found by repository, not by CVE, so no osv-to-CVE
	// alias mapping is needed here. Over-reopening is safe: a pass re-derives its own
	// findings from active supports.
	//
	// The holders come back in the lock order of their supply_chain_impact work item at
	// the active generation, work_item_id COLLATE "C" (#11147). The fence reopens every
	// holder in this one transaction, and a reopen now locks a holder's dead-letter row,
	// which the admin replay, the recovery drain, and poison recovery lock in that same
	// order. Scope-id order is not the same order: the work item id replaces ':' and '/'
	// with '_'. A holder with no item takes no row lock (it is enqueued), so it sorts last.
	fenceFindHoldersQuery = `SELECT holder.scope_id
		FROM (
		    SELECT DISTINCT support.scope_id, scope.active_generation_id
		    FROM supply_chain_impact_finding_supports AS support
		    JOIN ingestion_scopes AS scope
		      ON scope.scope_id = support.scope_id
		     AND scope.active_generation_id = support.generation_id
		    WHERE support.repository_id = $1
		      AND support.repository_id <> ''
		      AND support.scope_id <> $2
		) AS holder
		LEFT JOIN LATERAL (
		    SELECT min(work.work_item_id COLLATE "C") AS lock_key
		    FROM fact_work_items AS work
		    WHERE work.scope_id = holder.scope_id
		      AND work.generation_id = holder.active_generation_id
		      AND work.stage = 'reducer'
		      AND work.domain = 'supply_chain_impact'
		) AS item ON TRUE
		ORDER BY item.lock_key COLLATE "C" NULLS LAST, holder.scope_id COLLATE "C"`
	// fenceDetachPointerQuery takes the scope row, conditional on the generation read.
	fenceDetachPointerQuery = `UPDATE ingestion_scopes SET active_generation_id = NULL
		WHERE scope_id = $1 AND active_generation_id = $2`
	// fenceSupersedeGeneration writes the terminal status the ack's status predicate
	// refuses to revive. It runs only after the pointer update hit a row.
	fenceSupersedeGeneration = `UPDATE scope_generations SET status = 'superseded', superseded_at = $3
		WHERE scope_id = $1 AND generation_id = $2 AND status = 'active'`
)

// SupportsReadiness checks, on the fence's own transaction, that the supports table
// the holder read answers from is ready, and returns an error when it is not
// (#11197). The composition root binds it to postgres.RequireSupplyChainImpactSupportsReady;
// it is a function so this leaf package does not import the postgres root. The fence
// runs at READ COMMITTED, so the check and the holder read are not one snapshot: an
// old-writer image marking a scope dirty between them could let a fence retract with
// a holder missed. The fence therefore runs the check twice, before the detach and
// again after the holder read, and rolls back when either says not ready. A dirty
// mark commits atomically with the old writer's finding, so a finding the holder read
// missed is seen by the second check unless it commits after that check's own
// snapshot, a window of the reopen statements only. A committed fence does not re-run
// for that generation, so that last window is stated, not closed.
type SupportsReadiness func(ctx context.Context, tx db.Queryer) error

// HolderReopener reopens one holder scope's supply_chain_impact work on the caller's
// transaction, stamped with the generation that changed. The composition root binds it
// to postgres.ReopenSupplyChainImpactHolderInTransaction; it is a function so this leaf
// package does not import the postgres root.
type HolderReopener func(
	ctx context.Context,
	tx db.ExecQueryer,
	holderScopeID, sourceScopeID, sourceGenerationID string,
	now time.Time,
) error

// WithHolderReopener returns the store with the holder reopen the fence needs. It
// leaves the receiver unchanged.
func (s Store) WithHolderReopener(reopen HolderReopener) Store {
	s.reopenHolder = reopen
	return s
}

// WithSupportsReadiness returns the store with the supports readiness check the
// fence needs. It leaves the receiver unchanged.
func (s Store) WithSupportsReadiness(check SupportsReadiness) Store {
	s.supportsReady = check
	return s
}

// FenceStaleGoEvidence retracts the named generations and reopens their holders. The
// result is valid alongside an error. Facts stay in place: the scope's retirement purges
// them, and a later committed claim activates a successor. observedAt is the coordinator
// tick that read the page, so the stamps in one pass agree.
//
// Each fence is its own transaction, so one busy scope holds no other scope's locks and
// no two fences can wait on each other. It stops at the first error, which bounds a pass
// to one lock wait. The caller retries the page, where a retracted generation is no
// longer a candidate.
func (s Store) FenceStaleGoEvidence(
	ctx context.Context,
	fences []scannercomponent.StaleEvidenceFence,
	observedAt time.Time,
) (scannercomponent.StaleEvidenceFenceResult, error) {
	var result scannercomponent.StaleEvidenceFenceResult
	if len(fences) == 0 {
		return result, nil
	}
	if s.database == nil {
		return result, errors.New("go module repository store database is required")
	}
	beginner, ok := s.database.(db.Beginner)
	if !ok {
		return result, errors.New("go module repository store database must support transactions to fence evidence")
	}
	if s.reopenHolder == nil {
		return result, errors.New("go module repository store needs a holder reopener to fence evidence")
	}
	if s.supportsReady == nil {
		return result, errors.New("go module repository store needs a supports readiness check to fence evidence")
	}
	for _, fence := range fences {
		retracted, holders, err := s.fenceOne(ctx, beginner, fence, observedAt)
		if err != nil {
			return result, fmt.Errorf("fence stale go evidence: %w", err)
		}
		if retracted {
			result.Fenced++
			result.HoldersReopened += holders
		}
	}
	return result, nil
}

// fenceOne runs one fence in one transaction, reporting whether it retracted the
// generation and how many holders it reopened. A pointer that no longer names the
// generation commits nothing. A pointer naming a generation that is not active is a
// state error: it rolls back, and the error names the fault, not the repository.
func (s Store) fenceOne(
	ctx context.Context,
	beginner db.Beginner,
	fence scannercomponent.StaleEvidenceFence,
	observedAt time.Time,
) (bool, int, error) {
	repositoryID, ok := scannercomponent.ParseGoReachabilityScopeID(fence.ScopeID)
	if !ok {
		return false, 0, errors.New("fence scope is not a canonical go reachability scope")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return false, 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, fenceTimeoutsQuery); err != nil {
		return false, 0, fmt.Errorf("set timeouts: %w", err)
	}
	// The holder read answers from the supports table, so a table that is not ready
	// must stop the fence before it detaches anything: the transaction rolls back
	// with nothing retracted, and the coordinator retries the page (#11197). A fence
	// that retracted without reopening its holders would leave them stale.
	if err := s.supportsReady(ctx, tx); err != nil {
		return false, 0, fmt.Errorf("check supports readiness: %w", err)
	}
	affected, err := execAffected(ctx, tx, fenceDetachPointerQuery, fence.ScopeID, fence.GenerationID)
	if err != nil {
		return false, 0, fmt.Errorf("detach active generation: %w", err)
	}
	if affected == 0 {
		return false, 0, nil
	}
	affected, err = execAffected(ctx, tx, fenceSupersedeGeneration, fence.ScopeID, fence.GenerationID, observedAt)
	if err != nil {
		return false, 0, fmt.Errorf("supersede generation: %w", err)
	}
	if affected == 0 {
		return false, 0, errors.New("active pointer named a generation that is not active")
	}
	holders, err := readHolderScopes(ctx, tx, repositoryID, fence.ScopeID)
	if err != nil {
		return false, 0, fmt.Errorf("read holders: %w", err)
	}
	// The holder read above is a separate READ COMMITTED snapshot from the first
	// check, so check again before any reopen or the commit: a table that went not
	// ready in between rolls the whole fence back and the coordinator retries it.
	if err := s.supportsReady(ctx, tx); err != nil {
		return false, 0, fmt.Errorf("recheck supports readiness after the holder read: %w", err)
	}
	for _, holder := range holders {
		if err := s.reopenHolder(ctx, tx, holder, fence.ScopeID, fence.GenerationID, observedAt); err != nil {
			return false, 0, fmt.Errorf("reopen holder: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, 0, fmt.Errorf("commit: %w", err)
	}
	return true, len(holders), nil
}

func execAffected(ctx context.Context, tx db.Transaction, query string, args ...any) (int64, error) {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// readHolderScopes reads the holders and closes the rows before returning, because the
// reopen runs on the same transaction.
func readHolderScopes(ctx context.Context, tx db.Transaction, repositoryID, scopeID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, fenceFindHoldersQuery, repositoryID, scopeID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var holders []string
	for rows.Next() {
		var holder string
		if err := rows.Scan(&holder); err != nil {
			return nil, err
		}
		holders = append(holders, holder)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return holders, nil
}
