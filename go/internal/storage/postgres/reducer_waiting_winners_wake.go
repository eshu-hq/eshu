// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The winners-watermark wake (#10876): the advisory waits the winners
// maintainer's watermark-advance save reopens in-transaction, and the
// contention contract around that wake.

// wakeWinnersWatermarkQuery reopens winners-watermark waits whose enqueue
// time the new watermark passed. A range reopen, not an equality lookup:
// every advisory wait older than the rebuild wakes together. $1 is the wake
// timestamp (visible_at, updated_at), $2 the new watermark.
//
// The SET list deliberately diverges from waitingWakeReopenSetClause in one
// load-bearing way: reopened_at is the covering watermark ($2), not the wake
// wall time ($1). The claim read derives the handler's bound from
// COALESCE(reopened_at, created_at), so a woken row whose cycle start is the
// wake time always sits epsilon past the stamp that woke it and re-parks on
// the next claim: wake, reclaim, re-park, forever (#10876). With the bound
// aligned to the covering stamp the next claim's recheck passes at once, and
// a retouch still moves the bound first (retouch writes reopened_at itself),
// so the later-commit invariant is unchanged.
const wakeWinnersWatermarkQuery = `
WITH due AS (
    SELECT work_item_id FROM reducer_waiting_items
    WHERE waits_for_kind = 'winners_watermark'
      AND (waits_for_key->>'enqueued_at')::timestamptz <= $2::timestamptz
    ORDER BY work_item_id COLLATE "C"
    FOR UPDATE
),
reopened AS (
    UPDATE fact_work_items AS work
    SET status = 'retrying',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $1,
        next_attempt_at = NULL,
        updated_at = $1,
        reopened_at = $2::timestamptz
    FROM due
    WHERE work.work_item_id = due.work_item_id
      AND work.stage = 'reducer'
      AND work.status = 'retrying'
    RETURNING work.work_item_id
),
cleared AS (
    DELETE FROM reducer_waiting_items AS wait
    USING reopened
    WHERE wait.work_item_id = reopened.work_item_id
      AND wait.waits_for_kind = 'winners_watermark'
    RETURNING 1
)
SELECT (SELECT count(*) FROM reopened), (SELECT count(*) FROM cleared)
`

// defaultWinnersWakeLockTimeout bounds how long the winners watermark-advance
// save waits on a parked consumer row lock (#10876), mirroring the #7597
// activation wake. A 55P03 fails the tick retryably; the maintainer re-ticks
// on its ordinary cadence and the stamp is idempotent.
const defaultWinnersWakeLockTimeout = 2 * time.Second

// winnersWakeLockTimeoutSQL pins the providing save's lock wait before the
// wake runs. SET LOCAL keeps it inside the tick's transaction.
const winnersWakeLockTimeoutSQL = `SELECT set_config('lock_timeout', $1, true)`

// WinnersWakeContentionError reports a winners watermark-advance save that
// hit lock_timeout (55P03) or a deadlock (40P01) around the in-tx wake and
// rolled back. It is Retryable with no class, so the tick retries on the
// maintainer's ordinary cadence without touching any queue budget; the stamp
// and the wake are both idempotent.
type WinnersWakeContentionError struct {
	err error
}

// Error names the contention.
func (e WinnersWakeContentionError) Error() string {
	return fmt.Sprintf("wake winners waiting items: lock contention, retrying: %v", e.err)
}

// Unwrap exposes the driver error.
func (e WinnersWakeContentionError) Unwrap() error { return e.err }

// Retryable opts the rolled-back tick into the maintainer's next cadence.
func (WinnersWakeContentionError) Retryable() bool { return true }

// classifyWinnersWakeError maps lock_timeout and deadlock failures of the
// winners in-tx wake to the retryable contention error and returns every
// other error unchanged.
func classifyWinnersWakeError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "55P03" || pgErr.Code == "40P01") {
		return WinnersWakeContentionError{err: err}
	}
	return err
}

// wakeWinnersWatermark reopens every winners wait the rebuild's watermark
// passed. The waiting set is narrow (advisory only) and the maintainer
// commits on a 30-second cadence, so the coarse range scan stays inside the
// provider transaction within the measured ~16ms budget (theory proof a).
func wakeWinnersWatermark(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	site string,
	watermark time.Time,
	now time.Time,
) (int64, error) {
	var reopenedCount, clearedCount int64
	if err := querySingleRow(ctx, database, wakeWinnersWatermarkQuery,
		[]any{now, watermark}, &reopenedCount, &clearedCount); err != nil {
		recordWaitingWake(ctx, instruments, site, telemetry.WaitingWakeOutcomeError)
		return 0, fmt.Errorf("wake winners waiting items: %w", err)
	}
	outcome := telemetry.WaitingWakeOutcomeNoop
	if reopenedCount > 0 {
		outcome = telemetry.WaitingWakeOutcomeReopened
	}
	recordWaitingWake(ctx, instruments, site, outcome)
	return reopenedCount, nil
}

// recordWaitingWake counts one waiting-wake statement on
// eshu_dp_reducer_waiting_wake_total by site and outcome. Nil instruments are
// a no-op.
func recordWaitingWake(ctx context.Context, instruments *telemetry.Instruments, site, outcome string) {
	if instruments == nil || instruments.ReducerWaitingWake == nil {
		return
	}
	instruments.ReducerWaitingWake.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrWaitingWakeSite(site),
		telemetry.AttrOutcome(outcome),
	))
}
