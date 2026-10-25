// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"database/sql/driver"
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
	// Fence, when set, learns the pinned session's backend pid on acquisition
	// and forgets it on Release, so deferred sessions born while the run lock
	// session is alive are admitted and later ones are refused.
	Fence *RunFence
	// KeepAliveInterval is the cadence of the SELECT 1 that keeps the pinned
	// session from idle_session_timeout. Default 30 s.
	KeepAliveInterval time.Duration
}

// LockRefusal says why TryAcquireBulkLoadLock returned no lock.
type LockRefusal struct {
	// Reason is "locked" when another session holds (5318,1) or
	// "deferred_writers" when live deferred sessions hold (5318,2) shared.
	Reason string
	// Lock names the blocking lock: "bulk_load" is (5318,1), "deferred_writers"
	// is (5318,2).
	Lock string
	// Holder names the backend(s) holding the blocking lock.
	Holder string
}

// BulkLoadLock is the run-scoped exclusivity of one deferred bulk load. It pins
// one connection for the whole run so the session advisory lock outlives every
// pool checkout and dies with its backend if the process is killed. A keep-alive
// on that connection stops an idle_session_timeout from ending it mid-run.
type BulkLoadLock struct {
	conn     *sql.Conn
	pid      int
	logger   *slog.Logger
	acquired time.Time
	// deferredWriters is true when this lock also holds (5318,2) exclusive,
	// which only a Repairer takes.
	deferredWriters bool
	fence           *RunFence
	stopKeepAlive   context.CancelFunc
	keepAliveDone   chan struct{}
}

const defaultKeepAliveInterval = 30 * time.Second

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
// holding nothing, so neither is ever in the database's wait-for graph. The
// pinned session was itself born through the deferred-writer hook, so it holds
// (5318,2) shared like every other bootstrap-index session; the wait for the
// connection (not for the lock) can therefore be behind a Repairer rebuild.
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
		discardConn(conn)
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
		// The error may be a try whose answer never arrived, so the session may
		// hold the lock; never hand it back to a pool.
		discardConn(conn)
		return nil, err
	}
	lock := &BulkLoadLock{conn: conn, pid: pid, logger: options.Logger, acquired: time.Now(), fence: options.Fence}
	lock.fence.publish(pid, options.Logger)
	lock.startKeepAlive(options.KeepAliveInterval)
	options.Logger.InfoContext(ctx, "secret lines bulk load lock acquired",
		telemetry.EventAttr("secret_lines.bulk_load_lock_acquired"),
		"pid", pid, "waited_ms", time.Since(started).Milliseconds(), "polls", locker.polls-1,
		"connection", "pool")
	return lock, nil
}

// TryAcquireBulkLoadLock is the non-waiting form of AcquireBulkLoadLock for a
// steady-state Repairer (#8833 ruling-3, ruling-4). On one dedicated connection
// it tries (5318,1) and then (5318,2) exclusive, never waiting on either, so a
// Repairer is never in the locks' wait graph. (5318,2) is free only while no
// bootstrap-index session is alive, so holding both means no deferred writer can
// be writing. When either is taken it releases what it got, returns a nil lock,
// and says which lock blocked and who holds it. It never takes the schema lock
// (5318,0).
//
// database must not be the pool that Finalize uses: the lock pins a connection
// until Release, and a pool of one would starve the rebuild. The reducer opens
// a dedicated one-connection pool for it.
func TryAcquireBulkLoadLock(ctx context.Context, database Conner, options LockOptions) (*BulkLoadLock, LockRefusal, error) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	conn, err := database.Conn(ctx)
	if err != nil {
		return nil, LockRefusal{}, fmt.Errorf("open secret lines bulk load connection: %w", err)
	}
	var pid int
	if err := conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		discardConn(conn)
		return nil, LockRefusal{}, fmt.Errorf("read secret lines bulk load backend pid: %w", err)
	}
	runLocked, err := tryAdvisory(ctx, conn, BulkLoadLockID)
	if err != nil {
		discardConn(conn)
		return nil, LockRefusal{}, fmt.Errorf("try secret lines bulk load lock: %w", err)
	}
	if !runLocked {
		refusal := LockRefusal{Reason: "locked", Lock: "bulk_load", Holder: describeHolders(ctx, conn, BulkLoadLockID)}
		_ = conn.Close() // holds nothing: safe to return to the pool
		return nil, refusal, nil
	}
	writersLocked, err := tryAdvisory(ctx, conn, DeferredWritersLockID)
	if err != nil {
		discardConn(conn) // ending the session frees (5318,1)
		return nil, LockRefusal{}, fmt.Errorf("try secret lines deferred writer lock: %w", err)
	}
	if !writersLocked {
		refusal := LockRefusal{Reason: "deferred_writers", Lock: "deferred_writers", Holder: describeHolders(ctx, conn, DeferredWritersLockID)}
		if err := unlockAdvisory(ctx, conn, BulkLoadLockID); err != nil {
			discardConn(conn)
			return nil, refusal, nil
		}
		_ = conn.Close()
		return nil, refusal, nil
	}
	lock := &BulkLoadLock{conn: conn, pid: pid, logger: logger, acquired: time.Now(), deferredWriters: true}
	lock.startKeepAlive(options.KeepAliveInterval)
	logger.InfoContext(ctx, "secret lines bulk load lock acquired",
		telemetry.EventAttr("secret_lines.bulk_load_lock_acquired"),
		"pid", pid, "waited_ms", 0, "polls", 0, "connection", "dedicated")
	return lock, LockRefusal{}, nil
}

// startKeepAlive runs SELECT 1 on the pinned session every interval so a server
// idle_session_timeout (idle means waiting for a client query) never ends it
// while the run is alive. A failure means the session, and with it the lock, is
// gone: it logs once and stops. It does not cancel the run: the birth fence
// already refuses new deferred sessions, and live ones keep the Repairer out.
func (l *BulkLoadLock) startKeepAlive(interval time.Duration) {
	if interval <= 0 {
		interval = defaultKeepAliveInterval
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.stopKeepAlive = cancel
	l.keepAliveDone = make(chan struct{})
	go func() {
		defer close(l.keepAliveDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			pingCtx, pingCancel := context.WithTimeout(ctx, max(interval, 5*time.Second))
			_, err := l.conn.ExecContext(pingCtx, "SELECT 1")
			pingCancel()
			if err == nil {
				continue
			}
			if ctx.Err() == nil {
				l.logger.ErrorContext(context.Background(), "secret lines bulk load lock session lost",
					telemetry.EventAttr("secret_lines.bulk_load_lock_lost"),
					telemetry.FailureClassAttr("secret_lines_bulk_load_lock_lost"),
					"pid", l.pid, "held_seconds", time.Since(l.acquired).Seconds(), "error", err.Error())
			}
			return
		}
	}()
}

// Release unlocks and closes the pinned connection. A false unlock or an error
// means the session that held the lock is gone, so exclusivity was void for
// part of the run: the caller must fail the run loudly (a rerun rebuilds the
// table and heals it), never swallow the error. A session whose unlock is
// uncertain is discarded, never returned to a pool.
func (l *BulkLoadLock) Release(ctx context.Context) error {
	l.stopKeepAlive()
	<-l.keepAliveDone
	l.fence.clear()
	// The run's context may already be cancelled; the unlock gets its own
	// deadline, like the schema lock's release.
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()
	var errs []error
	uncertain := false
	unlock := func(id int, name string) {
		var released bool
		err := l.conn.QueryRowContext(unlockCtx, "SELECT pg_advisory_unlock($1, $2)",
			BulkLoadLockClass, id).Scan(&released)
		switch {
		case err != nil:
			uncertain = true
			errs = append(errs, fmt.Errorf("release secret lines %s lock (backend %d): %w", name, l.pid, err))
		case !released:
			uncertain = true
			errs = append(errs, fmt.Errorf("release secret lines %s lock: backend %d no longer held it, so a concurrent bulk load was possible during this run", name, l.pid))
		}
	}
	if l.deferredWriters {
		unlock(DeferredWritersLockID, "deferred writer")
	}
	unlock(BulkLoadLockID, "bulk load")
	if uncertain {
		discardConn(l.conn)
	} else if closeErr := l.conn.Close(); closeErr != nil {
		errs = append(errs, closeErr)
	}
	if len(errs) == 0 {
		l.logger.InfoContext(ctx, "secret lines bulk load lock released",
			telemetry.EventAttr("secret_lines.bulk_load_lock_released"),
			"pid", l.pid, "held_seconds", time.Since(l.acquired).Seconds())
	}
	return errors.Join(errs...)
}

// discardConn closes a session whose lock state is uncertain and tells
// database/sql to drop the physical connection instead of pooling it, so a
// later caller cannot inherit a held advisory lock. The session ending releases
// whatever it held. The reducer's package-manifest backfill does the same.
func discardConn(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

// tryAdvisory is one pg_try_advisory_lock of the bulk-load class on conn.
func tryAdvisory(ctx context.Context, conn *sql.Conn, id int) (bool, error) {
	var locked bool
	err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1, $2)", BulkLoadLockClass, id).Scan(&locked)
	return locked, err
}

// unlockAdvisory unlocks one bulk-load-class lock and errors when the session
// did not hold it.
func unlockAdvisory(ctx context.Context, conn *sql.Conn, id int) error {
	var released bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1, $2)", BulkLoadLockClass, id).Scan(&released); err != nil {
		return err
	}
	if !released {
		return errors.New("lock was not held")
	}
	return nil
}

// describeHolders names the backends holding lock id, or why it cannot.
func describeHolders(ctx context.Context, conn *sql.Conn, id int) string {
	holder, err := holdersOf(ctx, conn, id)
	if err != nil {
		return "holder unknown: " + err.Error()
	}
	return holder
}

// bulkLoadLocker is the coordination.Locker for the run lock (5318,1): one
// non-blocking try per poll, and the granted holder for the operator log.
type bulkLoadLocker struct{ conn *sql.Conn }

func (l bulkLoadLocker) TryLock(ctx context.Context) (bool, error) {
	return tryAdvisory(ctx, l.conn, BulkLoadLockID)
}

// DescribeHolder names the backend(s) holding the run lock in this database.
func (l bulkLoadLocker) DescribeHolder(ctx context.Context) (string, error) {
	return holdersOf(ctx, l.conn, BulkLoadLockID)
}

// holdersOf names the backend(s) holding lock id in this database, the same
// shape the schema lock's holder diagnostic uses plus the lock and its mode.
// Advisory locks are per database, so the query filters on the current one.
func holdersOf(ctx context.Context, conn *sql.Conn, id int) (string, error) {
	var holder sql.NullString
	err := conn.QueryRowContext(ctx, `
SELECT string_agg(format('pid=%s application_name=%s state=%s connected_for=%s lock=%s mode=%s',
    a.pid, coalesce(a.application_name, ''), coalesce(a.state, ''),
    date_trunc('second', now() - a.backend_start),
    CASE l.objid WHEN 1 THEN 'bulk_load' ELSE 'deferred_writers' END, l.mode), '; ' ORDER BY a.pid)
FROM pg_locks l
JOIN pg_stat_activity a ON a.pid = l.pid
WHERE l.locktype = 'advisory' AND l.classid = $1 AND l.objid = $2 AND l.granted
  AND l.database = (SELECT oid FROM pg_database WHERE datname = current_database())`,
		BulkLoadLockClass, id).Scan(&holder)
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
