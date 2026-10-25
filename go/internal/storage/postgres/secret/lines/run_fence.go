// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/jackc/pgx/v5"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DeferredWritersLockID is the second advisory lock object of the bulk-load
// family (class BulkLoadLockClass). Every bootstrap-index session that carries
// DeferredSessionSQL holds it SHARED from birth until the session ends, so an
// exclusive holder knows no deferred writer is alive. The reducer's Repairer
// takes it exclusive (non-waiting) before it rebuilds.
const DeferredWritersLockID = 2

// ErrDeferredSessionRefused is the error a deferred-writer session gets at
// birth when the run's pinned lock session is gone. The write that needed the
// connection fails, so the run exits non-zero instead of writing rows that no
// rebuild owns.
var ErrDeferredSessionRefused = errors.New("secret_lines.deferred_session_refused: the bulk-load run lock session is gone")

// runLockHeldSQL asks whether the given backend holds the run lock (5318,1) in
// this database. Advisory lock rows are per database, so it filters on the
// current one.
const runLockHeldSQL = `
SELECT EXISTS (
    SELECT 1 FROM pg_locks
    WHERE locktype = 'advisory' AND classid = $1 AND objid = $2 AND granted AND pid = $3
      AND database = (SELECT oid FROM pg_database WHERE datname = current_database())
)`

// RunFence ties deferred-writer sessions to their run lock (#8833 ruling-4).
//
// Deferred sessions skip write-time derivation, so a rebuild may publish ready
// only while no such session can still write. The run lock (5318,1) alone does
// not say that: it lives on one pinned session, and that session can die (an
// idle timeout, a failover, an operator's pg_terminate_backend) while the pool
// sessions keep writing. The fence closes the gap from both sides:
//
//   - Every session takes (5318,2) shared at birth (Hook), so a Repairer that
//     needs it exclusive sees the live writers and yields.
//   - A session may be born only while the run's own pinned session still
//     holds (5318,1). When the pool reconnects after every session died, the
//     new session is refused, not silently admitted under a state a rebuild
//     already published.
//
// Create one with NewRunFence, give Hook to the pool's connect options, and
// pass the same fence to AcquireBulkLoadLock through LockOptions.
type RunFence struct {
	pid    atomic.Int64
	logger atomic.Pointer[slog.Logger]
}

// NewRunFence returns a fence that logs refusals to logger (nil discards).
func NewRunFence(logger *slog.Logger) *RunFence {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	fence := &RunFence{}
	fence.logger.Store(logger)
	return fence
}

// Hook is the connect hook every deferred-writer session runs, after
// DeferredSessionSQL. It takes (5318,2) shared in the waiting form: the only
// holder it can wait behind is a Repairer rebuild, which holds nothing else and
// waits on nothing, so there is no cycle, and a bootstrap-index that starts
// during a rebuild waits for it. The wait is bounded by the context of the call
// that opens the connection. Once the run's pinned session is known, it then
// checks that session still holds (5318,1) and returns ErrDeferredSessionRefused
// when it does not; pgx closes the connection, which frees the shared lock.
func (f *RunFence) Hook() func(ctx context.Context, conn *pgx.Conn) error {
	return func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock_shared($1, $2)",
			BulkLoadLockClass, DeferredWritersLockID); err != nil {
			return fmt.Errorf("take secret lines deferred writer lock: %w", err)
		}
		pid := f.pid.Load()
		if pid == 0 {
			return nil
		}
		var held bool
		if err := conn.QueryRow(ctx, runLockHeldSQL, BulkLoadLockClass, BulkLoadLockID, pid).Scan(&held); err != nil {
			return fmt.Errorf("check secret lines run lock session: %w", err)
		}
		if held {
			return nil
		}
		f.logger.Load().ErrorContext(ctx, "secret lines deferred session refused: the bulk load run lock session is gone",
			telemetry.EventAttr("secret_lines.deferred_session_refused"),
			telemetry.FailureClassAttr("secret_lines_bulk_load_lock_lost"), "run_lock_pid", pid)
		return fmt.Errorf("%w (backend %d no longer holds the run lock)", ErrDeferredSessionRefused, pid)
	}
}

// publish records the pinned session's backend pid once it holds (5318,1) and
// adopts logger for later refusals: the pool is opened before the run's logger
// exists.
func (f *RunFence) publish(pid int, logger *slog.Logger) {
	if f != nil {
		f.pid.Store(int64(pid))
		f.logger.Store(logger)
	}
}

// clear forgets the pid at Release, when the run no longer owns the lock.
func (f *RunFence) clear() {
	if f != nil {
		f.pid.Store(0)
	}
}
