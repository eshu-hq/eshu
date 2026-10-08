// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// oweProducerActivationObligation records one producer-activation obligation
// (#7635) for every activated generation, inside the Ack transaction, so a
// quiet Ack still replays the consumers that wait on it. The settle decides
// whether the generation carries producer evidence and retires the rest as
// inapplicable. An earlier shape probed in Ack and owed only for producers,
// but the probe plans in ~8.4 ms per Ack through the Go driver (F1) while
// the insert costs ~0.1 ms, so probing on the Ack critical path would
// multiply Ack server time and the scope-row hold; the background settle
// absorbs the probe instead. The caller owns the transaction.
func oweProducerActivationObligation(ctx context.Context, tx db.ExecQueryer, scopeID, generationID string) error {
	return activation.InsertProducerActivation(ctx, tx, scopeID, generationID, projectorWorkItemID(scopeID, generationID))
}

// ProducerActivationRunnerStore adapts the activation Store plus the
// ingestion store's dependency-index settle to
// maintenance.ProducerActivationStore, the storage port of the resolution
// engine's producer-activation consumer.
type ProducerActivationRunnerStore struct {
	Activation activation.Store
	Ingestion  IngestionStore
}

var _ maintenance.ProducerActivationStore = ProducerActivationRunnerStore{}

// ClaimProducerActivation leases the oldest claimable producer obligation.
func (r ProducerActivationRunnerStore) ClaimProducerActivation(ctx context.Context, owner string, lease time.Duration) (*maintenance.ProducerActivation, error) {
	work, err := r.Activation.ClaimProducerActivation(ctx, owner, lease)
	if err != nil || work == nil {
		return nil, err
	}
	return &maintenance.ProducerActivation{
		ScopeID: work.ScopeID, GenerationID: work.GenerationID,
		LeaseOwner: work.LeaseOwner, LeaseToken: work.LeaseToken,
		LeaseUntil: work.LeaseUntil, CreatedAt: work.CreatedAt,
	}, nil
}

// SettleProducerActivation reopens the succeeded consumer items that wait on
// the claimed obligation's producer generation and completes the obligation
// under the caller's lease fence. A lease lost inside the transaction is
// reported as maintenance.ErrProducerActivationLeaseLost and a lock timeout
// as maintenance.ErrProducerActivationSettleLockTimeout.
func (r ProducerActivationRunnerStore) SettleProducerActivation(ctx context.Context, work maintenance.ProducerActivation) (maintenance.ProducerActivationSettleResult, error) {
	counts, outcome, err := r.Ingestion.SettleClaimedProducerActivation(ctx, activation.ProducerObligation{
		ScopeID: work.ScopeID, GenerationID: work.GenerationID,
		LeaseOwner: work.LeaseOwner, LeaseToken: work.LeaseToken,
		LeaseUntil: work.LeaseUntil, CreatedAt: work.CreatedAt,
	})
	if err != nil {
		return maintenance.ProducerActivationSettleResult{}, producerSettleError(err)
	}
	return maintenance.ProducerActivationSettleResult{
		Outcome:  string(outcome),
		Reopened: counts,
	}, nil
}

// producerSettleError maps a settle error to the consumer port's sentinels,
// keeping the cause in the chain: a lease lost inside the transaction to
// maintenance.ErrProducerActivationLeaseLost, and SQLSTATE 55P03 (the
// settle's lock_timeout expired while it waited for the scope or obligation
// row) to maintenance.ErrProducerActivationSettleLockTimeout. Every other
// error is returned unchanged.
func producerSettleError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrProducerLeaseLost):
		return fmt.Errorf("%w: %w", maintenance.ErrProducerActivationLeaseLost, err)
	case coordination.IsLockNotAvailable(err):
		return fmt.Errorf("%w: %w", maintenance.ErrProducerActivationSettleLockTimeout, err)
	default:
		return err
	}
}

// PruneProducerActivations deletes up to limit finished obligations older
// than retention.
func (r ProducerActivationRunnerStore) PruneProducerActivations(ctx context.Context, retention time.Duration, limit int) (int, error) {
	return r.Activation.PruneProducer(ctx, retention, limit)
}

// ProducerActivationStats reads the per-status census.
func (r ProducerActivationRunnerStore) ProducerActivationStats(ctx context.Context) (maintenance.ProducerActivationStats, error) {
	stats, err := r.Activation.StatsProducer(ctx)
	if err != nil {
		return maintenance.ProducerActivationStats{}, err
	}
	byState := make(map[string]int64, len(stats.ByState))
	for state, count := range stats.ByState {
		byState[string(state)] = count
	}
	return maintenance.ProducerActivationStats{ByState: byState, OldestOpenAge: stats.OldestOpenAge}, nil
}
