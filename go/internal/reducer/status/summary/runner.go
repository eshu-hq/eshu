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

// ModelPass reports one model's transaction inside a pass.
type ModelPass struct {
	// ModelKey is the model's row key.
	ModelKey string
	// Outcome is one of the Outcome constants for this model's own
	// transaction.
	Outcome string
	// AsOf is the database clock this model's transaction bound, zero when it
	// skipped before reading it.
	AsOf time.Time
	// RowCount is the number of entries the model's statement returned.
	RowCount int
	// Compute is the time the model's statement took.
	Compute time.Duration
	// Duration is the model's own transaction, from Begin to Commit or
	// Rollback; zero for a model that never opened one.
	Duration time.Duration
	// Err is the failure of an OutcomeError model.
	Err error
}

// Pass reports one writer pass. Its Outcome, AsOf, RowCount and Err are the
// first model's; Models has every model's own result.
type Pass struct {
	// Outcome is one of the Outcome constants, the first model's.
	Outcome string
	// Models holds one result per model, the first model then the companions,
	// in order. A companion that was not attempted because the first model
	// skipped on the lock or the missing table carries that outcome with no
	// transaction.
	Models []ModelPass
	// AsOf is the database clock the first model bound, zero when it skipped
	// before reading it.
	AsOf time.Time
	// RowCount is the number of rows the first model computed.
	RowCount int
	// Duration is the whole pass, from the first Begin to the last Commit or
	// Rollback.
	Duration time.Duration
	// Err is the failure of an OutcomeError first model.
	Err error
}

// Runner is the reducer-owned periodic writer of the status summary models
// (#7009). Every Interval it runs one pass. Each model is written in its own
// transaction, the first model first: SET LOCAL jit = off, the
// transaction-scoped advisory try-lock, the database clock, the statement at
// that clock, and one guarded single-row upsert. Any number of reducer
// replicas may run it; the lock makes exactly one compute per model per tick
// and the rest skip. Passes never overlap or queue: the next pass starts on
// the first interval boundary after the previous one ends.
type Runner struct {
	// DB opens the pass transaction on the primary.
	DB db.Beginner
	// Statement is the first model the runner writes.
	Statement Statement
	// Companions are further models written by the same pass, after
	// Statement's row is committed. Each has its own row, key, digest, as_of
	// guard, database clock and transaction, under the same advisory lock key,
	// so a companion that fails or runs out of time never discards an earlier
	// row. A companion runs under the smaller of the pass's remaining budget
	// and one interval.
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
	first := r.modelPass(passCtx, r.Statement)
	first.Duration = r.now().Sub(start)
	pass := Pass{
		Outcome: first.Outcome, AsOf: first.AsOf, RowCount: first.RowCount, Err: first.Err,
		Duration: first.Duration, Models: []ModelPass{first},
	}
	for _, companion := range r.Companions {
		pass.Models = append(pass.Models, r.companionPass(passCtx, first, companion))
	}
	if len(r.Companions) > 0 {
		pass.Duration = r.now().Sub(start)
	}
	if ctx.Err() != nil {
		return pass
	}
	r.record(ctx, pass)
	return pass
}

// companionPass writes one companion model after the first. A first model
// that skipped on the lock or the missing table means another writer owns this
// tick or the table is absent, so the companion reports the same outcome
// without opening a transaction. A first model that failed or was rejected by
// its guard does not stop the companion: the rows are independent.
func (r *Runner) companionPass(ctx context.Context, first ModelPass, statement Statement) ModelPass {
	if first.Outcome == OutcomeSkippedLock || first.Outcome == OutcomeSkippedMissingTable {
		return ModelPass{ModelKey: statement.ModelKey, Outcome: first.Outcome}
	}
	ctx, cancel := context.WithTimeout(ctx, r.interval())
	defer cancel()
	start := r.now()
	result := r.modelPass(ctx, statement)
	result.Duration = r.now().Sub(start)
	return result
}

// modelPass runs one model's transaction: read committed, jit off, the
// advisory try-lock, the database clock, the statement at that clock, one
// guarded upsert, and commit.
func (r *Runner) modelPass(ctx context.Context, statement Statement) (result ModelPass) {
	result.ModelKey = statement.ModelKey
	fail := func(err error) ModelPass {
		result.Outcome, result.Err = OutcomeError, err
		return result
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return fail(fmt.Errorf("begin status summary pass: %w", err))
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, setReadCommittedSQL); err != nil {
		return fail(fmt.Errorf("set status summary pass isolation: %w", err))
	}
	if _, err := tx.ExecContext(ctx, setJITOffSQL); err != nil {
		return fail(fmt.Errorf("disable jit for status summary pass: %w", err))
	}
	acquired, err := store.TryLock(ctx, tx)
	if err != nil {
		return fail(err)
	}
	if !acquired {
		result.Outcome = OutcomeSkippedLock
		return result
	}
	asOf, installed, err := readPassClock(ctx, tx)
	if err != nil {
		return fail(err)
	}
	result.AsOf = asOf
	if !installed {
		result.Outcome = OutcomeSkippedMissingTable
		return result
	}
	computeStart := r.now()
	entries, err := statement.Compute(ctx, tx, asOf)
	if err != nil {
		return fail(fmt.Errorf("compute status summary %q: %w", statement.ModelKey, err))
	}
	result.Compute, result.RowCount = r.now().Sub(computeStart), len(entries)
	advanced, err := store.Upsert(ctx, tx, store.Row{
		ModelKey:      statement.ModelKey,
		SchemaVersion: store.SchemaVersion,
		SourceSHA256:  statement.SourceSHA256,
		AsOf:          asOf,
		PassDuration:  result.Compute,
		RowCount:      len(entries),
		Entries:       entries,
	})
	if errors.Is(err, store.ErrNotInstalled) {
		result.RowCount, result.Outcome = 0, OutcomeSkippedMissingTable
		return result
	}
	if err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		committed = true // the driver ends the transaction on a failed commit
		return fail(fmt.Errorf("commit status summary pass: %w", err))
	}
	committed = true
	result.Outcome = OutcomeOK
	if !advanced {
		result.Outcome = OutcomeRejectedGuard
	}
	return result
}

// statements returns the writer's statements: the first model, then the
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
