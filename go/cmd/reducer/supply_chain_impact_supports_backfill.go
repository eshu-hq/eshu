// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	supportsBackfillAdvisoryLockClass = 5319
	supportsBackfillAdvisoryLockID    = 10880
	supportsBackfillUnlockTimeout     = 5 * time.Second
	// supportsBackfillPassSteps bounds the steps one leadership pass runs, so a
	// long sweep hands the election back regularly.
	supportsBackfillPassSteps = 200
	// supportsBackfillPassBudget bounds one pass in wall time.
	supportsBackfillPassBudget = time.Minute

	supportsBackfillFastPoll = 500 * time.Millisecond
	// supportsBackfillIdlePoll paces a finished, contended, or failed pass. A
	// finished sweep costs a marker read and a dirty-scope check per poll, and
	// that poll is also how soon a scope an old writer marked gets drained.
	supportsBackfillIdlePoll = 30 * time.Second
)

// startSupplyChainImpactSupportsBackfill starts the reducer-owned supports
// backfill and returns a wait function for shutdown before the database closes.
func startSupplyChainImpactSupportsBackfill(ctx context.Context, run func(context.Context) error, logger *slog.Logger) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := run(ctx); err != nil && ctx.Err() == nil && logger != nil {
			logger.Error("supply chain impact supports backfill stopped", "error", err)
		}
	}()
	return func() { <-done }
}

// runSupplyChainImpactSupportsBackfill repeats the backfill until the readiness
// marker is set, then idles on a marker read. Each pass has one elected reducer
// owner across replicas; a replica that loses the election waits for the next
// poll.
func runSupplyChainImpactSupportsBackfill(ctx context.Context, database *sql.DB, instruments *telemetry.Instruments, logger *slog.Logger) error {
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: database}}
	for {
		started := time.Now()
		complete := false
		ran, err := withSupportsBackfillLeadership(ctx, database, func(passCtx context.Context) error {
			var passErr error
			complete, passErr = runSupportsBackfillPass(passCtx, backfill.RunStep, instruments, logger)
			return passErr
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		switch {
		case err != nil:
			recordSupportsBackfillStep(ctx, instruments, telemetry.SupplyChainImpactSupportsBackfillStepFailed)
			if logger != nil {
				logger.Error("supply chain impact supports backfill pass failed", "error", err, "duration_seconds", time.Since(started).Seconds())
			}
		case !ran:
			recordSupportsBackfillStep(ctx, instruments, telemetry.SupplyChainImpactSupportsBackfillStepContended)
		}
		wait := supportsBackfillIdlePoll
		if ran && err == nil && !complete {
			wait = supportsBackfillFastPoll
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// runSupportsBackfillPass runs steps until the backfill completes, the step
// budget or the time budget runs out, or a step fails. It records telemetry for
// every step and reports whether the marker is set.
func runSupportsBackfillPass(
	ctx context.Context,
	step func(context.Context) (postgres.SupplyChainImpactSupportsBackfillReport, error),
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (bool, error) {
	deadline := time.Now().Add(supportsBackfillPassBudget)
	for i := 0; i < supportsBackfillPassSteps && time.Now().Before(deadline); i++ {
		stepStarted := time.Now()
		report, err := step(ctx)
		if err != nil {
			return false, err
		}
		outcome := supportsBackfillOutcome(report)
		recordSupportsBackfillStep(ctx, instruments, outcome)
		instruments.RecordSupplyChainImpactSupportsBackfillStepResult(ctx, outcome, time.Since(stepStarted), report.DirtyScopes)
		instruments.RecordSupplyChainImpactSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionUnchanged,
			telemetry.SupplyChainImpactSupportsSourceBackfill, report.Unchanged)
		instruments.RecordSupplyChainImpactSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionInserted,
			telemetry.SupplyChainImpactSupportsSourceBackfill, report.Written)
		instruments.RecordSupplyChainImpactSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionDeleted,
			telemetry.SupplyChainImpactSupportsSourceBackfill, report.Removed)
		if outcome == telemetry.SupplyChainImpactSupportsBackfillStepGap && logger != nil {
			logger.Warn("supply chain impact supports backfill verification found live findings without a support; restarting the sweep",
				"missing_capped", report.Missing)
		}
		if report.Complete && report.DirtyScopes == 0 {
			instruments.RecordSupplyChainImpactSupportsBackfillReady(ctx, true)
			if outcome == telemetry.SupplyChainImpactSupportsBackfillStepComplete && logger != nil {
				logger.Info("supply chain impact supports backfill complete")
			}
			return true, nil
		}
		instruments.RecordSupplyChainImpactSupportsBackfillReady(ctx, false)
	}
	return false, nil
}

// supportsBackfillOutcome classifies one step into the closed outcome set.
func supportsBackfillOutcome(report postgres.SupplyChainImpactSupportsBackfillReport) string {
	switch {
	case report.Drained > 0:
		return telemetry.SupplyChainImpactSupportsBackfillStepDrain
	case report.Verified && report.Missing > 0:
		return telemetry.SupplyChainImpactSupportsBackfillStepGap
	case report.Complete && report.Verified:
		return telemetry.SupplyChainImpactSupportsBackfillStepComplete
	case report.Complete:
		return telemetry.SupplyChainImpactSupportsBackfillStepIdle
	default:
		return telemetry.SupplyChainImpactSupportsBackfillStepChunk
	}
}

func recordSupportsBackfillStep(ctx context.Context, instruments *telemetry.Instruments, outcome string) {
	instruments.RecordSupplyChainImpactSupportsBackfillStep(ctx, outcome)
}

// withSupportsBackfillLeadership holds a session advisory lock on a dedicated
// connection while fn runs. The lock only elects the one reducer that sweeps; the
// steps run on the normal pool, each under the conflict-domain locks that keep
// them safe against writers. A contending reducer skips the pass.
func withSupportsBackfillLeadership(ctx context.Context, database *sql.DB, fn func(context.Context) error) (ran bool, err error) {
	conn, err := database.Conn(ctx)
	if err != nil {
		return false, fmt.Errorf("open supports backfill lock connection: %w", err)
	}
	defer func() { err = errors.Join(err, conn.Close()) }()

	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1, $2)",
		supportsBackfillAdvisoryLockClass, supportsBackfillAdvisoryLockID).Scan(&locked); err != nil {
		// The server may have granted the lock before the response failed.
		// Discard this session rather than return an uncertain lock to the pool.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return false, fmt.Errorf("acquire supports backfill lock: %w", err)
	}
	if !locked {
		return false, nil
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), supportsBackfillUnlockTimeout)
		defer cancel()
		var unlocked bool
		unlockErr := conn.QueryRowContext(unlockCtx, "SELECT pg_advisory_unlock($1, $2)",
			supportsBackfillAdvisoryLockClass, supportsBackfillAdvisoryLockID).Scan(&unlocked)
		if unlockErr != nil || !unlocked {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			if unlockErr != nil {
				err = errors.Join(err, fmt.Errorf("release supports backfill lock: %w", unlockErr))
			} else {
				err = errors.Join(err, errors.New("release supports backfill lock: session did not hold lock"))
			}
		}
	}()
	return true, fn(ctx)
}
