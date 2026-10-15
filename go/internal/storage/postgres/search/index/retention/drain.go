// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Generation names one superseded generation whose search index rows a pass
// pre-deletes. Every statement carries both ids, so a batch can never touch
// a row of another generation of the same scope.
type Generation struct {
	ScopeID      string
	GenerationID string
}

// Outcome is how one generation's pre-delete ended in a pass. The set is
// closed; only OutcomeDrained admits the generation to the locked prune.
type Outcome string

// Pre-delete outcomes.
const (
	// OutcomeDrained: a from-start page of both tables returned no row, so
	// the locked cascade has nothing left to delete for the generation.
	OutcomeDrained Outcome = "drained"
	// OutcomeLockBusy: another pass or a prune transaction holds the
	// generation's advisory lock; this pass leaves it alone.
	OutcomeLockBusy Outcome = "advisory_lock_busy"
	// OutcomeIneligible: the generation is no longer a superseded,
	// non-active generation without live work, so the batch deleted
	// nothing.
	OutcomeIneligible Outcome = "ineligible"
	// OutcomeLockYield: a batch hit lock_timeout or a deadlock and rolled
	// back with nothing applied; a writer holds rows of the generation.
	OutcomeLockYield Outcome = "lock_yield"
	// OutcomeStalled: a page found rows but deleted none of them, so another
	// page would loop on the same rows.
	OutcomeStalled Outcome = "stalled"
	// OutcomeBudget: the pass's row cap or deadline ran out first.
	OutcomeBudget Outcome = "budget_exhausted"
)

// Report is one generation's pre-delete in one pass. Rows deleted by earlier,
// committed batches stay deleted whatever the outcome.
type Report struct {
	Generation
	Outcome          Outcome
	Batches          int
	TermsDeleted     int64
	DocumentsDeleted int64
	// KeysetDuration is the forward keyset walk; SweepDuration is every
	// from-start re-sweep after the first short page, the walk over the dead
	// index entries the keyset pass left behind.
	KeysetDuration time.Duration
	SweepDuration  time.Duration
}

// Drained reports whether the generation may enter the locked prune.
func (r Report) Drained() bool { return r.Outcome == OutcomeDrained }

// Rows is the rows the generation's batches deleted in this pass.
func (r Report) Rows() int64 { return r.TermsDeleted + r.DocumentsDeleted }

// Budget bounds one pass's pre-delete across generations: the rows it may
// delete and the wall-clock deadline. The first batch of a pass always runs,
// so every pass makes progress on its oldest generation. A Budget is not safe
// for concurrent use; one pass drains its generations one after another.
type Budget struct {
	// RowCap is the most rows the pass deletes before it stops starting
	// batches. Zero or less stops after the first batch.
	RowCap int64
	// Deadline stops the pass from starting a batch at or after it. The zero
	// time means no deadline.
	Deadline time.Time

	rows    int64
	batches int
	// afterBatch, set only by tests, runs after each committed batch.
	afterBatch func()
}

// Batches is the batch transactions the pass committed or rolled back so far.
func (b *Budget) Batches() int { return b.batches }

// Rows is the rows the pass deleted so far.
func (b *Budget) Rows() int64 { return b.rows }

// Exhausted reports whether the pass may start no further batch.
func (b *Budget) Exhausted(now time.Time) bool {
	if b.batches == 0 {
		return false
	}
	return b.rows >= b.RowCap || (!b.Deadline.IsZero() && !now.Before(b.Deadline))
}

// cursor is a keyset position; the zero value is the start of the keyspace.
type cursor struct{ first, second string }

// table is one search index table the pre-delete drains, in drain order.
type table struct {
	query string
	args  func(Generation, cursor) []any
	// add charges deleted rows to the report's counter for the table.
	add func(*Report, int64)
}

var tables = [...]table{
	{
		query: deleteTermsBatchQuery,
		args: func(g Generation, c cursor) []any {
			return []any{g.ScopeID, g.GenerationID, c.first, c.second, BatchRows}
		},
		add: func(r *Report, n int64) { r.TermsDeleted += n },
	},
	{
		query: deleteDocumentsBatchQuery,
		args: func(g Generation, c cursor) []any {
			return []any{g.ScopeID, g.GenerationID, c.first, BatchRows}
		},
		add: func(r *Report, n int64) { r.DocumentsDeleted += n },
	},
}

// batchResult is one committed batch.
type batchResult struct {
	eligible bool
	selected int64
	deleted  int64
	last     cursor
}

// Drain pre-deletes one superseded generation's eshu_search_index_terms and
// eshu_search_index_documents rows in committed batch transactions of at
// most BatchRows rows, terms first. Each table walks its primary key forward
// in keyset pages until a page comes back short, then re-sweeps from the
// start of the keyspace until a from-start page returns no row: that re-sweep
// catches a row inserted behind the cursor and pays the dead-index-entry walk
// here, unlocked, instead of under the prune's scope lock. Only then is the
// table drained.
//
// Every batch is its own transaction: it takes the generation's advisory lock
// with pg_try_advisory_xact_lock(LockClass, hashtext(generation_id)), sets a
// lock_timeout below deadlock_timeout, and runs one statement that re-checks
// eligibility in its own snapshot. A busy lock, an ineligible generation, a
// lock yield, a stalled page or a spent budget ends the generation's drain
// with that outcome and a nil error; the caller moves on. ctx is checked
// before every batch. An error (a cancelled ctx included) returns the report
// so far with an empty Outcome; a rerun converges because every batch is
// idempotent and committed batches stay deleted.
func Drain(ctx context.Context, database db.Beginner, generation Generation, budget *Budget) (Report, error) {
	report := Report{Generation: generation, Outcome: OutcomeDrained}
	for _, spec := range tables {
		if outcome, err := drainTable(ctx, database, spec, &report, budget); err != nil || outcome != OutcomeDrained {
			report.Outcome = outcome
			return report, err
		}
	}
	return report, nil
}

// drainTable runs one table's keyset walk and re-sweep for Drain.
func drainTable(ctx context.Context, database db.Beginner, spec table, report *Report, budget *Budget) (Outcome, error) {
	var at cursor
	sweeping := false
	for {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("search index pre-delete: %w", err)
		}
		if budget.Exhausted(time.Now()) {
			return OutcomeBudget, nil
		}
		started := time.Now()
		result, outcome, err := runBatch(ctx, database, spec, report.Generation, at)
		if sweeping {
			report.SweepDuration += time.Since(started)
		} else {
			report.KeysetDuration += time.Since(started)
		}
		report.Batches++
		budget.batches++
		if err != nil {
			return "", err
		}
		if budget.afterBatch != nil {
			budget.afterBatch()
		}
		if outcome != "" {
			return outcome, nil
		}
		spec.add(report, result.deleted)
		budget.rows += result.deleted
		switch {
		case result.selected >= BatchRows:
			at = result.last
		case at == (cursor{}) && result.selected == 0:
			return OutcomeDrained, nil
		case result.deleted == 0:
			return OutcomeStalled, nil
		default:
			at, sweeping = cursor{}, true
		}
	}
}

// runBatch runs one pre-delete batch transaction. A non-empty outcome means
// the batch applied nothing and the generation's drain ends there.
func runBatch(ctx context.Context, database db.Beginner, spec table, generation Generation, at cursor) (result batchResult, outcome Outcome, err error) {
	tx, err := database.Begin(ctx)
	if err != nil {
		return result, "", fmt.Errorf("search index pre-delete: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, setLockTimeoutQuery, lockTimeoutSetting); err != nil {
		return result, "", fmt.Errorf("search index pre-delete: set lock_timeout: %w", err)
	}
	locked, err := queryBool(ctx, tx, tryLockQuery, generation.GenerationID)
	if err != nil {
		return result, "", fmt.Errorf("search index pre-delete: try lock: %w", err)
	}
	if !locked {
		return result, OutcomeLockBusy, nil
	}
	rows, err := tx.QueryContext(ctx, spec.query, spec.args(generation, at)...)
	if err != nil {
		if isLockYield(err) {
			return result, OutcomeLockYield, nil
		}
		return result, "", fmt.Errorf("search index pre-delete: batch: %w", err)
	}
	if err := scanBatch(rows, &result); err != nil {
		if isLockYield(err) {
			return result, OutcomeLockYield, nil
		}
		return result, "", fmt.Errorf("search index pre-delete: batch: %w", err)
	}
	if !result.eligible {
		return result, OutcomeIneligible, nil
	}
	if err := tx.Commit(); err != nil {
		return result, "", fmt.Errorf("search index pre-delete: commit: %w", err)
	}
	committed = true
	return result, "", nil
}

// scanBatch reads the batch statement's single result row.
func scanBatch(rows db.Rows, result *batchResult) error {
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return errors.New("batch returned no row")
	}
	if err := rows.Scan(&result.eligible, &result.selected, &result.deleted, &result.last.first, &result.last.second); err != nil {
		return err
	}
	return rows.Err()
}

// queryBool reads one boolean column from a single-row query.
func queryBool(ctx context.Context, q db.Queryer, query string, args ...any) (bool, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	var value bool
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, err
		}
		return false, errors.New("query returned no row")
	}
	if err := rows.Scan(&value); err != nil {
		return false, err
	}
	return value, rows.Err()
}

// isLockYield reports the errors that mean another session holds rows the
// batch needs: lock_timeout expiry or a detected deadlock. Both roll the
// batch back with nothing applied.
func isLockYield(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "55P03" || pgErr.Code == "40P01"
}
