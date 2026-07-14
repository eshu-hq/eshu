// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const repoDependencyLeaseOwnerActiveSQL = `
SELECT EXISTS (
    SELECT 1
    FROM shared_projection_partition_leases
    WHERE projection_domain = $1
      AND partition_id = $2
      AND partition_count = $3
      AND lease_owner = $4
      AND lease_expires_at > CURRENT_TIMESTAMP
)`

// RepoDependencyAcceptanceUnitGate holds the existing per-repository
// generation/maintenance fence across one repo-dependency graph replacement
// and its durable intent completion.
type RepoDependencyAcceptanceUnitGate struct {
	db Beginner
}

// NewRepoDependencyAcceptanceUnitGate creates a repository-scoped gate backed
// by a transaction-capable Postgres adapter.
func NewRepoDependencyAcceptanceUnitGate(db Beginner) *RepoDependencyAcceptanceUnitGate {
	return &RepoDependencyAcceptanceUnitGate{db: db}
}

// WithAcceptanceUnit enters the repository critical section only while key's
// process owns an unexpired partition lease. Generation commits take the
// matching shared advisory lock, so accepted generation cannot change during
// the callback. Distinct repository ids use distinct locks and remain parallel.
func (g *RepoDependencyAcceptanceUnitGate) WithAcceptanceUnit(
	ctx context.Context,
	key reducer.RepoDependencyAcceptanceUnitGateKey,
	fn func(context.Context, reducer.RepoDependencyProjectionIntentReader) error,
) (bool, error) {
	if g == nil || g.db == nil {
		return false, fmt.Errorf("repo dependency acceptance-unit gate requires a transaction beginner")
	}
	if strings.TrimSpace(key.Domain) == "" || strings.TrimSpace(key.AcceptanceUnitID) == "" ||
		key.PartitionID < 0 || key.PartitionCount <= 0 || strings.TrimSpace(key.LeaseOwner) == "" {
		return false, fmt.Errorf("repo dependency acceptance-unit gate key is incomplete")
	}
	if fn == nil {
		return false, fmt.Errorf("repo dependency acceptance-unit gate callback is required")
	}

	tx, err := g.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin repo dependency acceptance-unit gate: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := acquireDeferredMaintenanceRepoExclusiveLocks(ctx, tx, []string{key.AcceptanceUnitID}); err != nil {
		return false, fmt.Errorf("lock repo dependency acceptance unit %q: %w", key.AcceptanceUnitID, err)
	}
	owned, err := repoDependencyLeaseOwnerActive(ctx, tx, key)
	if err != nil {
		return false, err
	}
	if !owned {
		return false, nil
	}

	txStore := NewSharedIntentStore(tx)
	if err := fn(ctx, txStore); err != nil {
		return true, err
	}
	if err := tx.Commit(); err != nil {
		return true, fmt.Errorf("commit repo dependency acceptance-unit gate: %w", err)
	}
	committed = true
	return true, nil
}

func repoDependencyLeaseOwnerActive(
	ctx context.Context,
	tx ExecQueryer,
	key reducer.RepoDependencyAcceptanceUnitGateKey,
) (bool, error) {
	rows, err := tx.QueryContext(
		ctx,
		repoDependencyLeaseOwnerActiveSQL,
		key.Domain,
		key.PartitionID,
		key.PartitionCount,
		key.LeaseOwner,
	)
	if err != nil {
		return false, fmt.Errorf("validate repo dependency partition owner: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, fmt.Errorf("validate repo dependency partition owner returned no row")
	}
	var owned bool
	if err := rows.Scan(&owned); err != nil {
		return false, fmt.Errorf("scan repo dependency partition owner: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read repo dependency partition owner: %w", err)
	}
	return owned, nil
}
