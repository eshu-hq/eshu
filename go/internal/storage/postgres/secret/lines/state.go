// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// beginDeferralSQL opens a new epoch and takes readiness away in one
// autocommit statement, so the state is durable before the caller's first
// deferred write. The epoch always increments: a finalizer that claimed an
// older epoch can then never publish ready over this load's skipped writes.
const beginDeferralSQL = `
INSERT INTO content_file_secret_lines_state (singleton, state, epoch, updated_at)
VALUES (TRUE, 'not_built', 1, clock_timestamp())
ON CONFLICT (singleton) DO UPDATE
SET state = 'not_built',
    epoch = content_file_secret_lines_state.epoch + 1,
    build_started_at = NULL,
    build_completed_at = NULL,
    failed_at = NULL,
    failure_class = '',
    updated_at = clock_timestamp()
RETURNING epoch`

// claimSQL moves the current epoch to building. It claims nothing when the
// epoch moved on (a newer bulk load began) or the epoch is already ready.
const claimSQL = `
UPDATE content_file_secret_lines_state
SET state = 'building',
    build_started_at = clock_timestamp(),
    build_completed_at = NULL,
    failed_at = NULL,
    failure_class = '',
    updated_at = clock_timestamp()
WHERE singleton = TRUE
  AND epoch = $1
  AND state <> 'ready'`

// publishReadySQL is epoch-fenced and only leaves building: a newer bulk load
// (epoch bump) keeps readers on the legacy scan.
const publishReadySQL = `
UPDATE content_file_secret_lines_state
SET state = 'ready',
    build_completed_at = clock_timestamp(),
    failed_at = NULL,
    failure_class = '',
    updated_at = clock_timestamp()
WHERE singleton = TRUE
  AND epoch = $1
  AND state = 'building'`

const publishFailedSQL = `
UPDATE content_file_secret_lines_state
SET state = 'failed',
    failed_at = clock_timestamp(),
    failure_class = 'backfill_failed',
    updated_at = clock_timestamp()
WHERE singleton = TRUE
  AND epoch = $1
  AND state = 'building'`

// BeginDeferral opens a bulk-load epoch and returns it. Call it, and wait for
// it to return, before the first write from a DeferredSessionSQL connection: it
// turns readiness off so readers fall back to the legacy scan while the side
// table is knowingly incomplete. Pass the returned epoch to Finalize.
func BeginDeferral(ctx context.Context, queryer db.Queryer) (int64, error) {
	rows, err := queryer.QueryContext(ctx, beginDeferralSQL)
	if err != nil {
		return 0, fmt.Errorf("begin secret lines deferral: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, errors.Join(errors.New("begin secret lines deferral: no epoch returned"), rows.Err())
	}
	var epoch int64
	if err := rows.Scan(&epoch); err != nil {
		return 0, fmt.Errorf("scan secret lines epoch: %w", err)
	}
	return epoch, rows.Err()
}

func claim(ctx context.Context, exec db.Executor, epoch int64) (bool, error) {
	result, err := exec.ExecContext(ctx, claimSQL, epoch)
	if err != nil {
		return false, fmt.Errorf("claim secret lines finalization: %w", err)
	}
	claimed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read secret lines claim: %w", err)
	}
	return claimed == 1, nil
}

func publishReady(ctx context.Context, exec db.Executor, epoch int64) (bool, error) {
	result, err := exec.ExecContext(ctx, publishReadySQL, epoch)
	if err != nil {
		return false, fmt.Errorf("publish secret lines ready: %w", err)
	}
	published, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read secret lines ready publication: %w", err)
	}
	return published == 1, nil
}

// publishFailed records a failed finalization with its own short deadline so a
// cancelled or timed-out finalizer context still leaves a durable 'failed'.
func publishFailed(exec db.Executor, epoch int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := exec.ExecContext(ctx, publishFailedSQL, epoch); err != nil {
		return fmt.Errorf("publish secret lines failed: %w", err)
	}
	return nil
}
