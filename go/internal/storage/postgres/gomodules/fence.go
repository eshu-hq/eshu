// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/facts"
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
// retracted generation for the keyed winners tick. That tick only re-picks among stored
// findings, which is why the holders are reopened here.
const (
	// fenceTimeoutsQuery bounds the waits, so a scope or work item held by another
	// transaction cannot stall the reconcile loop, and a regressed holder read cannot
	// hold the fence's locks. Both values are transaction-local. The lock value matches
	// the projector ack's default.
	fenceTimeoutsQuery = `SELECT set_config('lock_timeout', '2000ms', true), set_config('statement_timeout', '10000ms', true)`
	// fenceDetachPointerQuery takes the scope row, conditional on the generation read.
	fenceDetachPointerQuery = `UPDATE ingestion_scopes SET active_generation_id = NULL
		WHERE scope_id = $1 AND active_generation_id = $2`
	// fenceSupersedeGeneration writes the terminal status the ack's status predicate
	// refuses to revive. It runs only after the pointer update hit a row.
	fenceSupersedeGeneration = `UPDATE scope_generations SET status = 'superseded', superseded_at = $3
		WHERE scope_id = $1 AND generation_id = $2 AND status = 'active'`
	// fenceFindHoldersQuery finds the scopes holding an active finding for the
	// repository, on fact_records_supply_chain_impact_repository_lookup_idx. The fact
	// kind is a literal because a bound parameter would not prove the partial-index
	// predicate under a generic plan. Holders are found by repository, not by CVE, so no
	// osv-to-CVE alias mapping is needed here. Over-reopening is safe: a pass re-derives
	// its own findings from active facts.
	fenceFindHoldersQuery = `SELECT DISTINCT finding.scope_id
		FROM fact_records AS finding
		JOIN ingestion_scopes AS scope
		  ON scope.scope_id = finding.scope_id
		 AND scope.active_generation_id = finding.generation_id
		WHERE finding.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
		  AND finding.is_tombstone = FALSE
		  AND finding.payload->>'repository_id' = $1
		  AND finding.scope_id <> $2
		ORDER BY finding.scope_id`
)

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
