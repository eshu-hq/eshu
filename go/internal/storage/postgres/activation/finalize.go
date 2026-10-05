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
// When the phase is absent and the generation has no repository fact, no
// pass can ever publish it, so Finalize retires the obligation as
// inapplicable instead of returning phase_not_ready (#7584 ruling D3(d)).
//
// A wake commits only while the caller still owns the lease on the database
// clock; when the lease expired mid-transaction Finalize rolls back and
// returns ErrLeaseLost, and the next owner repeats the wake.
func (s Store) Finalize(ctx context.Context, work Obligation) (FinalizeResult, error) {
	owned, result, err := s.openOwned(ctx, work, "finalize")
	if err != nil || owned == nil {
		return result, err
	}
	defer owned.rollback()
	if !owned.active.Valid || owned.active.String != work.GenerationID {
		return owned.retire(ctx, obsoleteObligationQuery, OutcomeObsolete)
	}
	var ready bool
	if _, err := queryOne(ctx, owned.tx, phaseReadyQuery, []any{work.ScopeID, work.GenerationID}, &ready); err != nil {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: read phase: %w", err)
	}
	if !ready {
		var repository bool
		if _, err := queryOne(ctx, owned.tx, repositoryFactQuery, []any{work.ScopeID, work.GenerationID}, &repository); err != nil {
			return FinalizeResult{}, fmt.Errorf("finalize activation obligation: read repository fact: %w", err)
		}
		if !repository {
			return owned.retire(ctx, inapplicableObligationQuery, OutcomeInapplicable)
		}
		return FinalizeResult{Outcome: OutcomePhaseNotReady}, nil
	}

	woken, err := execRows(ctx, owned.tx, wakeQuery, work.ScopeID, work.GenerationID)
	if err != nil {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: wake: %w", err)
	}
	var remaining bool
	if _, err := queryOne(ctx, owned.tx, remainingWorkQuery, []any{work.ScopeID, work.GenerationID}, &remaining); err != nil {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: read remaining work: %w", err)
	}
	if remaining {
		var stillOwned bool
		if _, err := queryOne(ctx, owned.tx, stillOwnedQuery, owned.fence, &stillOwned); err != nil {
			return FinalizeResult{}, fmt.Errorf("finalize activation obligation: recheck lease: %w", err)
		}
		if !stillOwned {
			return FinalizeResult{}, fmt.Errorf("finalize activation obligation: partial wake: %w", ErrLeaseLost)
		}
		return owned.commit(FinalizeResult{Outcome: OutcomeWorkPending, Woken: int(woken)})
	}
	changed, err := execRows(ctx, owned.tx, completeObligationQuery, owned.fence...)
	if err != nil {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: complete: %w", err)
	}
	if changed != 1 {
		return FinalizeResult{}, fmt.Errorf("finalize activation obligation: completion: %w", ErrLeaseLost)
	}
	return owned.commit(FinalizeResult{Outcome: OutcomeCompleted, Woken: int(woken)})
}

// RetireInapplicable retires one claimed obligation as inapplicable after the
// maintainer reported that the owed partition maps to no repository in the
// shipped active-repository read (a repo_id collision loser, #7584 ruling
// D3(c)). It takes the same locks and lease fence as Finalize; a scope that
// moved to another generation retires as obsolete instead.
func (s Store) RetireInapplicable(ctx context.Context, work Obligation) (FinalizeResult, error) {
	owned, result, err := s.openOwned(ctx, work, "retire inapplicable")
	if err != nil || owned == nil {
		return result, err
	}
	defer owned.rollback()
	if !owned.active.Valid || owned.active.String != work.GenerationID {
		return owned.retire(ctx, obsoleteObligationQuery, OutcomeObsolete)
	}
	return owned.retire(ctx, inapplicableObligationQuery, OutcomeInapplicable)
}

// ownedObligation is an open transaction holding the scope and obligation row
// locks for an obligation whose lease the caller still owns.
type ownedObligation struct {
	tx        db.Transaction
	op        string
	active    sql.NullString
	fence     []any
	committed bool
}

// openOwned begins the transaction, takes the scope lock then the obligation
// lock, and checks the caller's lease. It returns a nil ownedObligation with
// the outcome to report when the scope or row is missing or the caller is not
// the owner; nothing is written in that case.
func (s Store) openOwned(ctx context.Context, work Obligation, op string) (*ownedObligation, FinalizeResult, error) {
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return nil, FinalizeResult{}, fmt.Errorf("%s activation obligation: begin: %w", op, err)
	}
	owned := &ownedObligation{tx: tx, op: op}
	if _, err := tx.ExecContext(ctx, finalizeLockTimeoutQuery); err != nil {
		owned.rollback()
		return nil, FinalizeResult{}, fmt.Errorf("%s activation obligation: set lock timeout: %w", op, err)
	}
	found, err := queryOne(ctx, tx, lockScopeQuery, []any{work.ScopeID}, &owned.active)
	if err != nil || !found {
		owned.rollback()
		if err != nil {
			return nil, FinalizeResult{}, fmt.Errorf("%s activation obligation: lock scope: %w", op, err)
		}
		return nil, FinalizeResult{Outcome: OutcomeMissing}, nil
	}
	var (
		token     int64
		owner     string
		state     string
		unexpired bool
	)
	found, err = queryOne(ctx, tx, lockObligationQuery, []any{work.ScopeID, work.GenerationID},
		&token, &owner, &state, &unexpired)
	if err != nil || !found {
		owned.rollback()
		if err != nil {
			return nil, FinalizeResult{}, fmt.Errorf("%s activation obligation: lock obligation: %w", op, err)
		}
		return nil, FinalizeResult{Outcome: OutcomeMissing}, nil
	}
	if state != string(StateLeased) || owner != work.LeaseOwner || token != work.LeaseToken || !unexpired {
		owned.rollback()
		return nil, FinalizeResult{Outcome: OutcomeNotOwner}, nil
	}
	owned.fence = []any{work.ScopeID, work.GenerationID, work.LeaseToken, work.LeaseOwner}
	return owned, FinalizeResult{}, nil
}

// retire runs one token-fenced terminal write and commits it. A fence that
// matched no row means the lease moved on; nothing is committed.
func (o *ownedObligation) retire(ctx context.Context, query string, outcome Outcome) (FinalizeResult, error) {
	changed, err := execRows(ctx, o.tx, query, o.fence...)
	if err != nil {
		return FinalizeResult{}, fmt.Errorf("%s activation obligation: retire %s: %w", o.op, outcome, err)
	}
	if changed != 1 {
		return FinalizeResult{Outcome: OutcomeNotOwner}, nil
	}
	return o.commit(FinalizeResult{Outcome: outcome})
}

func (o *ownedObligation) commit(result FinalizeResult) (FinalizeResult, error) {
	if err := o.tx.Commit(); err != nil {
		return FinalizeResult{}, fmt.Errorf("%s activation obligation: commit: %w", o.op, err)
	}
	o.committed = true
	return result, nil
}

func (o *ownedObligation) rollback() {
	if !o.committed {
		_ = o.tx.Rollback()
		o.committed = true
	}
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
