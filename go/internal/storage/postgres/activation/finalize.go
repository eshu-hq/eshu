// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Finalize settles one claimed obligation in a single transaction. It never
// publishes a phase, never runs maintenance and never releases a reducer
// claim; it only reads the exact generation's phase, wakes the rows that
// wait for it, and completes or retires the obligation under the caller's
// lease fence.
//
// Lock order matches ProjectorQueue.Ack: the scope row first, then the
// obligation row. The wake skips rows other transactions hold, so it never
// waits on reducer work.
//
// A wake commits only while the caller still owns the lease on the database
// clock; when the lease expired mid-transaction Finalize rolls back and
// returns ErrLeaseLost, and the next owner repeats the wake.
func (s Store) Finalize(ctx context.Context, work Obligation) (FinalizeResult, error) {
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	commit := func(result FinalizeResult) (FinalizeResult, error) {
		if err := tx.Commit(); err != nil {
			return FinalizeResult{}, fmt.Errorf("finalize activation obligation: commit: %w", err)
		}
		committed = true
		return result, nil
	}

	if _, err := tx.ExecContext(ctx, finalizeLockTimeoutQuery); err != nil {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: set lock timeout: %w", err)
	}
	var active sql.NullString
	found, err := queryOne(ctx, tx, lockScopeQuery, []any{work.ScopeID}, &active)
	if err != nil {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: lock scope: %w", err)
	}
	if !found {
		return FinalizeResult{Outcome: OutcomeMissing}, nil
	}
	var (
		token     int64
		owner     string
		state     string
		unexpired bool
	)
	found, err = queryOne(ctx, tx, lockObligationQuery, []any{work.ScopeID, work.GenerationID},
		&token, &owner, &state, &unexpired)
	if err != nil {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: lock obligation: %w", err)
	}
	if !found {
		return FinalizeResult{Outcome: OutcomeMissing}, nil
	}
	if state != string(StateLeased) || owner != work.LeaseOwner || token != work.LeaseToken || !unexpired {
		return FinalizeResult{Outcome: OutcomeNotOwner}, nil
	}
	fence := []any{work.ScopeID, work.GenerationID, work.LeaseToken, work.LeaseOwner}

	if !active.Valid || active.String != work.GenerationID {
		changed, err := execRows(ctx, tx, obsoleteObligationQuery, fence...)
		if err != nil {
			return FinalizeResult{}, fmt.Errorf("finalize activation obligation: retire obsolete: %w", err)
		}
		if changed != 1 {
			return FinalizeResult{Outcome: OutcomeNotOwner}, nil
		}
		return commit(FinalizeResult{Outcome: OutcomeObsolete})
	}

	var ready bool
	if _, err := queryOne(ctx, tx, phaseReadyQuery, []any{work.ScopeID, work.GenerationID}, &ready); err != nil {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: read phase: %w", err)
	}
	if !ready {
		return FinalizeResult{Outcome: OutcomePhaseNotReady}, nil
	}

	woken, err := execRows(ctx, tx, wakeQuery, work.ScopeID, work.GenerationID)
	if err != nil {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: wake: %w", err)
	}
	var remaining bool
	if _, err := queryOne(ctx, tx, remainingWorkQuery, []any{work.ScopeID, work.GenerationID}, &remaining); err != nil {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: read remaining work: %w", err)
	}
	if remaining {
		var owned bool
		if _, err := queryOne(ctx, tx, stillOwnedQuery, fence, &owned); err != nil {
			return FinalizeResult{}, fmt.Errorf("finalize activation obligation: recheck lease: %w", err)
		}
		if !owned {
			return FinalizeResult{}, fmt.Errorf("finalize activation obligation: partial wake: %w", ErrLeaseLost)
		}
		return commit(FinalizeResult{Outcome: OutcomeWorkPending, Woken: int(woken)})
	}
	changed, err := execRows(ctx, tx, completeObligationQuery, fence...)
	if err != nil {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: complete: %w", err)
	}
	if changed != 1 {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: completion: %w", ErrLeaseLost)
	}
	return commit(FinalizeResult{Outcome: OutcomeCompleted, Woken: int(woken)})
}

// queryOne scans the first row of query into dest and reports whether a row
// existed.
func queryOne(ctx context.Context, queryer db.Queryer, query string, args []any, dest ...any) (bool, error) {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	if err := rows.Scan(dest...); err != nil {
		return false, err
	}
	return true, rows.Err()
}

func execRows(ctx context.Context, executor db.Executor, query string, args ...any) (int64, error) {
	result, err := executor.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Join(errors.New("rows affected"), err)
	}
	return n, nil
}
