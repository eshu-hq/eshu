// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// MinInterval is the shortest cadence the writer accepts. A 2 s cadence
	// raised reducer claim p95 by 55-61 ms on the #7009 fixture; 5 s stayed
	// inside the no-writer spread.
	MinInterval = 5 * time.Second
	// DefaultInterval is the cadence used when none is configured. The
	// active-work pass measured a 1.0 s median on the ops-qa read replica
	// (2026-10-06 probe), which the #7009 ruling maps to a 10 s interval.
	DefaultInterval = 10 * time.Second
)

// Pass outcomes, the closed value set of the outcome metric label and span
// attribute.
const (
	// OutcomeOK means the pass wrote a row that advanced the stored as_of.
	OutcomeOK = "ok"
	// OutcomeSkippedLock means another writer held the advisory lock, so this
	// pass ran no statement.
	OutcomeSkippedLock = "skipped_lock"
	// OutcomeSkippedMissingTable means status_summary_snapshots does not exist
	// yet (migration 161 has not applied); the pass ran no statement or wrote
	// nothing.
	OutcomeSkippedMissingTable = "skipped_missing_table"
	// OutcomeRejectedGuard means the upsert's as_of guard kept a stored row
	// that is as new or newer, so nothing changed.
	OutcomeRejectedGuard = "rejected_guard"
	// OutcomeError means the pass failed and rolled back; the next tick
	// retries.
	OutcomeError = "error"
)

// setReadCommittedSQL pins the pass to READ COMMITTED whatever the cluster's
// default_transaction_isolation is. Under REPEATABLE READ the guarded upsert
// raises 40001 when another writer committed the row after the pass's
// snapshot; READ COMMITTED re-checks the guard against the committed row.
// It must be the transaction's first statement.
const setReadCommittedSQL = `SET TRANSACTION ISOLATION LEVEL READ COMMITTED`

// setJITOffSQL turns JIT off for the pass, as the live status read does
// (#7639): the statement is cheaper to plan than to compile.
const setJITOffSQL = `SET LOCAL jit = off`

// passClockSQL reads the database clock the pass binds as as_of, and whether
// the model table exists, in one round trip after the lock is held. Reading
// the clock from the database keeps as_of monotonic across reducer replicas
// whose host clocks differ.
const passClockSQL = `SELECT clock_timestamp(), to_regclass('status_summary_snapshots') IS NOT NULL`

// Statement is one model's source: the statement a pass runs and the digest
// of its text. The storage package supplies it (postgres.
// ReadActiveWorkSummaryEntries and postgres.ActiveWorkSummarySourceSHA256),
// so this package never holds the statement's SQL.
type Statement struct {
	// ModelKey is the stored row's key.
	ModelKey string
	// SourceSHA256 is the hex SHA-256 of the statement text Compute runs; the
	// reader's rolling-upgrade fence compares it.
	SourceSHA256 string
	// Compute runs the statement on the pass transaction with asOf as $1 and
	// returns its rows in live order.
	Compute func(ctx context.Context, queryer db.Queryer, asOf time.Time) ([]store.Entry, error)
}

// RowResult is one model's part of a pass that reached the row writes.
type RowResult struct {
	// ModelKey is the model's row key.
	ModelKey string
	// Outcome is OutcomeOK when the row's upsert advanced the stored as_of and
	// OutcomeRejectedGuard when the guard kept a row that is as new or newer.
	Outcome string
	// RowCount is the number of entries the model's statement returned.
	RowCount int
	// Compute is the time the model's statement took inside the pass.
	Compute time.Duration
}

// Pass reports one writer pass.
type Pass struct {
	// Outcome is one of the Outcome constants. A pass with several models is
	// OutcomeOK when any row advanced and OutcomeRejectedGuard when every
	// row's guard rejected it; Rows has each model's own result.
	Outcome string
	// Rows holds one result per model, in statement order, for a pass that
	// reached the row writes; empty for a pass that skipped or failed before.
	Rows []RowResult
	// AsOf is the database clock the pass bound, zero when it skipped before
	// reading it.
	AsOf time.Time
	// RowCount is the number of rows the pass computed.
	RowCount int
	// Duration is the whole pass, from Begin to Commit or Rollback.
	Duration time.Duration
	// Err is the failure of an OutcomeError pass.
	Err error
}

// Runner is the reducer-owned periodic writer of one status summary model
// (#7009). Every Interval it runs one pass in one transaction: SET LOCAL jit =
// off, the transaction-scoped advisory try-lock, the database clock, the
// statement at that clock, and one guarded single-row upsert. Any number of
// reducer replicas may run it; the lock makes exactly one compute per tick
// and the rest skip. Passes never overlap or queue: the next pass starts on
// the first interval boundary after the previous one ends.
type Runner struct {
	// DB opens the pass transaction on the primary.
	DB db.Beginner
	// Statement is the first model the runner writes.
	Statement Statement
	// Companions are further models written by the same pass: each its own
	// row with its own key, digest, as_of guard and compute time, under the
	// pass's one transaction, lock and database clock. Their statements run
	// after Statement's, and every row is written after every statement has
	// run, so a slow statement never holds an earlier row's lock.
	Companions []Statement
	// Interval is the cadence; zero means DefaultInterval and anything below
	// MinInterval is refused.
	Interval time.Duration
	// Now is the clock for pass durations and pacing; nil means time.Now.
	Now func() time.Time
	// Wait sleeps between passes; nil means a timer that stops on cancel.
	Wait func(context.Context, time.Duration) error

	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Logger      *slog.Logger

	warnedMissingTable atomic.Bool
}

// Run writes the model every interval until ctx is cancelled, then returns
// nil. A failed pass is counted and logged and the next tick retries; only an
// invalid runner configuration returns an error.
func (r *Runner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	interval := r.interval()
	r.recordUp(ctx, 1)
	defer r.recordUp(context.WithoutCancel(ctx), 0)
	r.logStart(ctx, interval)
	for {
		if ctx.Err() != nil {
			return nil
		}
		pass := r.RunOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if pass.Duration > interval {
			r.recordOverrun(ctx, pass, interval)
		}
		if err := r.wait(ctx, nextWait(pass.Duration, interval)); err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("wait for status summary writer pass: %w", err)
		}
	}
}

// RunOnce runs one pass and records its telemetry. A pass cut short by the
// caller's cancellation is returned as an error outcome but not counted.
func (r *Runner) RunOnce(ctx context.Context) Pass {
	if err := r.validate(); err != nil {
		return Pass{Outcome: OutcomeError, Err: err}
	}
	if r.Tracer != nil {
		var span trace.Span
		ctx, span = r.Tracer.Start(ctx, telemetry.SpanReducerStatusSummaryPass)
		defer span.End()
	}
	start := r.now()
	passCtx, cancel := context.WithTimeout(ctx, 2*r.interval())
	defer cancel()
	pass := r.pass(passCtx)
	pass.Duration = r.now().Sub(start)
	if ctx.Err() != nil {
		return pass
	}
	r.record(ctx, pass)
	return pass
}

// pass runs the one transaction of a writer pass.
func (r *Runner) pass(ctx context.Context) (result Pass) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return failed(fmt.Errorf("begin status summary pass: %w", err))
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, setReadCommittedSQL); err != nil {
		return failed(fmt.Errorf("set status summary pass isolation: %w", err))
	}
	if _, err := tx.ExecContext(ctx, setJITOffSQL); err != nil {
		return failed(fmt.Errorf("disable jit for status summary pass: %w", err))
	}
	acquired, err := store.TryLock(ctx, tx)
	if err != nil {
		return failed(err)
	}
	if !acquired {
		return Pass{Outcome: OutcomeSkippedLock}
	}
	asOf, installed, err := readPassClock(ctx, tx)
	if err != nil {
		return failed(err)
	}
	if !installed {
		return Pass{Outcome: OutcomeSkippedMissingTable, AsOf: asOf}
	}
	statements := r.statements()
	computed := make([]computedRow, 0, len(statements))
	for _, statement := range statements {
		computeStart := r.now()
		entries, err := statement.Compute(ctx, tx, asOf)
		if err != nil {
			return failed(fmt.Errorf("compute status summary %q: %w", statement.ModelKey, err))
		}
		computed = append(computed, computedRow{statement: statement, entries: entries, took: r.now().Sub(computeStart)})
	}
	result = Pass{AsOf: asOf, RowCount: len(computed[0].entries)}
	advancedAny := false
	for _, c := range computed {
		advanced, err := store.Upsert(ctx, tx, store.Row{
			ModelKey:      c.statement.ModelKey,
			SchemaVersion: store.SchemaVersion,
			SourceSHA256:  c.statement.SourceSHA256,
			AsOf:          asOf,
			PassDuration:  c.took,
			RowCount:      len(c.entries),
			Entries:       c.entries,
		})
		if errors.Is(err, store.ErrNotInstalled) {
			result.Rows = nil
			result.Outcome = OutcomeSkippedMissingTable
			return result
		}
		if err != nil {
			return failed(err)
		}
		outcome := OutcomeRejectedGuard
		if advanced {
			outcome, advancedAny = OutcomeOK, true
		}
		result.Rows = append(result.Rows, RowResult{
			ModelKey: c.statement.ModelKey, Outcome: outcome, RowCount: len(c.entries), Compute: c.took,
		})
	}
	if err := tx.Commit(); err != nil {
		committed = true // the driver ends the transaction on a failed commit
		return failed(fmt.Errorf("commit status summary pass: %w", err))
	}
	committed = true
	result.Outcome = OutcomeOK
	if !advancedAny {
		result.Outcome = OutcomeRejectedGuard
	}
	return result
}

// computedRow is one model's statement result inside a pass.
type computedRow struct {
	statement Statement
	entries   []store.Entry
	took      time.Duration
}

// statements returns the pass's statements: the first model, then the
// companions.
func (r *Runner) statements() []Statement {
	return append([]Statement{r.Statement}, r.Companions...)
}

// readPassClock reads the pass's database clock and whether the model table
// exists.
func readPassClock(ctx context.Context, queryer db.Queryer) (time.Time, bool, error) {
	rows, err := queryer.QueryContext(ctx, passClockSQL)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read status summary pass clock: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return time.Time{}, false, fmt.Errorf("read status summary pass clock: %w", err)
		}
		return time.Time{}, false, errors.New("read status summary pass clock: no result row")
	}
	var (
		asOf      time.Time
		installed bool
	)
	if err := rows.Scan(&asOf, &installed); err != nil {
		return time.Time{}, false, fmt.Errorf("scan status summary pass clock: %w", err)
	}
	return asOf.UTC(), installed, rows.Err()
}

// nextWait returns how long to wait after a pass of the given duration so the
// next pass starts on the first interval boundary after this one ends. A
// pass never starts early and never queues behind a slow one.
func nextWait(elapsed, interval time.Duration) time.Duration {
	if elapsed < 0 {
		elapsed = 0
	}
	periods := elapsed/interval + 1
	return periods*interval - elapsed
}

func failed(err error) Pass {
	return Pass{Outcome: OutcomeError, Err: err}
}

func (r *Runner) validate() error {
	switch {
	case r.DB == nil:
		return errors.New("status summary writer: database is required")
	case strings.TrimSpace(r.Statement.ModelKey) == "":
		return errors.New("status summary writer: statement model key is required")
	case strings.TrimSpace(r.Statement.SourceSHA256) == "":
		return fmt.Errorf("status summary writer %q: statement source digest is required", r.Statement.ModelKey)
	case r.Statement.Compute == nil:
		return fmt.Errorf("status summary writer %q: statement compute is required", r.Statement.ModelKey)
	case r.validateCompanions() != nil:
		return r.validateCompanions()
	case r.Interval != 0 && r.Interval < MinInterval:
		return fmt.Errorf("status summary writer: interval %s is below the %s floor", r.Interval, MinInterval)
	}
	return nil
}

// validateCompanions checks each companion statement like the first one and
// refuses a model key used twice, since two rows cannot share a key.
func (r *Runner) validateCompanions() error {
	seen := map[string]bool{r.Statement.ModelKey: true}
	for _, c := range r.Companions {
		switch {
		case strings.TrimSpace(c.ModelKey) == "":
			return errors.New("status summary writer: companion model key is required")
		case strings.TrimSpace(c.SourceSHA256) == "":
			return fmt.Errorf("status summary writer %q: companion source digest is required", c.ModelKey)
		case c.Compute == nil:
			return fmt.Errorf("status summary writer %q: companion compute is required", c.ModelKey)
		case seen[c.ModelKey]:
			return fmt.Errorf("status summary writer: model key %q is used twice", c.ModelKey)
		}
		seen[c.ModelKey] = true
	}
	return nil
}

func (r *Runner) interval() time.Duration {
	if r.Interval == 0 {
		return DefaultInterval
	}
	return r.Interval
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Runner) wait(ctx context.Context, d time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
