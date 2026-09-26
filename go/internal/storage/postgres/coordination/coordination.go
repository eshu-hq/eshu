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
// holder is logged while waiting. Subject names what is being owned in the
// messages and Lock labels the events' lock attribute; empty values mean the
// schema bootstrap lock ("schema bootstrap" and "schema"), so the #6956 texts
// stay byte-identical. The secret-lines bulk-load run lock (#7125) passes
// "secret lines bulk load" and "bulk_load".
type OwnershipPolicy struct {
	Wait     time.Duration
	Poll     time.Duration
	LogEvery time.Duration
	Subject  string
	Lock     string
}

const (
	defaultOwnershipSubject = "schema bootstrap"
	defaultOwnershipLock    = "schema"
)

// LockRetryPolicy bounds RetryOnLockTimeout: the backoff doubles from
// InitialBackoff up to MaxBackoff, and the statement fails once the next
// backoff would end after Deadline. Deadline is wall clock and shared by
// every statement of one bootstrap run, so it counts the lock_timeout each
// failed attempt waited as well as the sleeps; Budget is the duration the
// deadline was derived from, named in the failure.
type LockRetryPolicy struct {
	Budget         time.Duration
	Deadline       time.Time
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
// on the first failed poll and then every policy.LogEvery, and fails once
// policy.Wait is spent so a stuck owner cannot hang a bootstrapper forever.
// Polling has no queue order: two waiters race for the lock when it frees,
// and each is bounded only by its own wait. now is injectable for tests.
func WaitForOwnership(
	ctx context.Context,
	logger *slog.Logger,
	locker Locker,
	policy OwnershipPolicy,
	sleep Sleeper,
	now func() time.Time,
) error {
	subject, lockName := policy.Subject, policy.Lock
	if subject == "" {
		subject = defaultOwnershipSubject
	}
	if lockName == "" {
		lockName = defaultOwnershipLock
	}
	started := now()
	var lastLog time.Time
	polls := 0
	for {
		locked, err := locker.TryLock(ctx)
		if err != nil {
			return fmt.Errorf("acquire %s ownership: %w", subject, err)
		}
		if locked {
			if polls > 0 {
				logger.InfoContext(ctx, "postgres "+subject+" ownership acquired after waiting",
					telemetry.EventAttr("bootstrap.postgres.ownership.acquired"),
					"lock", lockName,
					"waited_ms", now().Sub(started).Milliseconds(),
					"polls", polls,
				)
			}
			return nil
		}
		polls++
		current := now()
		waited := current.Sub(started)
		if waited > policy.Wait {
			return fmt.Errorf("acquire %s ownership: another bootstrapper (%s) held it for more than %s", subject, describeHolder(ctx, locker), policy.Wait)
		}
		// The holder is queried only when it is about to be logged: on the
		// first failed poll and then every LogEvery.
		if lastLog.IsZero() || current.Sub(lastLog) >= policy.LogEvery {
			logger.InfoContext(ctx, "postgres "+subject+" waiting for ownership",
				telemetry.EventAttr("bootstrap.postgres.ownership.waiting"),
				"lock", lockName,
				"holder", describeHolder(ctx, locker),
				"waited_ms", waited.Milliseconds(),
				"wait_ms", policy.Wait.Milliseconds(),
			)
			lastLog = current
		}
		if err := sleep(ctx, policy.Poll); err != nil {
			return fmt.Errorf("acquire %s ownership: %w", subject, err)
		}
	}
}

func describeHolder(ctx context.Context, locker Locker) string {
	holder, err := locker.DescribeHolder(ctx)
	if err != nil {
		return "unknown (" + err.Error() + ")"
	}
	return holder
}

// RetryOnLockTimeout runs exec until it succeeds, fails for a reason other
// than lock_timeout, or the run's retry deadline would pass during the next
// backoff. Only SQLSTATE 55P03 is retried: that statement never acquired
// its lock, so nothing was applied. now is injectable for tests.
func RetryOnLockTimeout(
	ctx context.Context,
	logger *slog.Logger,
	path string,
	policy LockRetryPolicy,
	sleep Sleeper,
	now func() time.Time,
	exec func() error,
) error {
	backoff := policy.InitialBackoff
	started := now()
	for attempt := 1; ; attempt++ {
		err := exec()
		if err == nil {
			if attempt > 1 {
				logger.InfoContext(ctx, "postgres schema migration acquired its lock after retrying",
					telemetry.EventAttr("bootstrap.postgres.migration.lock_recovered"),
					"path", path,
					"attempts", attempt,
					"waited_ms", now().Sub(started).Milliseconds(),
				)
			}
			return nil
		}
		if !IsLockNotAvailable(err) {
			return err
		}
		current := now()
		if current.Add(backoff).After(policy.Deadline) {
			return fmt.Errorf("lock retry budget %s for this bootstrap run exhausted after %d attempts on %s (%s left): %w",
				policy.Budget, attempt, path, policy.Deadline.Sub(current).Round(time.Millisecond), err)
		}
		logger.WarnContext(ctx, "postgres schema migration waiting for a lock held by another session",
			telemetry.EventAttr("bootstrap.postgres.migration.lock_wait"),
			"path", path,
			"attempt", attempt,
			"backoff_ms", backoff.Milliseconds(),
			"waited_ms", current.Sub(started).Milliseconds(),
			"budget_left_ms", policy.Deadline.Sub(current).Milliseconds(),
			"error", err.Error(),
		)
		if err := sleep(ctx, backoff); err != nil {
			return fmt.Errorf("retry %s after lock timeout: %w", path, err)
		}
		backoff = min(backoff*2, policy.MaxBackoff)
	}
}
