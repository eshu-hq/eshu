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
	postgresdb "github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	packageManifestBackfillAdvisoryLockClass = 5318
	packageManifestBackfillAdvisoryLockID    = 7088
	packageManifestBackfillFastPollInterval  = time.Second
	packageManifestBackfillIdlePollInterval  = 30 * time.Second
	packageManifestBackfillUnlockTimeout     = 5 * time.Second
)

// startPackageManifestConsumptionKeyBackfill starts the reducer-owned repair
// loop and returns a wait function for shutdown before the database closes.
func startPackageManifestConsumptionKeyBackfill(
	ctx context.Context,
	run func(context.Context) error,
	logger *slog.Logger,
) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := run(ctx); err != nil && ctx.Err() == nil && logger != nil {
			logger.Error("package manifest consumption key backfill stopped", "error", err)
		}
	}()
	return func() { <-done }
}

// runPackageManifestConsumptionKeyBackfill periodically repairs old-writer
// dirty scopes. Each pass has one elected reducer owner across replicas.
func runPackageManifestConsumptionKeyBackfill(ctx context.Context, database *sql.DB, instruments *telemetry.Instruments, logger *slog.Logger) error {
	for {
		var ready bool
		var dirtyScopes, cursorUpdated int64
		var sampleErr error
		started := time.Now()
		ran, err := withPackageManifestBackfillLeadership(ctx, database, func(passCtx context.Context, conn *sql.Conn) error {
			bound := packageManifestBackfillConn{conn: conn}
			if err := postgres.BackfillPackageManifestConsumptionKeys(passCtx, bound); err != nil {
				return err
			}
			var readyErr error
			ready, readyErr = postgres.PackageManifestConsumptionKeysReady(passCtx, bound)
			if readyErr != nil {
				return readyErr
			}
			if err := conn.QueryRowContext(passCtx, `SELECT count(*) FROM (
    SELECT 1 FROM package_manifest_consumption_key_dirty_scopes LIMIT 26
) AS bounded`).Scan(&dirtyScopes); err != nil {
				sampleErr = fmt.Errorf("sample package manifest dirty scopes: %w", err)
				return nil
			}
			if err := conn.QueryRowContext(passCtx, `SELECT COALESCE((
    SELECT EXTRACT(EPOCH FROM updated_at)::bigint
    FROM package_manifest_consumption_key_backfill_progress
    WHERE marker_name = $1
), 0)`, postgres.PackageManifestConsumptionKeyBackfillMarker).Scan(&cursorUpdated); err != nil {
				sampleErr = fmt.Errorf("sample package manifest backfill cursor: %w", err)
			}
			return nil
		})
		if ctx.Err() == nil {
			recordPackageManifestBackfillPass(ctx, instruments, ran, ready, err, time.Since(started))
			if ran && err == nil && sampleErr == nil && instruments != nil {
				instruments.PackageManifestBackfillDirtyScopes.Record(ctx, dirtyScopes)
				instruments.PackageManifestBackfillCursorUpdated.Record(ctx, cursorUpdated)
			}
			if logger != nil {
				if err != nil {
					logger.Error("package manifest consumption key backfill pass failed", "error", err, "duration_seconds", time.Since(started).Seconds())
				} else if !ran {
					logger.Info("package manifest consumption key backfill contended", "duration_seconds", time.Since(started).Seconds())
				} else if sampleErr != nil {
					logger.Warn("package manifest consumption key backfill progress sample failed", "error", sampleErr, "ready", ready, "duration_seconds", time.Since(started).Seconds())
				} else {
					logger.Info("package manifest consumption key backfill pass", "ready", ready, "dirty_scopes_capped", dirtyScopes, "cursor_updated_unixtime", cursorUpdated, "duration_seconds", time.Since(started).Seconds())
				}
			}
		}
		timer := time.NewTimer(packageManifestBackfillPollInterval(ran, ready, err))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// recordPackageManifestBackfillPass reports every election, including contention
// and failures, with a closed outcome vocabulary.
func recordPackageManifestBackfillPass(
	ctx context.Context,
	instruments *telemetry.Instruments,
	ran, ready bool,
	passErr error,
	duration time.Duration,
) {
	if instruments == nil {
		return
	}
	outcome := "contended"
	if passErr != nil {
		outcome = "failed"
	} else if ran && ready {
		outcome = "ready"
	} else if ran {
		outcome = "incomplete"
	}
	attrs := metric.WithAttributes(attribute.String(telemetry.MetricDimensionOutcome, outcome))
	instruments.PackageManifestBackfillPasses.Add(ctx, 1, attrs)
	instruments.PackageManifestBackfillDuration.Record(ctx, duration.Seconds(), attrs)
	if ran && passErr == nil {
		instruments.PackageManifestBackfillLastSuccess.Record(ctx, time.Now().Unix())
		var readyValue int64
		if ready {
			readyValue = 1
		}
		instruments.PackageManifestBackfillReady.Record(ctx, readyValue)
	}
}

// packageManifestBackfillPollInterval advances initial backfill promptly while
// limiting idle, contended, and failed passes to the normal repair cadence.
func packageManifestBackfillPollInterval(ran, ready bool, err error) time.Duration {
	if ran && !ready && err == nil {
		return packageManifestBackfillFastPollInterval
	}
	return packageManifestBackfillIdlePollInterval
}

// withPackageManifestBackfillLeadership holds a session advisory lock on a
// dedicated connection while the repair pass runs its transactions on that session.
// A contending reducer skips this pass and retries on the next poll.
func withPackageManifestBackfillLeadership(
	ctx context.Context,
	database *sql.DB,
	backfill func(context.Context, *sql.Conn) error,
) (ran bool, err error) {
	conn, err := database.Conn(ctx)
	if err != nil {
		return false, fmt.Errorf("open package manifest backfill lock connection: %w", err)
	}
	defer func() { err = errors.Join(err, conn.Close()) }()

	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1, $2)",
		packageManifestBackfillAdvisoryLockClass, packageManifestBackfillAdvisoryLockID).Scan(&locked); err != nil {
		// The server may have granted the lock before the response failed.
		// Discard this session rather than return an uncertain lock to the pool.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return false, fmt.Errorf("acquire package manifest backfill lock: %w", err)
	}
	if !locked {
		return false, nil
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), packageManifestBackfillUnlockTimeout)
		defer cancel()
		var unlocked bool
		unlockErr := conn.QueryRowContext(unlockCtx, "SELECT pg_advisory_unlock($1, $2)",
			packageManifestBackfillAdvisoryLockClass, packageManifestBackfillAdvisoryLockID).Scan(&unlocked)
		if unlockErr != nil || !unlocked {
			// The session must not return to the pool while it may still hold
			// the lock; a later try on that session would be reentrant.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			if unlockErr != nil {
				err = errors.Join(err, fmt.Errorf("release package manifest backfill lock: %w", unlockErr))
			} else {
				err = errors.Join(err, errors.New("release package manifest backfill lock: session did not hold lock"))
			}
		}
	}()
	return true, backfill(ctx, conn)
}

// packageManifestBackfillConn keeps the backfill's transactions on the same
// session as its advisory lock, including when the pool permits one connection.
type packageManifestBackfillConn struct{ conn *sql.Conn }

func (c packageManifestBackfillConn) QueryContext(ctx context.Context, query string, args ...any) (postgresdb.Rows, error) {
	return c.conn.QueryContext(ctx, query, args...)
}

func (c packageManifestBackfillConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.conn.ExecContext(ctx, query, args...)
}

func (c packageManifestBackfillConn) Begin(ctx context.Context) (postgresdb.Transaction, error) {
	tx, err := c.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return postgres.SQLTx{Tx: tx}, nil
}
