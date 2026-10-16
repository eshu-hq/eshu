// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cpubudget"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RunDeferredBackfillBatchesWith is runDeferredBackfillBatches with the
// per-batch under-lock generation read supplied by the caller; see
// writeDeferredBackfillBatchWith. It merges both per-batch maps: contributions
// for the fan-in publisher, and inserted-rows-per-partition for the pass's
// memo-hit skip-set revision (issue #7636).
func (s Store) RunDeferredBackfillBatchesWith(
	ctx context.Context,
	repoIDs []string,
	bounds [][2]int,
	workers int,
	evidenceBySourceRepo map[string][]relationships.EvidenceFact,
	snapshotGenerations map[string]string,
	instruments *telemetry.Instruments,
	loadGenerations RepositoryGenerationLoader,
) (map[ScopeGenerationPartition][]string, map[ScopeGenerationPartition]int64, error) {
	totalBatches := len(bounds)
	groupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu            sync.Mutex
		contributions = make(map[ScopeGenerationPartition][]string)
		insertedRows  = make(map[ScopeGenerationPartition]int64)
		firstErr      error
	)
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for i := range bounds {
		mu.Lock()
		stop := firstErr != nil
		mu.Unlock()
		if stop || groupCtx.Err() != nil {
			break
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(batchIndex int, lo, hi int) {
			defer wg.Done()
			defer func() { <-sem }()

			batchStart := time.Now()
			batchContributions, batchInsertedRows, err := s.writeDeferredBackfillBatchWith(
				groupCtx, repoIDs[lo:hi], evidenceBySourceRepo, snapshotGenerations, loadGenerations,
			)
			batchDuration := time.Since(batchStart).Seconds()

			mu.Lock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
				return
			}
			for partition, partitionRepoIDs := range batchContributions {
				contributions[partition] = append(contributions[partition], partitionRepoIDs...)
			}
			for partition, inserted := range batchInsertedRows {
				insertedRows[partition] += inserted
			}
			batchPartitions := len(batchContributions)
			mu.Unlock()

			// Per-batch progress signal, emitted OUTSIDE the contribution-map lock:
			// each batch commits independently, so recording duration and a
			// completion count here lets an operator watch the backfill advance
			// batch-by-batch instead of seeing nothing until the whole pass returns
			// (the gap that hid the issue #3704 long pole). The OTEL instruments are
			// internally synchronized, so they must not extend the shared-state
			// critical section. Readiness is no longer published per batch, so the
			// line reports the partitions this batch contributed evidence to; the
			// readiness count for the pass lands in deferred_backfill_fanin_completed.
			if instruments != nil {
				instruments.DeferredBackfillBatchDuration.Record(ctx, batchDuration)
				instruments.DeferredBackfillBatchesCompleted.Add(ctx, 1)
			}
			log.Printf(
				"deferred_backfill_batch_committed batch=%d total_batches=%d repos=%d partitions=%d duration_s=%.2f workers=%d",
				batchIndex+1, totalBatches, hi-lo, batchPartitions, batchDuration, workers,
			)
		}(i, bounds[i][0], bounds[i][1])
	}

	wg.Wait()

	if firstErr != nil {
		return nil, nil, firstErr
	}
	return contributions, insertedRows, nil
}

// DeferredBackfillWorkerCount returns the number of deferred-maintenance batch
// transactions processed concurrently. ESHU_DEFERRED_BACKFILL_CONCURRENCY
// overrides; an unset or invalid value derives from NumCPU clamped to
// deferredBackfillMaxWorkers. Batches each hold one pooled connection and never
// nest a second acquisition, so a worker count above the pool size throttles on
// Begin rather than deadlocking; at ESHU_POSTGRES_MAX_OPEN_CONNS=1 operators set
// ESHU_DEFERRED_BACKFILL_CONCURRENCY=1 and the pass runs serially.
func DeferredBackfillWorkerCount() int {
	if raw := strings.TrimSpace(os.Getenv("ESHU_DEFERRED_BACKFILL_CONCURRENCY")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if n > deferredBackfillMaxWorkers {
				return deferredBackfillMaxWorkers
			}
			return n
		}
	}
	return deferredBackfillDefaultWorkerCount(cpubudget.UsableCPUs())
}

func deferredBackfillDefaultWorkerCount(numCPU int) int {
	if numCPU < 1 {
		return 1
	}
	if numCPU > deferredBackfillMaxWorkers {
		return deferredBackfillMaxWorkers
	}
	return numCPU
}

const (
	// deferredBackfillMaxWorkers is the hard ceiling for the default and for an
	// operator opt-up via ESHU_DEFERRED_BACKFILL_CONCURRENCY, matching the
	// content-writer batch cap.
	deferredBackfillMaxWorkers = 8
)

// RepositoryGenerationLoader reads the active (scope_id, generation_id) of
// repositories for one deferred-maintenance batch under the batch's locks. The
// whole pass passes LoadAllActiveRepositoryGenerations, which ignores repoIDs
// and keeps the shipped corpus-wide read; the partition-scoped pass passes a
// loader that returns the same rows for the batch's repositories only.
type RepositoryGenerationLoader func(
	ctx context.Context,
	queryer db.Queryer,
	repoIDs []string,
) (map[string]RepositoryGenerationIdentity, error)
