// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reset

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	// RefinalizeClaimFenceLockTimeout bounds one attempt to acquire the
	// transaction-scoped queue claim fence.
	RefinalizeClaimFenceLockTimeout = time.Second

	// DefaultRefinalizeClaimFenceRetryBudget bounds cumulative fence contention,
	// including the first timed-out lock attempt.
	DefaultRefinalizeClaimFenceRetryBudget = 30 * time.Second
)

// FenceAdmissionBudget bounds the time spent acquiring the reducer claim
// fence. Its context remains usable through commit after admission succeeds.
type FenceAdmissionBudget struct {
	parent   context.Context
	ctx      context.Context
	cancel   context.CancelFunc
	budget   time.Duration
	mu       sync.Mutex
	started  time.Time
	deadline time.Time
	timer    *time.Timer
	admitted bool
	expired  bool
	closed   bool
}

// NewFenceAdmissionBudget creates a caller-cancelable fence admission context.
// The timer starts at the first table-lock attempt, not at transaction start.
func NewFenceAdmissionBudget(parent context.Context, budget time.Duration) *FenceAdmissionBudget {
	if budget <= 0 {
		budget = DefaultRefinalizeClaimFenceRetryBudget
	}
	ctx, cancel := context.WithCancel(parent)
	return &FenceAdmissionBudget{parent: parent, ctx: ctx, cancel: cancel, budget: budget}
}

// Context returns the context used for transaction acquisition and statements.
func (b *FenceAdmissionBudget) Context() context.Context { return b.ctx }

// Elapsed reports time since the first fence lock attempt, or zero beforehand.
func (b *FenceAdmissionBudget) Elapsed() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.started.IsZero() {
		return 0
	}
	return time.Since(b.started)
}

// Expired reports whether admission lost the deadline race.
func (b *FenceAdmissionBudget) Expired() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.expireIfDueLocked()
}

// Close stops the deadline timer and releases the child context.
func (b *FenceAdmissionBudget) Close() {
	b.mu.Lock()
	b.closed = true
	if b.timer != nil {
		b.timer.Stop()
	}
	b.mu.Unlock()
	b.cancel()
}

func (b *FenceAdmissionBudget) start() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.admitted || !b.started.IsZero() {
		return
	}
	b.started = time.Now()
	b.deadline = b.started.Add(b.budget)
	b.timer = time.AfterFunc(b.budget, b.expire)
}

func (b *FenceAdmissionBudget) admit() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.parent.Err(); err != nil {
		return err
	}
	if b.started.IsZero() {
		return fmt.Errorf("refinalize fence admission was not started")
	}
	if b.expireIfDueLocked() {
		return fmt.Errorf("refinalize fence admission budget expired")
	}
	b.admitted = true
	if b.timer != nil {
		b.timer.Stop()
	}
	return nil
}

func (b *FenceAdmissionBudget) expire() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.expireIfDueLocked() || b.admitted || b.closed {
		return
	}
	b.timer = time.AfterFunc(time.Until(b.deadline), b.expire)
}

func (b *FenceAdmissionBudget) expireIfDueLocked() bool {
	if b.expired {
		return true
	}
	if b.started.IsZero() || b.admitted || time.Now().Before(b.deadline) {
		return false
	}
	b.expired = true
	b.cancel()
	return true
}

// ClaimFenceLockTimeoutError identifies a lock_timeout raised while acquiring
// the refinalize table fence. It lets the transaction owner retry only this
// failure without retrying unrelated 55P03 errors.
type ClaimFenceLockTimeoutError struct{ err error }

// Error returns the lock-fence failure description.
func (e *ClaimFenceLockTimeoutError) Error() string {
	return fmt.Sprintf("refinalize reducer claim fence: %v", e.err)
}

// Unwrap exposes the PostgreSQL error for SQLSTATE classification.
func (e *ClaimFenceLockTimeoutError) Unwrap() error { return e.err }

// refinalizeReducerClaimFenceQuery is deliberately EXCLUSIVE, not SHARE ROW
// EXCLUSIVE. Claim UPDATEs take ROW EXCLUSIVE and claim-fenced relationship
// activation takes ROW SHARE through SELECT FOR UPDATE; EXCLUSIVE conflicts
// with both while still permitting ordinary ACCESS SHARE reads. Recovery takes
// it immediately after the lock-free drain and before any transaction write,
// avoiding lock upgrades when two recoveries overlap.
const refinalizeReducerClaimFenceQuery = `
LOCK TABLE fact_work_items IN EXCLUSIVE MODE
`

// AcquireReducerClaimFence closes the last-poll-to-commit window for both new
// claims and expired workers attempting claim-fenced publication. It keeps the
// exclusive lock transaction-scoped through enqueue, reset, retirement, and
// commit, and limits each lock acquisition independently.
func AcquireReducerClaimFence(
	ctx context.Context,
	q ClaimFenceQueryer,
	generations Generations,
) error {
	return acquireReducerClaimFence(ctx, q, generations, nil)
}

// AcquireReducerClaimFenceWithAdmission stops the admission budget immediately
// after PostgreSQL accepts the table lock and before any post-fence statements.
func AcquireReducerClaimFenceWithAdmission(
	ctx context.Context,
	q ClaimFenceQueryer,
	generations Generations,
	budget *FenceAdmissionBudget,
) error {
	return acquireReducerClaimFence(ctx, q, generations, budget)
}

func acquireReducerClaimFence(
	ctx context.Context,
	q ClaimFenceQueryer,
	generations Generations,
	budget *FenceAdmissionBudget,
) error {
	if generations.Len() == 0 {
		return nil
	}
	rows, err := q.QueryContext(ctx, `SELECT current_setting('lock_timeout')`)
	if err != nil {
		return fmt.Errorf("refinalize reducer claim fence: read lock_timeout: %w", err)
	}
	if !rows.Next() {
		rowsErr := rows.Err()
		_ = rows.Close()
		if rowsErr != nil {
			return fmt.Errorf("refinalize reducer claim fence: read lock_timeout: %w", rowsErr)
		}
		return fmt.Errorf("refinalize reducer claim fence: read lock_timeout: no setting returned")
	}
	var priorTimeout string
	if err := rows.Scan(&priorTimeout); err != nil {
		_ = rows.Close()
		return fmt.Errorf("refinalize reducer claim fence: scan lock_timeout: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("refinalize reducer claim fence: close lock_timeout read: %w", err)
	}
	if _, err := q.ExecContext(ctx, `SELECT set_config('lock_timeout', $1, true)`, RefinalizeClaimFenceLockTimeout.String()); err != nil {
		return fmt.Errorf("refinalize reducer claim fence: set lock_timeout: %w", err)
	}
	if budget != nil {
		budget.start()
	}
	if _, err := q.ExecContext(ctx, refinalizeReducerClaimFenceQuery); err != nil {
		return &ClaimFenceLockTimeoutError{err: err}
	}
	if budget != nil {
		if err := budget.admit(); err != nil {
			return err
		}
	}
	if _, err := q.ExecContext(ctx, `SELECT set_config('lock_timeout', $1, true)`, priorTimeout); err != nil {
		return fmt.Errorf("refinalize reducer claim fence: restore lock_timeout: %w", err)
	}
	inflight, err := countInflightReducers(ctx, q, generations)
	if err != nil {
		return fmt.Errorf("refinalize reducer claim fence recheck: %w", err)
	}
	if inflight == 0 {
		return nil
	}
	return newInflightReducersError(ctx, q, generations, inflight, 0, 0)
}
