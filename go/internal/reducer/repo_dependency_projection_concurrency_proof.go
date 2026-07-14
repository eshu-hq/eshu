// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package reducer

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"time"
)

// runRepoDependencyProjection fans the proof Odù's acceptance units across
// workers without using shared_projection_intents.partition_hash. That stored
// hash is per edge and would split one source repository's retract/rewrite
// snapshot. This proof-only coordinator instead scans the tiny fixture in
// memory and assigns every row for one acceptance unit to exactly one shard.
func runRepoDependencyProjection(ctx context.Context, runner *RepoDependencyProjectionRunner) error {
	workers := runner.Config.workerCount()
	if workers <= 1 {
		return runner.runSerial(ctx)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)
	for workerID := range workers {
		worker := *runner
		worker.Config.Workers = 1
		worker.Config.LeaseOwner = fmt.Sprintf("%s/proof-%d-of-%d", runner.Config.leaseOwner(), workerID, workers)
		worker.IntentReader = &ifaRepoDependencyProofShardReader{
			inner:      runner.IntentReader,
			shardID:    workerID,
			shardCount: workers,
		}
		worker.LeaseManager = ifaRepoDependencyProofShardLeaseManager{
			inner:      runner.LeaseManager,
			shardID:    workerID,
			shardCount: workers,
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := worker.runSerial(ctx); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				cancel()
			}
		}()
	}
	wg.Wait()

	errMu.Lock()
	defer errMu.Unlock()
	return firstErr
}

type ifaRepoDependencyProofShardReader struct {
	inner      RepoDependencyProjectionIntentReader
	shardID    int
	shardCount int
}

func (r *ifaRepoDependencyProofShardReader) ListPendingDomainIntents(
	ctx context.Context,
	domain string,
	limit int,
) ([]SharedProjectionIntentRow, error) {
	rows, err := r.inner.ListPendingDomainIntents(ctx, domain, maxRepoDependencyAcceptanceScanLimit)
	if err != nil {
		return nil, err
	}
	owned := make([]SharedProjectionIntentRow, 0, min(len(rows), limit))
	for _, row := range rows {
		acceptanceUnitID, ok := repoDependencyAcceptanceUnitID(row)
		if !ok {
			// Keep malformed rows visible to worker zero so the shipped validation
			// path fails closed instead of the proof silently filtering them out.
			if r.shardID == 0 {
				owned = append(owned, row)
			}
			continue
		}
		if ifaRepoDependencyAcceptanceShard(acceptanceUnitID, r.shardCount) == r.shardID {
			owned = append(owned, row)
		}
	}
	if limit > 0 && len(owned) > limit {
		owned = owned[:limit]
	}
	return owned, nil
}

func (r *ifaRepoDependencyProofShardReader) ListAcceptanceUnitDomainIntents(
	ctx context.Context,
	acceptanceUnitID string,
	domain string,
	limit int,
) ([]SharedProjectionIntentRow, error) {
	return r.inner.ListAcceptanceUnitDomainIntents(ctx, acceptanceUnitID, domain, limit)
}

func (r *ifaRepoDependencyProofShardReader) MarkIntentsCompleted(
	ctx context.Context,
	intentIDs []string,
	completedAt time.Time,
) error {
	return r.inner.MarkIntentsCompleted(ctx, intentIDs, completedAt)
}

type ifaRepoDependencyProofShardLeaseManager struct {
	inner      PartitionLeaseManager
	shardID    int
	shardCount int
}

func (m ifaRepoDependencyProofShardLeaseManager) ClaimPartitionLease(
	ctx context.Context,
	domain string,
	_, _ int,
	leaseOwner string,
	leaseTTL time.Duration,
) (bool, error) {
	return m.inner.ClaimPartitionLease(ctx, domain, m.shardID, m.shardCount, leaseOwner, leaseTTL)
}

func (m ifaRepoDependencyProofShardLeaseManager) ReleasePartitionLease(
	ctx context.Context,
	domain string,
	_, _ int,
	leaseOwner string,
) error {
	return m.inner.ReleasePartitionLease(ctx, domain, m.shardID, m.shardCount, leaseOwner)
}

func ifaRepoDependencyAcceptanceShard(acceptanceUnitID string, shardCount int) int {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(acceptanceUnitID))
	return int(hasher.Sum32() % uint32(shardCount))
}
