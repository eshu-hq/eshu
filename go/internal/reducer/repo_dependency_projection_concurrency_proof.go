// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"time"
)

// runRepoDependencyProjection fans source-owned acceptance units across
// workers without using shared_projection_intents.partition_hash. That stored
// hash is per edge and would split one source repository's retract/rewrite
// snapshot. The coordinator scans pending rows and assigns every row for one
// acceptance unit to exactly one fixed shard.
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
		worker.Config.PartitionID = workerID
		worker.Config.PartitionCount = workers
		worker.Config.LeaseOwner = fmt.Sprintf("%s/worker-%d-of-%d", runner.Config.leaseOwner(), workerID, workers)
		worker.IntentReader = &repoDependencyShardReader{
			inner:      runner.IntentReader,
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

type repoDependencyShardReader struct {
	inner      RepoDependencyProjectionIntentReader
	shardID    int
	shardCount int
}

type repoDependencyPendingIntentContinuationReader interface {
	ListPendingDomainIntentsAfter(
		ctx context.Context,
		domain string,
		afterCreatedAt time.Time,
		afterIntentID string,
		limit int,
	) ([]SharedProjectionIntentRow, error)
}

func (r *repoDependencyShardReader) ListPendingDomainIntents(
	ctx context.Context,
	domain string,
	limit int,
) ([]SharedProjectionIntentRow, error) {
	rows, err := r.inner.ListPendingDomainIntents(ctx, domain, maxRepoDependencyAcceptanceScanLimit)
	if err != nil {
		return nil, err
	}
	owned := make([]SharedProjectionIntentRow, 0, min(len(rows), max(limit, 1)))
	for {
		owned = r.appendOwnedPendingRows(owned, rows, limit)
		if limit > 0 && len(owned) >= limit {
			return owned[:limit], nil
		}
		if len(rows) < maxRepoDependencyAcceptanceScanLimit {
			return owned, nil
		}

		continuation, ok := r.inner.(repoDependencyPendingIntentContinuationReader)
		if !ok {
			return nil, fmt.Errorf(
				"repo dependency shard %d/%d requires pending-intent continuation after a full %d-row page",
				r.shardID,
				r.shardCount,
				maxRepoDependencyAcceptanceScanLimit,
			)
		}
		last := rows[len(rows)-1]
		rows, err = continuation.ListPendingDomainIntentsAfter(
			ctx,
			domain,
			last.CreatedAt,
			last.IntentID,
			maxRepoDependencyAcceptanceScanLimit,
		)
		if err != nil {
			return nil, fmt.Errorf("continue pending repo dependency intents for shard %d/%d: %w", r.shardID, r.shardCount, err)
		}
		if len(rows) > 0 && !repoDependencyPendingCursorAfter(rows[0], last) {
			return nil, fmt.Errorf(
				"repo dependency shard %d/%d pending-intent continuation did not advance after (%s, %q)",
				r.shardID,
				r.shardCount,
				last.CreatedAt.UTC().Format(time.RFC3339Nano),
				last.IntentID,
			)
		}
	}
}

func repoDependencyPendingCursorAfter(row, cursor SharedProjectionIntentRow) bool {
	return row.CreatedAt.After(cursor.CreatedAt) ||
		(row.CreatedAt.Equal(cursor.CreatedAt) && row.IntentID > cursor.IntentID)
}

func (r *repoDependencyShardReader) appendOwnedPendingRows(
	owned []SharedProjectionIntentRow,
	rows []SharedProjectionIntentRow,
	limit int,
) []SharedProjectionIntentRow {
	for _, row := range rows {
		acceptanceUnitID, ok := repoDependencyAcceptanceUnitID(row)
		if !ok {
			// Keep malformed rows visible to worker zero so the shipped validation
			// path fails closed instead of silently filtering them out.
			if r.shardID == 0 {
				owned = append(owned, row)
			}
		} else if ifaRepoDependencyAcceptanceShard(acceptanceUnitID, r.shardCount) == r.shardID {
			owned = append(owned, row)
		}
		if limit > 0 && len(owned) >= limit {
			return owned
		}
	}
	return owned
}

func (r *repoDependencyShardReader) ListAcceptanceUnitDomainIntents(
	ctx context.Context,
	acceptanceUnitID string,
	domain string,
	limit int,
) ([]SharedProjectionIntentRow, error) {
	return r.inner.ListAcceptanceUnitDomainIntents(ctx, acceptanceUnitID, domain, limit)
}

func (r *repoDependencyShardReader) MarkIntentsCompleted(
	ctx context.Context,
	intentIDs []string,
	completedAt time.Time,
) error {
	return r.inner.MarkIntentsCompleted(ctx, intentIDs, completedAt)
}

func ifaRepoDependencyAcceptanceShard(acceptanceUnitID string, shardCount int) int {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(acceptanceUnitID))
	return int(hasher.Sum32() % uint32(shardCount))
}
