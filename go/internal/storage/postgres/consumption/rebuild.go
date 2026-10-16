// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package consumption

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DefaultLockTimeout bounds how long one rebuild transaction waits for a
// lock, chiefly the scope row while an ingester commits that scope. A
// transaction that times out fails with SQLSTATE 55P03; the pass returns the
// error and the next pass retries the scope from its start.
const DefaultLockTimeout = 5 * time.Second

// PageLoader returns up to one page of the scope's sidecar-source facts with
// fact_id greater than afterFactID, ordered by fact_id. A page shorter than
// the Rebuilder's PageSize ends the scope.
type PageLoader func(ctx context.Context, queryer db.Queryer, scopeID, afterFactID string) ([]facts.Envelope, error)

// Refresher rewrites both sidecars for exactly the given facts inside tx:
// it deletes their keys by fact_id and inserts the keys derived from them.
type Refresher func(ctx context.Context, tx db.ExecQueryer, envelopes []facts.Envelope) error

// Rebuilder rebuilds one scope's package-consumption sidecars in short
// transactions: one start transaction, then one transaction per fact page.
// Beginner, LoadPage, Refresh, and PageSize are required. A zero LockTimeout
// uses DefaultLockTimeout; a nil Instruments records nothing.
type Rebuilder struct {
	Beginner    db.Beginner
	LoadPage    PageLoader
	Refresh     Refresher
	PageSize    int
	LockTimeout time.Duration
	Instruments *telemetry.Instruments
}

// RebuildScope deletes the scope's sidecar rows and re-derives them from its
// facts. The start transaction locks the scope row, deletes both sidecars'
// rows for the scope, and writes the scope's dirty row as a readiness fence,
// keeping its row version. Each later transaction locks the scope row,
// refreshes one page, and commits; the last page also deletes the dirty row,
// but only when that row still has the captured version. No transaction holds
// the scope row across more than one page.
//
// A failed or canceled transaction rolls back only itself. The pages already
// committed stay, the dirty row keeps readers from treating the partial
// sidecar as ready, and the next call rebuilds the scope from its start.
// The returned Result reflects the work done even when err is not nil.
func (r Rebuilder) RebuildScope(ctx context.Context, scopeID string, phase Phase) (Result, error) {
	if err := r.validate(); err != nil {
		return Result{ScopeID: scopeID, Phase: phase, Outcome: OutcomeFailed}, err
	}
	started := time.Now()
	result := Result{ScopeID: scopeID, Phase: phase}
	err := r.rebuild(ctx, &result)
	result.Duration = time.Since(started)
	if err != nil {
		result.Outcome = outcomeForError(err)
	}
	r.recordRebuild(ctx, result)
	return result, err
}

func (r Rebuilder) validate() error {
	if r.Beginner == nil || r.LoadPage == nil || r.Refresh == nil || r.PageSize <= 0 {
		return errors.New("package consumption scope rebuild requires a beginner, page loader, refresher, and positive page size")
	}
	return nil
}

func (r Rebuilder) rebuild(ctx context.Context, result *Result) error {
	token, proceed, err := r.start(ctx, result)
	if err != nil || !proceed {
		return err
	}
	cursor := ""
	for {
		last, done, err := r.page(ctx, result, token, cursor)
		if err != nil || done {
			return err
		}
		cursor = last
	}
}

// start runs the start transaction. proceed is false when the scope row is
// gone or a dirty repair finds the scope already repaired.
func (r Rebuilder) start(ctx context.Context, result *Result) (token string, proceed bool, err error) {
	scopeID := result.ScopeID
	err = r.inScopeTransaction(ctx, result, func(tx db.Transaction, locked bool) error {
		if !locked {
			if _, err := tx.ExecContext(ctx, deleteDirtyScopeSQL, scopeID); err != nil {
				return fmt.Errorf("clear disappeared package consumption scope: %w", err)
			}
			result.Outcome = OutcomeDisappeared
			return nil
		}
		if result.Phase == PhaseDirtyRepair {
			present, err := queryBool(ctx, tx, dirtyScopePresentSQL, scopeID)
			if err != nil {
				return fmt.Errorf("check package consumption dirty scope: %w", err)
			}
			if !present {
				result.Outcome = OutcomeSkipped
				return nil
			}
		}
		if _, err := tx.ExecContext(ctx, deleteManifestKeysSQL, scopeID); err != nil {
			return fmt.Errorf("clear package manifest consumption key scope: %w", err)
		}
		if _, err := tx.ExecContext(ctx, deleteRegistryKeysSQL, scopeID); err != nil {
			return fmt.Errorf("clear package registry identity key scope: %w", err)
		}
		fenced, err := queryString(ctx, tx, fenceDirtyScopeSQL, scopeID)
		if err != nil {
			return fmt.Errorf("fence package consumption dirty scope: %w", err)
		}
		token, proceed = fenced, true
		return nil
	})
	if err != nil {
		return "", false, err
	}
	return token, proceed, nil
}

// page refreshes the facts after cursor in one transaction. done is true once
// the scope is finished: the page was short, or the scope row disappeared.
func (r Rebuilder) page(ctx context.Context, result *Result, token, cursor string) (last string, done bool, err error) {
	scopeID := result.ScopeID
	loaded := 0
	err = r.inScopeTransaction(ctx, result, func(tx db.Transaction, locked bool) error {
		if !locked {
			// The scope was deleted mid-rebuild; its facts and their sidecar
			// keys went with it through ON DELETE CASCADE.
			if _, err := tx.ExecContext(ctx, deleteDirtyScopeSQL, scopeID); err != nil {
				return fmt.Errorf("clear disappeared package consumption scope: %w", err)
			}
			result.Outcome, done = OutcomeDisappeared, true
			return nil
		}
		envelopes, err := r.LoadPage(ctx, tx, scopeID, cursor)
		if err != nil {
			return err
		}
		if err := r.Refresh(ctx, tx, envelopes); err != nil {
			return err
		}
		loaded = len(envelopes)
		if loaded == r.PageSize {
			last = envelopes[loaded-1].FactID
			return nil
		}
		cleared, err := execAffected(ctx, tx, clearDirtyScopeIfUnchangedSQL, scopeID, token)
		if err != nil {
			return fmt.Errorf("clear package consumption dirty scope: %w", err)
		}
		result.Outcome, done = OutcomeLeftDirty, true
		if cleared {
			result.Outcome = OutcomeCleared
		}
		return nil
	})
	if err != nil {
		return "", false, err
	}
	result.Pages++
	result.Facts += loaded
	return last, done, nil
}

// inScopeTransaction runs body in one transaction that first bounds its lock
// waits and takes the scope row lock. locked is false when no scope row
// exists. Duration, lock wait, and outcome are recorded for every attempt.
func (r Rebuilder) inScopeTransaction(ctx context.Context, result *Result, body func(tx db.Transaction, locked bool) error) (err error) {
	began := time.Now()
	defer func() { r.recordTransaction(ctx, result, time.Since(began), err) }()
	tx, err := r.Beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin package consumption scope rebuild transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, beginRebuildTransactionSQL, lockTimeoutSetting(r.lockTimeout())); err != nil {
		return fmt.Errorf("configure package consumption scope rebuild transaction: %w", err)
	}
	lockStarted := time.Now()
	locked, err := queryExists(ctx, tx, lockScopeSQL, result.ScopeID)
	r.recordLockWait(ctx, result, time.Since(lockStarted))
	if err != nil {
		return fmt.Errorf("lock package consumption scope %q: %w", result.ScopeID, err)
	}
	if err := body(tx, locked); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit package consumption scope rebuild transaction: %w", err)
	}
	committed = true
	return nil
}

func (r Rebuilder) lockTimeout() time.Duration {
	if r.LockTimeout > 0 {
		return r.LockTimeout
	}
	return DefaultLockTimeout
}

func lockTimeoutSetting(timeout time.Duration) string {
	return fmt.Sprintf("%dms", max(timeout.Milliseconds(), 1))
}

func queryExists(ctx context.Context, queryer db.Queryer, query string, args ...any) (bool, error) {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	found := rows.Next()
	return found, rows.Err()
}

func queryBool(ctx context.Context, queryer db.Queryer, query string, args ...any) (bool, error) {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	var value bool
	if rows.Next() {
		if err := rows.Scan(&value); err != nil {
			return false, err
		}
	}
	return value, rows.Err()
}

func queryString(ctx context.Context, queryer db.Queryer, query string, args ...any) (string, error) {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", errors.New("statement returned no row")
	}
	var value string
	if err := rows.Scan(&value); err != nil {
		return "", err
	}
	return value, rows.Err()
}

func execAffected(ctx context.Context, executor db.Executor, query string, args ...any) (bool, error) {
	outcome, err := executor.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}
