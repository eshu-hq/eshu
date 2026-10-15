// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reset

import (
	"context"
	"fmt"
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
	if generations.Len() == 0 {
		return nil
	}
	rows, err := q.QueryContext(ctx, `SELECT current_setting('lock_timeout')`)
	if err != nil {
		return fmt.Errorf("refinalize reducer claim fence: read lock_timeout: %w", err)
	}
	if !rows.Next() {
		_ = rows.Close()
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
	if _, err := q.ExecContext(ctx, refinalizeReducerClaimFenceQuery); err != nil {
		return &ClaimFenceLockTimeoutError{err: err}
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
