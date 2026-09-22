// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordination

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Locker is the schema ownership lock as the wait loop sees it: one try per
// poll, plus a description of whoever holds it for the operator log. The
// postgres root package implements it on the bootstrap session.
type Locker interface {
	TryLock(context.Context) (bool, error)
	DescribeHolder(context.Context) (string, error)
}

// OwnershipPolicy bounds WaitForOwnership: Wait is the total time to wait
// for another bootstrapper, Poll the try cadence, LogEvery how often the
// holder is logged while waiting.
type OwnershipPolicy struct {
	Wait     time.Duration
	Poll     time.Duration
	LogEvery time.Duration
}

// LockRetryPolicy bounds RetryOnLockTimeout: the backoff doubles from
// InitialBackoff up to MaxBackoff, and the statement fails once the next
// backoff would push the total slept past Budget.
type LockRetryPolicy struct {
	Budget         time.Duration
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// Sleeper waits for d or until ctx ends; tests inject a recorder.
type Sleeper func(ctx context.Context, d time.Duration) error

// SleepContext is the production Sleeper.
func SleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// IsLockNotAvailable reports SQLSTATE 55P03, raised when lock_timeout expires
// while a statement waits for a lock. The statement acquired nothing, so it
// applied nothing and can be re-run.
func IsLockNotAvailable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "55P03"
}

// WaitForOwnership polls locker until the lock is taken, logging the holder
// while it waits, and fails once policy.Wait is spent so a stuck owner cannot
// hang a bootstrapper forever. now is injectable for tests.
func WaitForOwnership(
	ctx context.Context,
	logger *slog.Logger,
	locker Locker,
	policy OwnershipPolicy,
	sleep Sleeper,
	now func() time.Time,
) error {
	started := now()
	var lastLog time.Time
	polls := 0
	for {
		locked, err := locker.TryLock(ctx)
		if err != nil {
			return fmt.Errorf("acquire schema bootstrap ownership: %w", err)
		}
		if locked {
			if polls > 0 {
				logger.InfoContext(ctx, "postgres schema bootstrap ownership acquired after waiting",
					telemetry.EventAttr("bootstrap.postgres.ownership.acquired"),
					"waited_ms", now().Sub(started).Milliseconds(),
					"polls", polls,
				)
			}
			return nil
		}
		polls++
		current := now()
		waited := current.Sub(started)
		holder, holderErr := locker.DescribeHolder(ctx)
		if holderErr != nil {
			holder = "unknown (" + holderErr.Error() + ")"
		}
		if waited > policy.Wait {
			return fmt.Errorf("acquire schema bootstrap ownership: another bootstrapper (%s) held it for more than %s", holder, policy.Wait)
		}
		if lastLog.IsZero() || current.Sub(lastLog) >= policy.LogEvery {
			logger.InfoContext(ctx, "postgres schema bootstrap waiting for ownership",
				telemetry.EventAttr("bootstrap.postgres.ownership.waiting"),
				"holder", holder,
				"waited_ms", waited.Milliseconds(),
				"wait_ms", policy.Wait.Milliseconds(),
			)
			lastLog = current
		}
		if err := sleep(ctx, policy.Poll); err != nil {
			return fmt.Errorf("acquire schema bootstrap ownership: %w", err)
		}
	}
}

// RetryOnLockTimeout runs exec until it succeeds, fails for a reason other
// than lock_timeout, or the retry budget is spent. Only SQLSTATE 55P03 is
// retried: that statement never acquired its lock, so nothing was applied.
func RetryOnLockTimeout(
	ctx context.Context,
	logger *slog.Logger,
	path string,
	policy LockRetryPolicy,
	sleep Sleeper,
	exec func() error,
) error {
	backoff := policy.InitialBackoff
	var slept time.Duration
	for attempt := 1; ; attempt++ {
		err := exec()
		if err == nil {
			if attempt > 1 {
				logger.InfoContext(ctx, "postgres schema migration acquired its lock after retrying",
					telemetry.EventAttr("bootstrap.postgres.migration.lock_recovered"),
					"path", path,
					"attempts", attempt,
					"slept_ms", slept.Milliseconds(),
				)
			}
			return nil
		}
		if !IsLockNotAvailable(err) {
			return err
		}
		if slept+backoff > policy.Budget {
			return fmt.Errorf("lock retry budget %s exhausted after %d attempts on %s: %w", policy.Budget, attempt, path, err)
		}
		logger.WarnContext(ctx, "postgres schema migration waiting for a lock held by another session",
			telemetry.EventAttr("bootstrap.postgres.migration.lock_wait"),
			"path", path,
			"attempt", attempt,
			"backoff_ms", backoff.Milliseconds(),
			"slept_ms", slept.Milliseconds(),
			"budget_ms", policy.Budget.Milliseconds(),
			"error", err.Error(),
		)
		if err := sleep(ctx, backoff); err != nil {
			return fmt.Errorf("retry %s after lock timeout: %w", path, err)
		}
		slept += backoff
		backoff = min(backoff*2, policy.MaxBackoff)
	}
}
