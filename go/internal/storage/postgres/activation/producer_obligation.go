// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// ProducerOutcome is why one producer settle call ended. It is a closed set,
// safe as a metric label.
type ProducerOutcome string

// Producer settle outcomes.
const (
	// ProducerCompleted: the dependents were reopened and the token-fenced
	// completion committed. Zero dependents is still completed: the
	// obligation settles the signal, not a row count.
	ProducerCompleted ProducerOutcome = "completed"
	// ProducerObsolete: the scope no longer points at the generation, so
	// the obligation retired without reopening anything.
	ProducerObsolete ProducerOutcome = "obsolete"
	// ProducerNotOwner: the caller's owner, token or lease no longer
	// matches the row (expired, reclaimed, already finished, or forged).
	// Nothing was written.
	ProducerNotOwner ProducerOutcome = "not_owner"
	// ProducerMissing: no scope row or no obligation row exists for the
	// identity. Nothing was written.
	ProducerMissing ProducerOutcome = "missing"
	// ProducerInapplicable: the generation carries no producer evidence
	// at settle time, so no consumer can wait on it.
	ProducerInapplicable ProducerOutcome = "inapplicable"
)

// ProducerObligation is one claimed producer-activation obligation: the
// producer scope generation whose activation must replay its dependents, and
// the lease identity that fences its completion.
type ProducerObligation struct {
	ScopeID      string
	GenerationID string
	LeaseOwner   string
	LeaseToken   int64
	LeaseUntil   time.Time
	// CreatedAt is when the obligation was owed (Ack time). The
	// staleness conjunct reopens only items that completed before it.
	CreatedAt time.Time
}

// ProducerSettleResult reports one producer settle call.
type ProducerSettleResult struct {
	Outcome ProducerOutcome
	// Reopened counts the consumer rows whose reopen committed.
	Reopened int
}

// InsertProducerActivation records the producer-activation obligation for one
// scope generation. Callers run it inside the transaction that activates the
// generation, so the obligation commits or rolls back with the activation. A
// repeated insert for the same generation is a no-op, except that
// re-activating a generation whose row is obsolete owes it again (see
// insertProducerObligationQuery).
func InsertProducerActivation(ctx context.Context, executor db.Executor, scopeID, generationID, workItemID string) error {
	if _, err := executor.ExecContext(ctx, insertProducerObligationQuery, scopeID, generationID, workItemID); err != nil {
		return fmt.Errorf("insert producer activation obligation: %w", err)
	}
	return nil
}

// ClaimProducerActivation leases the oldest claimable producer obligation to
// owner for lease. It returns nil when nothing is claimable. A pending row,
// or a leased row whose lease has expired on the database clock, is
// claimable; claiming bumps the row's claim_token, so every earlier holder
// is fenced out.
func (s Store) ClaimProducerActivation(ctx context.Context, owner string, lease time.Duration) (*ProducerObligation, error) {
	if strings.TrimSpace(owner) == "" || lease <= 0 {
		return nil, errors.New("claim producer activation obligation: owner and positive lease required")
	}
	rows, err := s.database.QueryContext(ctx, claimProducerObligationQuery, owner,
		float64(lease)/float64(time.Millisecond))
	if err != nil {
		return nil, fmt.Errorf("claim producer activation obligation: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("claim producer activation obligation: %w", err)
		}
		return nil, nil
	}
	var work ProducerObligation
	if err := rows.Scan(&work.ScopeID, &work.GenerationID, &work.LeaseOwner,
		&work.LeaseToken, &work.LeaseUntil, &work.CreatedAt); err != nil {
		return nil, fmt.Errorf("claim producer activation obligation: scan: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim producer activation obligation: %w", err)
	}
	return &work, nil
}

// ProducerSettleTx is an open transaction holding the scope and producer
// obligation row locks for an obligation whose lease the caller still owns.
// The reopen itself runs in the postgres package (the dependency index lives
// beside crossScopeCorrelationReopenDomains, which this package cannot
// import), so the settle orchestrator holds this handle across the reopen
// and completes or retires under the same fence.
type ProducerSettleTx struct {
	tx        db.Transaction
	active    sql.NullString
	fence     []any
	committed bool
}

// BeginProducerSettle begins the transaction, takes the scope lock then the
// obligation lock, and checks the caller's lease. It returns a nil handle
// with the outcome to report when the scope or row is missing or the caller
// is not the owner; nothing is written in that case.
//
// Lock order matches ProjectorQueue.Ack: the scope row first, then the
// obligation row.
func (s Store) BeginProducerSettle(ctx context.Context, work ProducerObligation) (*ProducerSettleTx, ProducerSettleResult, error) {
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return nil, ProducerSettleResult{}, fmt.Errorf("settle producer activation obligation: begin: %w", err)
	}
	settle := &ProducerSettleTx{tx: tx}
	if _, err := tx.ExecContext(ctx, finalizeLockTimeoutQuery); err != nil {
		settle.Rollback()
		return nil, ProducerSettleResult{}, fmt.Errorf("settle producer activation obligation: set lock timeout: %w", err)
	}
	found, err := queryOne(ctx, tx, lockScopeQuery, []any{work.ScopeID}, &settle.active)
	if err != nil || !found {
		settle.Rollback()
		if err != nil {
			return nil, ProducerSettleResult{}, fmt.Errorf("settle producer activation obligation: lock scope: %w", err)
		}
		return nil, ProducerSettleResult{Outcome: ProducerMissing}, nil
	}
	var (
		token     int64
		owner     string
		state     string
		unexpired bool
	)
	found, err = queryOne(ctx, tx, lockProducerObligationQuery, []any{work.ScopeID, work.GenerationID},
		&token, &owner, &state, &unexpired)
	if err != nil || !found {
		settle.Rollback()
		if err != nil {
			return nil, ProducerSettleResult{}, fmt.Errorf("settle producer activation obligation: lock obligation: %w", err)
		}
		return nil, ProducerSettleResult{Outcome: ProducerMissing}, nil
	}
	if state != string(StateLeased) || owner != work.LeaseOwner || token != work.LeaseToken || !unexpired {
		settle.Rollback()
		return nil, ProducerSettleResult{Outcome: ProducerNotOwner}, nil
	}
	settle.fence = []any{work.ScopeID, work.GenerationID, work.LeaseToken, work.LeaseOwner}
	return settle, ProducerSettleResult{}, nil
}

// Tx exposes the settle transaction for the dependency-index reopen. The
// caller must not commit or roll back through it; use Complete, Retire or
// Rollback.
func (s *ProducerSettleTx) Tx() db.Transaction {
	return s.tx
}

// IsActiveGeneration reports whether the scope still points at generationID.
// A scope that moved on retires the obligation as obsolete instead of
// reopening against a generation that is no longer active.
func (s *ProducerSettleTx) IsActiveGeneration(generationID string) bool {
	return s.active.Valid && s.active.String == generationID
}

// StillOwned rechecks the lease on the database clock before a reopen
// commits, so a reopen never commits under an expired lease.
func (s *ProducerSettleTx) StillOwned(ctx context.Context) (bool, error) {
	var owned bool
	if _, err := queryOne(ctx, s.tx, stillOwnedProducerQuery, s.fence, &owned); err != nil {
		return false, fmt.Errorf("settle producer activation obligation: recheck lease: %w", err)
	}
	return owned, nil
}

// Complete runs the token-fenced completion and commits it with the reopen
// the caller already ran in the transaction. A fence that matches no row
// means the lease moved on; nothing is committed.
func (s *ProducerSettleTx) Complete(ctx context.Context, reopened int) (ProducerSettleResult, error) {
	changed, err := execRows(ctx, s.tx, completeProducerObligationQuery, s.fence...)
	if err != nil {
		return ProducerSettleResult{}, fmt.Errorf("settle producer activation obligation: complete: %w", err)
	}
	if changed != 1 {
		return ProducerSettleResult{Outcome: ProducerNotOwner}, nil
	}
	if err := s.tx.Commit(); err != nil {
		return ProducerSettleResult{}, fmt.Errorf("settle producer activation obligation: commit: %w", err)
	}
	s.committed = true
	return ProducerSettleResult{Outcome: ProducerCompleted, Reopened: reopened}, nil
}

// Retire runs one token-fenced terminal write (obsolete or inapplicable) and
// commits it. A fence that matched no row means the lease moved on; nothing
// is committed. Any other outcome is a caller bug and fails without writing.
func (s *ProducerSettleTx) Retire(ctx context.Context, outcome ProducerOutcome) (ProducerSettleResult, error) {
	if outcome != ProducerObsolete && outcome != ProducerInapplicable {
		return ProducerSettleResult{}, fmt.Errorf("settle producer activation obligation: retire %q: not a terminal outcome", outcome)
	}
	query := obsoleteProducerObligationQuery
	if outcome == ProducerInapplicable {
		query = inapplicableProducerObligationQuery
	}
	changed, err := execRows(ctx, s.tx, query, s.fence...)
	if err != nil {
		return ProducerSettleResult{}, fmt.Errorf("settle producer activation obligation: retire %s: %w", outcome, err)
	}
	if changed != 1 {
		return ProducerSettleResult{Outcome: ProducerNotOwner}, nil
	}
	if err := s.tx.Commit(); err != nil {
		return ProducerSettleResult{}, fmt.Errorf("settle producer activation obligation: commit: %w", err)
	}
	s.committed = true
	return ProducerSettleResult{Outcome: outcome}, nil
}

// Rollback aborts the settle transaction. It is safe to call after Complete
// or Retire.
func (s *ProducerSettleTx) Rollback() {
	if !s.committed {
		_ = s.tx.Rollback()
		s.committed = true
	}
}
