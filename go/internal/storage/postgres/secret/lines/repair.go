// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RepairOutcome is the result of one Repairer poll and the outcome label of
// eshu_dp_secret_lines_repair_runs_total, so the set is closed.
type RepairOutcome string

// The RepairOutcome values.
const (
	// RepairSkippedReady: the state is ready, or no state row exists, so there
	// is nothing to rebuild and no lock was tried.
	RepairSkippedReady RepairOutcome = "skipped_ready"
	// RepairSkippedLocked: another session holds the bulk-load lock (5318,1),
	// a bootstrap-index run or another reducer replica that is rebuilding, or
	// live bootstrap-index sessions hold the deferred-writer lock (5318,2).
	RepairSkippedLocked RepairOutcome = "skipped_locked"
	// RepairPublished: this poll rebuilt the side table and published ready.
	RepairPublished RepairOutcome = "published"
	// RepairSuperseded: the epoch moved during the poll (a newer bulk load or a
	// migration began), so this rebuild did not publish. The next poll takes the
	// new epoch.
	RepairSuperseded RepairOutcome = "superseded"
	// RepairCancelled: shutdown cancelled the poll while it rebuilt. The state
	// stays failed and the next start heals it.
	RepairCancelled RepairOutcome = "cancelled"
	// RepairFailed: the state read, the lock, or the rebuild failed. A failed
	// rebuild leaves the state failed. The next poll retries after a backoff.
	RepairFailed RepairOutcome = "failed"
)

const (
	defaultRepairPoll       = 30 * time.Second
	defaultRepairMaxBackoff = time.Hour
	repairFailureClass      = "secret_lines_backfill_failure"
)

// readRepairStateSQL reads the singleton state row. policy_version arrives with
// migration 234 (#8833 ruling-3). to_jsonb yields it when the column exists and
// NULL before, so the Repairer runs on either schema.
const readRepairStateSQL = `
SELECT s.state, s.epoch, (to_jsonb(s) ->> 'policy_version')::bigint
FROM content_file_secret_lines_state s
WHERE s.singleton`

// RepairOptions tunes a Repairer. Zero values take the defaults.
type RepairOptions struct {
	// PollInterval is the wait between polls of the state row. Default 30 s.
	PollInterval time.Duration
	// MaxBackoff caps the wait after consecutive failed polls. The wait doubles
	// from PollInterval on each failure. Default 1 h.
	MaxBackoff time.Duration
	// KeepAliveInterval is the cadence of the SELECT 1 that keeps the pinned
	// lock session from idle_session_timeout during a rebuild. Default 30 s.
	KeepAliveInterval time.Duration
	// Finalize tunes the rebuild. Its Logger and Instruments default to the
	// Repairer's.
	Finalize Options
	// Logger receives the secret_lines.repair_* events. Nil discards.
	Logger *slog.Logger
	// Instruments receives eshu_dp_secret_lines_repair_runs_total. Nil records
	// nothing.
	Instruments *telemetry.Instruments
}

// Repairer rebuilds content_file_secret_lines whenever its state row is not
// ready, outside bootstrap-index (#8833 ruling-3). A migration that changes the
// derivation function moves the state to not_built with a new epoch, and reads
// stay on the legacy scan until a Repairer publishes ready for that epoch. It
// also heals a failed or abandoned bootstrap finalize.
//
// Exclusivity comes from the bulk-load advisory lock (5318,1), taken with a
// non-waiting try before the claim. Finalize's claim is not single-flight, so
// two unlocked finalizers would collide on the side table's primary key. The
// Repairer never waits on the lock, so it is never in the lock's wait graph,
// and it never takes the schema lock (5318,0), so bootstrap-index's lock order
// holds.
type Repairer struct {
	database Database
	conner   Conner
	options  RepairOptions
	failures int
}

// NewRepairer returns a Repairer that rebuilds through database and pins the
// bulk-load lock on a connection from conner. Pass the same *sql.DB behind both.
func NewRepairer(database Database, conner Conner, options RepairOptions) *Repairer {
	if options.PollInterval <= 0 {
		options.PollInterval = defaultRepairPoll
	}
	if options.MaxBackoff <= 0 {
		options.MaxBackoff = defaultRepairMaxBackoff
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	if options.Finalize.Logger == nil {
		options.Finalize.Logger = options.Logger
	}
	if options.Finalize.Instruments == nil {
		options.Finalize.Instruments = options.Instruments
	}
	return &Repairer{database: database, conner: conner, options: options}
}

// Run polls until ctx ends and returns nil then. The first poll runs at once,
// so a fresh install or a just-applied migration starts its rebuild without
// waiting a poll interval. A failed poll is logged and retried, never returned.
func (r *Repairer) Run(ctx context.Context) error {
	for {
		outcome, _ := r.Tick(ctx)
		if ctx.Err() != nil {
			return nil
		}
		timer := time.NewTimer(r.nextDelay(outcome))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// nextDelay records the outcome and returns the wait before the next poll:
// PollInterval, doubled per consecutive failure up to MaxBackoff.
func (r *Repairer) nextDelay(outcome RepairOutcome) time.Duration {
	if outcome != RepairFailed {
		r.failures = 0
		return r.options.PollInterval
	}
	r.failures++
	return backoff(r.options.PollInterval, r.options.MaxBackoff, r.failures)
}

// backoff is poll doubled failures times, capped at limit.
func backoff(poll, limit time.Duration, failures int) time.Duration {
	delay := poll
	for i := 0; i < failures && delay < limit; i++ {
		delay *= 2
	}
	return min(delay, limit)
}

// Tick runs one poll, counts its outcome, and logs a failure. The error is
// non-nil only with RepairFailed; a poll that ends because ctx was cancelled is
// RepairCancelled with a Warn event and a nil error.
func (r *Repairer) Tick(ctx context.Context) (RepairOutcome, error) {
	outcome, err := r.tick(ctx)
	if err != nil && ctx.Err() != nil {
		cancelledErr := err
		// Shutdown cancelled the poll. The state stays failed (or untouched)
		// and the next start heals it; this is not a repair failure.
		outcome, err = RepairCancelled, nil
		r.options.Logger.WarnContext(ctx, "secret lines repair cancelled by shutdown",
			telemetry.EventAttr("secret_lines.repair_cancelled"),
			"error", cancelledErr.Error())
	}
	if r.options.Instruments != nil && r.options.Instruments.SecretLinesRepairRuns != nil {
		r.options.Instruments.SecretLinesRepairRuns.Add(ctx, 1,
			metric.WithAttributes(attribute.String("outcome", string(outcome))))
	}
	if err != nil {
		r.options.Logger.ErrorContext(ctx, "secret lines repair failed",
			telemetry.EventAttr("secret_lines.repair_failed"),
			"failure_class", repairFailureClass, "error", err.Error(),
			"next_retry_seconds", backoff(r.options.PollInterval, r.options.MaxBackoff, r.failures+1).Seconds())
	}
	return outcome, err
}

func (r *Repairer) tick(ctx context.Context) (RepairOutcome, error) {
	state, epoch, _, found, err := r.readState(ctx)
	if err != nil {
		return RepairFailed, err
	}
	if !found || state == StateReady {
		r.options.Logger.DebugContext(ctx, "secret lines repair skipped",
			telemetry.EventAttr("secret_lines.repair_skipped"), "reason", "ready", "state_row", found)
		return RepairSkippedReady, nil
	}
	lock, refusal, err := TryAcquireBulkLoadLock(ctx, r.conner, LockOptions{Logger: r.options.Logger, KeepAliveInterval: r.options.KeepAliveInterval})
	if err != nil {
		return RepairFailed, err
	}
	if lock == nil {
		r.options.Logger.InfoContext(ctx, "secret lines repair skipped",
			telemetry.EventAttr("secret_lines.repair_skipped"), "reason", refusal.Reason, "lock", refusal.Lock,
			"holder", refusal.Holder, "state", state, "epoch", epoch)
		return RepairSkippedLocked, nil
	}
	outcome, runErr := r.repairLocked(ctx)
	if releaseErr := lock.Release(ctx); releaseErr != nil {
		return RepairFailed, errors.Join(runErr, releaseErr)
	}
	return outcome, runErr
}

// repairLocked re-reads the state under the lock, because a holder that just
// released may have published, and runs Finalize for the epoch it reads.
func (r *Repairer) repairLocked(ctx context.Context) (RepairOutcome, error) {
	state, epoch, policy, found, err := r.readState(ctx)
	if err != nil {
		return RepairFailed, err
	}
	if !found || state == StateReady {
		return RepairSkippedReady, nil
	}
	started := time.Now()
	startAttrs := []any{telemetry.EventAttr("secret_lines.repair_started"), "epoch", epoch, "state", state}
	if policy.Valid {
		startAttrs = append(startAttrs, "policy_version", policy.Int64)
	}
	r.options.Logger.InfoContext(ctx, "secret lines repair started", startAttrs...)
	result, err := Finalize(ctx, r.database, epoch, r.options.Finalize)
	if err != nil {
		return RepairFailed, fmt.Errorf("finalize secret lines epoch %d: %w", epoch, err)
	}
	outcome := RepairSuperseded
	if result.Claimed && result.Published {
		outcome = RepairPublished
	}
	r.options.Logger.InfoContext(ctx, "secret lines repair complete",
		telemetry.EventAttr("secret_lines.repair_complete"), "epoch", epoch, "outcome", string(outcome),
		"claimed", result.Claimed, "published", result.Published, "repositories", result.Repositories,
		"files", result.Files, "rows", result.Rows, "duration_seconds", time.Since(started).Seconds())
	return outcome, nil
}

// readState reads the state row; found is false when there is none.
func (r *Repairer) readState(ctx context.Context) (state string, epoch int64, policy sql.NullInt64, found bool, err error) {
	rows, err := r.database.QueryContext(ctx, readRepairStateSQL)
	if err != nil {
		return "", 0, policy, false, fmt.Errorf("read secret lines state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return "", 0, policy, false, rows.Err()
	}
	if err := rows.Scan(&state, &epoch, &policy); err != nil {
		return "", 0, policy, false, fmt.Errorf("scan secret lines state: %w", err)
	}
	return state, epoch, policy, true, rows.Err()
}
