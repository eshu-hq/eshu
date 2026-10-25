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

// DeferredPoolOptions tunes OpenDeferredWriterPool.
type DeferredPoolOptions struct {
	// Probe opens the plain, hook-free pool the probe session comes from. It is
	// opened first, so an unreachable database fails here within the opener's
	// own ping bound (ESHU_POSTGRES_PING_TIMEOUT for runtime.OpenPostgres).
	Probe func(context.Context) (*sql.DB, error)
	// Open opens the deferred-writer pool, whose connect hook takes (5318,2)
	// shared and may refuse a session born after the run lock session died.
	Open func(context.Context) (*sql.DB, error)
	// Fence is the run fence the Open pool's connect hook is built from, when the
	// composition has one. The caller must hand this same object to the run lock
	// (LockOptions.Fence): a different fence leaves the birth fence unarmed.
	// BootstrapDeferredPoolOptions sets it; OpenDeferredWriterPool does not read
	// it, it exists so a test can tie the hook's fence to the lock's.
	Fence *RunFence
	// Wait bounds how long the probe waits for a reducer rebuild to release
	// (5318,2). bootstrap-index passes the schema ownership wait
	// (ESHU_SCHEMA_BOOTSTRAP_OWNERSHIP_WAIT). Zero takes 3 minutes.
	Wait time.Duration
	// Poll is the try cadence. Zero takes one second.
	Poll time.Duration
	// LogEvery is how often the holder is logged while waiting. Zero takes 15 s.
	LogEvery time.Duration
	// Logger receives the ownership events. Nil discards.
	Logger *slog.Logger
}

// OpenDeferredWriterPool opens the deferred-writer pool of a bulk load behind a
// probe session that holds (5318,2) shared (#8833 ruling-6). A Repairer rebuild
// holds (5318,2) exclusive, and the pool's connect hook waits for it in its
// blocking form, which the pool's startup ping would cut off after the ping
// timeout. The probe polls a try-lock for the ownership wait first, logs the
// holder as bootstrap.postgres.ownership.waiting with lock=deferred_writers, and
// fails naming the holder when the wait is spent. While the probe holds the lock
// no Repairer can take it exclusive, so the hook never waits and the ping keeps
// its meaning: an unreachable database. The probe is released once the pool is
// open, or when opening it fails.
func OpenDeferredWriterPool(ctx context.Context, options DeferredPoolOptions) (*sql.DB, error) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	probePool, err := options.Probe(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = probePool.Close() }()
	probe, err := AcquireDeferredWriterProbe(ctx, probePool, ProbeOptions{
		Wait: options.Wait, Poll: options.Poll, LogEvery: options.LogEvery, Logger: logger,
	})
	if err != nil {
		return nil, err
	}
	pool, openErr := options.Open(ctx)
	if releaseErr := probe.Release(ctx); releaseErr != nil {
		if openErr != nil {
			return nil, errors.Join(openErr, releaseErr)
		}
		// The probe only sequenced the startup. The pool's own sessions hold
		// (5318,2) shared, so a lost probe lock changes nothing for the run.
		logger.WarnContext(ctx, "secret lines deferred writer probe release failed",
			telemetry.EventAttr("secret_lines.deferred_writer_probe_release_failed"),
			"error", releaseErr.Error())
	}
	if openErr != nil {
		return nil, openErr
	}
	return pool, nil
}

// ProbeOptions tunes AcquireDeferredWriterProbe. Zero values take the defaults.
type ProbeOptions struct {
	Wait     time.Duration
	Poll     time.Duration
	LogEvery time.Duration
	Logger   *slog.Logger
}

// DeferredWriterProbe is one session holding (5318,2) shared, taken before the
// deferred-writer pool opens.
type DeferredWriterProbe struct {
	conn   *sql.Conn
	pid    int
	logger *slog.Logger
}

// AcquireDeferredWriterProbe takes (5318,2) shared on one connection from
// database with a try-lock poll, waiting up to options.Wait for a Repairer
// rebuild to release it and then failing with an error that names the holder.
// The poll holds nothing while it waits, so the probe is never in the
// database's wait-for graph.
func AcquireDeferredWriterProbe(ctx context.Context, database Conner, options ProbeOptions) (*DeferredWriterProbe, error) {
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
		return nil, fmt.Errorf("open secret lines deferred writer probe connection: %w", err)
	}
	var pid int
	if err := conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		discardConn(conn)
		return nil, fmt.Errorf("read secret lines deferred writer probe backend pid: %w", err)
	}
	if err := coordination.WaitForOwnership(ctx, options.Logger, deferredWritersLocker{conn: conn}, coordination.OwnershipPolicy{
		Wait:     options.Wait,
		Poll:     options.Poll,
		LogEvery: options.LogEvery,
		Subject:  "secret lines deferred writers",
		Lock:     "deferred_writers",
	}, coordination.SleepContext, time.Now); err != nil {
		// The error may be a try whose answer never arrived, so the session may
		// hold the lock; never hand it back to a pool.
		discardConn(conn)
		return nil, err
	}
	return &DeferredWriterProbe{conn: conn, pid: pid, logger: options.Logger}, nil
}

// Release unlocks and closes the probe connection. A session whose unlock is
// uncertain (an error, or the lock was not held) is discarded, never returned
// to a pool.
func (p *DeferredWriterProbe) Release(ctx context.Context) error {
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()
	var released bool
	err := p.conn.QueryRowContext(unlockCtx, "SELECT pg_advisory_unlock_shared($1, $2)",
		BulkLoadLockClass, DeferredWritersLockID).Scan(&released)
	switch {
	case err != nil:
		discardConn(p.conn)
		return fmt.Errorf("release secret lines deferred writer probe lock (backend %d): %w", p.pid, err)
	case !released:
		discardConn(p.conn)
		return fmt.Errorf("release secret lines deferred writer probe lock: backend %d no longer held it", p.pid)
	}
	return p.conn.Close()
}

// deferredWritersLocker is the coordination.Locker for (5318,2) shared: one
// non-blocking try per poll, and the granted holders for the operator log.
type deferredWritersLocker struct{ conn *sql.Conn }

func (l deferredWritersLocker) TryLock(ctx context.Context) (bool, error) {
	var locked bool
	err := l.conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock_shared($1, $2)",
		BulkLoadLockClass, DeferredWritersLockID).Scan(&locked)
	return locked, err
}

// DescribeHolder names the backend(s) holding (5318,2) in this database.
func (l deferredWritersLocker) DescribeHolder(ctx context.Context) (string, error) {
	return holdersOf(ctx, l.conn, DeferredWritersLockID)
}
