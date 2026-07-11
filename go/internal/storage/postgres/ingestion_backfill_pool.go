// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

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
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// runDeferredBackfillBatches executes the partitioned per-repository batches with
// a bounded worker pool and accumulates the published readiness rows. The batches
// are independent (disjoint repository sets, idempotent ON CONFLICT writes,
// per-batch transaction scope), so the only shared mutable state is the readiness
// counter and the first-error latch, both guarded. The first failing batch cancels
// the remaining work through ctx so a partial pass stops promptly; the deferred
// maintenance pass is idempotent and re-runs converge.
func (s IngestionStore) runDeferredBackfillBatches(
	ctx context.Context,
	repoIDs []string,
	bounds [][2]int,
	workers int,
	evidenceBySourceRepo map[string][]relationships.EvidenceFact,
	snapshotGenerations map[string]string,
	catalogFingerprint string,
	instruments *telemetry.Instruments,
) (int, error) {
	totalBatches := len(bounds)
	groupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu        sync.Mutex
		readiness int
		firstErr  error
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
			published, err := s.writeDeferredBackfillBatch(groupCtx, repoIDs[lo:hi], evidenceBySourceRepo, snapshotGenerations, catalogFingerprint)
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
			readiness += published
			mu.Unlock()

			// Per-batch progress signal, emitted OUTSIDE the readiness-counter lock:
			// each batch commits independently, so recording duration and a
			// completion count here lets an operator watch the backfill advance
			// batch-by-batch instead of seeing nothing until the whole pass returns
			// (the gap that hid the issue #3704 long pole). The OTEL instruments are
			// internally synchronized, so they must not extend the shared-counter
			// critical section.
			if instruments != nil {
				instruments.DeferredBackfillBatchDuration.Record(ctx, batchDuration)
				instruments.DeferredBackfillBatchesCompleted.Add(ctx, 1)
			}
			log.Printf(
				"deferred_backfill_batch_committed batch=%d total_batches=%d repos=%d readiness_rows=%d duration_s=%.2f workers=%d",
				batchIndex+1, totalBatches, hi-lo, published, batchDuration, workers,
			)
		}(i, bounds[i][0], bounds[i][1])
	}

	wg.Wait()

	if firstErr != nil {
		return readiness, firstErr
	}
	return readiness, nil
}

// deferredBackfillWorkerCount returns the number of deferred-maintenance batch
// transactions processed concurrently. ESHU_DEFERRED_BACKFILL_CONCURRENCY
// overrides; an unset or invalid value derives from NumCPU clamped to
// deferredBackfillMaxWorkers. Batches each hold one pooled connection and never
// nest a second acquisition, so a worker count above the pool size throttles on
// Begin rather than deadlocking; at ESHU_POSTGRES_MAX_OPEN_CONNS=1 operators set
// ESHU_DEFERRED_BACKFILL_CONCURRENCY=1 and the pass runs serially.
func deferredBackfillWorkerCount() int {
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
	deferredBackfillMaxWorkers = 16
)
