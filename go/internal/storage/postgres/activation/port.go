// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/obligation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// RunnerStore adapts Store to obligation.Store, the
// storage port of the resolution engine's activation obligation consumer.
type RunnerStore struct {
	Store Store
}

var _ obligation.Store = RunnerStore{}

// ClaimActivation leases the oldest claimable obligation.
func (r RunnerStore) ClaimActivation(ctx context.Context, owner string, lease time.Duration) (*obligation.Obligation, error) {
	work, err := r.Store.Claim(ctx, owner, lease)
	if err != nil || work == nil {
		return nil, err
	}
	return &obligation.Obligation{
		ScopeID: work.ScopeID, GenerationID: work.GenerationID,
		LeaseOwner: work.LeaseOwner, LeaseToken: work.LeaseToken,
		LeaseUntil: work.LeaseUntil, CreatedAt: work.CreatedAt,
	}, nil
}

// FinalizeActivation settles one claimed obligation. A lease lost inside the
// transaction is reported as obligation.ErrLeaseLost and a lock
// timeout as obligation.ErrFinalizeLockTimeout (finalizeError).
func (r RunnerStore) FinalizeActivation(ctx context.Context, work obligation.Obligation) (obligation.FinalizeResult, error) {
	result, err := r.Store.Finalize(ctx, Obligation{
		ScopeID: work.ScopeID, GenerationID: work.GenerationID,
		LeaseOwner: work.LeaseOwner, LeaseToken: work.LeaseToken,
		LeaseUntil: work.LeaseUntil, CreatedAt: work.CreatedAt,
	})
	if err != nil {
		return obligation.FinalizeResult{}, finalizeError(err)
	}
	return obligation.FinalizeResult{Outcome: string(result.Outcome), Woken: result.Woken}, nil
}

// RetireActivationInapplicable retires one claimed obligation as
// inapplicable under the lease fence. It takes Finalize's locks, so its
// errors map the same way.
func (r RunnerStore) RetireActivationInapplicable(ctx context.Context, work obligation.Obligation) (obligation.FinalizeResult, error) {
	result, err := r.Store.RetireInapplicable(ctx, Obligation{
		ScopeID: work.ScopeID, GenerationID: work.GenerationID,
		LeaseOwner: work.LeaseOwner, LeaseToken: work.LeaseToken,
		LeaseUntil: work.LeaseUntil, CreatedAt: work.CreatedAt,
	})
	if err != nil {
		return obligation.FinalizeResult{}, finalizeError(err)
	}
	return obligation.FinalizeResult{Outcome: string(result.Outcome)}, nil
}

// finalizeError maps a Finalize or RetireInapplicable error to the consumer
// port's sentinels, keeping the cause in the chain: a lease lost inside the
// transaction to obligation.ErrLeaseLost, and SQLSTATE 55P03
// (Finalize's lock_timeout expired while it waited for the scope or
// obligation row) to obligation.ErrFinalizeLockTimeout. Every
// other error is returned unchanged.
func finalizeError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrLeaseLost):
		return fmt.Errorf("%w: %w", obligation.ErrLeaseLost, err)
	case coordination.IsLockNotAvailable(err):
		return fmt.Errorf("%w: %w", obligation.ErrFinalizeLockTimeout, err)
	default:
		return err
	}
}

// CatchUpActivations owes obligations to one bounded page of scopes.
func (r RunnerStore) CatchUpActivations(ctx context.Context, cursor string, pageSize int) (obligation.CatchUpPage, error) {
	page, err := r.Store.CatchUp(ctx, cursor, pageSize)
	if err != nil {
		return obligation.CatchUpPage{}, err
	}
	return obligation.CatchUpPage{NextCursor: page.NextCursor, Scanned: page.Scanned, Inserted: page.Inserted}, nil
}

// PruneActivations deletes up to limit finished obligations older than retention.
func (r RunnerStore) PruneActivations(ctx context.Context, retention time.Duration, limit int) (int, error) {
	return r.Store.Prune(ctx, retention, limit)
}

// ActivationStats reads the per-status census.
func (r RunnerStore) ActivationStats(ctx context.Context) (obligation.Stats, error) {
	stats, err := r.Store.Stats(ctx)
	if err != nil {
		return obligation.Stats{}, err
	}
	byState := make(map[string]int64, len(stats.ByState))
	for state, count := range stats.ByState {
		byState[string(state)] = count
	}
	return obligation.Stats{ByState: byState, OldestOpenAge: stats.OldestOpenAge}, nil
}
