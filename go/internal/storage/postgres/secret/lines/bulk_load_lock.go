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

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Advisory lock key of the bulk-load run. Class 5318 is the bootstrap family
// (5318,0 is the schema bootstrap lock in the postgres root package); object 1
// is the deferred secret-lines bulk load. The lock is session-scoped and
// per-database: released by pg_advisory_unlock or when its session ends.
const (
	BulkLoadLockClass = 5318
	BulkLoadLockID    = 1
)

const (
	defaultLockWait     = 3 * time.Minute
	defaultLockPoll     = time.Second
	defaultLockLogEvery = 15 * time.Second
	unlockTimeout       = 2 * time.Second
)

// Conner opens the dedicated connection that pins the run lock; *sql.DB
// satisfies it.
type Conner interface {
	Conn(context.Context) (*sql.Conn, error)
}

// LockOptions tunes AcquireBulkLoadLock. Zero values take the defaults.
type LockOptions struct {
	// Wait bounds how long a second run waits for the holder before failing.
	// bootstrap-index passes the schema ownership wait
	// (ESHU_SCHEMA_BOOTSTRAP_OWNERSHIP_WAIT) so one knob sets both bounds. The
	// two waits run in sequence, so the worst case is twice that value.
	Wait time.Duration
	// Poll is the pg_try_advisory_lock cadence.
	Poll time.Duration
	// LogEvery is how often the holder is logged while waiting.
	LogEvery time.Duration
	// Logger receives the acquired and released events. Nil discards.
	Logger *slog.Logger
}

// BulkLoadLock is the run-scoped exclusivity of one deferred bulk load. It pins
// one connection for the whole run so the session advisory lock outlives every
// pool checkout and dies with its backend if the process is killed.
type BulkLoadLock struct {
	conn     *sql.Conn
	pid      int
	logger   *slog.Logger
	acquired time.Time
}

// AcquireBulkLoadLock takes the bulk-load run lock on a dedicated connection,
// waiting up to options.Wait for a running bulk load to finish and then failing
// with an error that names the holder's backend. Hold it from before
// BeginDeferral until after Finalize: the epoch fence only protects against a
// finalizer that outlives its own epoch, so two loads that overlap between
// BeginDeferral and Finalize would let the first finalizer publish ready over
// rows the second is still writing without derivation.
//
// Lock order is fixed: this lock is taken before the schema bootstrap lock
// (5318,0) and never while holding it, and both waiters poll a try-lock while
// holding nothing, so neither is ever in the database's wait-for graph.
func AcquireBulkLoadLock(ctx context.Context, database Conner, options LockOptions) (*BulkLoadLock, error) {
	if options.Wait <= 0 {
		options.Wait = defaultLockWait
	}
	if options.Poll <= 0 {
		options.Poll = defaultLockPoll
	}
	if options.LogEvery <= 0 {
		options.LogEvery = defaultLockLogEvery
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	conn, err := database.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open secret lines bulk load connection: %w", err)
	}
	var pid int
	if err := conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read secret lines bulk load backend pid: %w", err)
	}
	locker := &countingLocker{inner: bulkLoadLocker{conn: conn}}
	started := time.Now()
	if err := coordination.WaitForOwnership(ctx, options.Logger, locker, coordination.OwnershipPolicy{
		Wait:     options.Wait,
		Poll:     options.Poll,
		LogEvery: options.LogEvery,
		Subject:  "secret lines bulk load",
		Lock:     "bulk_load",
	}, coordination.SleepContext, time.Now); err != nil {
		_ = conn.Close()
		return nil, err
	}
	lock := &BulkLoadLock{conn: conn, pid: pid, logger: options.Logger, acquired: time.Now()}
	options.Logger.InfoContext(ctx, "secret lines bulk load lock acquired",
		telemetry.EventAttr("secret_lines.bulk_load_lock_acquired"),
		"pid", pid, "waited_ms", time.Since(started).Milliseconds(), "polls", locker.polls-1)
	return lock, nil
}

// Release unlocks and closes the pinned connection. A false unlock or an error
// means the session that held the lock is gone, so exclusivity was void for
// part of the run: the caller must fail the run loudly (a rerun rebuilds the
// table and heals it), never swallow the error.
func (l *BulkLoadLock) Release(ctx context.Context) error {
	// The run's context may already be cancelled; the unlock gets its own
	// deadline, like the schema lock's release.
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()
	var released bool
	unlockErr := l.conn.QueryRowContext(unlockCtx, "SELECT pg_advisory_unlock($1, $2)",
		BulkLoadLockClass, BulkLoadLockID).Scan(&released)
	closeErr := l.conn.Close()
	switch {
	case unlockErr != nil:
		unlockErr = fmt.Errorf("release secret lines bulk load lock (backend %d): %w", l.pid, unlockErr)
	case !released:
		unlockErr = fmt.Errorf("release secret lines bulk load lock: backend %d no longer held it, so a concurrent bulk load was possible during this run", l.pid)
	default:
		l.logger.InfoContext(ctx, "secret lines bulk load lock released",
			telemetry.EventAttr("secret_lines.bulk_load_lock_released"),
			"pid", l.pid, "held_seconds", time.Since(l.acquired).Seconds())
	}
	return errors.Join(unlockErr, closeErr)
}

// bulkLoadLocker is the coordination.Locker for the run lock: one
// non-blocking try per poll, and the granted holder for the operator log.
type bulkLoadLocker struct{ conn *sql.Conn }

func (l bulkLoadLocker) TryLock(ctx context.Context) (bool, error) {
	var locked bool
	err := l.conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1, $2)",
		BulkLoadLockClass, BulkLoadLockID).Scan(&locked)
	return locked, err
}

// DescribeHolder names the backend(s) holding the lock in this database, the
// same shape the schema lock's holder diagnostic uses. Advisory locks are
// per database, so the query filters on the current one.
func (l bulkLoadLocker) DescribeHolder(ctx context.Context) (string, error) {
	var holder sql.NullString
	err := l.conn.QueryRowContext(ctx, `
SELECT string_agg(format('pid=%s application_name=%s state=%s connected_for=%s',
    a.pid, coalesce(a.application_name, ''), coalesce(a.state, ''),
    date_trunc('second', now() - a.backend_start)), '; ' ORDER BY a.pid)
FROM pg_locks l
JOIN pg_stat_activity a ON a.pid = l.pid
WHERE l.locktype = 'advisory' AND l.classid = $1 AND l.objid = $2 AND l.granted
  AND l.database = (SELECT oid FROM pg_database WHERE datname = current_database())`,
		BulkLoadLockClass, BulkLoadLockID).Scan(&holder)
	if err != nil {
		return "", err
	}
	if !holder.Valid || holder.String == "" {
		return "no granted holder visible", nil
	}
	return holder.String, nil
}

// countingLocker counts tries so the acquired event can report how many polls
// the wait took.
type countingLocker struct {
	inner coordination.Locker
	polls int
}

func (c *countingLocker) TryLock(ctx context.Context) (bool, error) {
	c.polls++
	return c.inner.TryLock(ctx)
}

func (c *countingLocker) DescribeHolder(ctx context.Context) (string, error) {
	return c.inner.DescribeHolder(ctx)
}
