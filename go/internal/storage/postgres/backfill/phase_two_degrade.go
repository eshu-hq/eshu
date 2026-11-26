// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// PhaseTwoDegraded records a deferred-backfill pass whose phase-two ArgoCD
// config reload timed out on the budget sentinel while phase one succeeded.
// The pass continues on the unmerged phase-one facts: a sound subset, since
// MergeRelationshipFacts is additive and fewer facts can only omit edges,
// never invent them. The parked config repos reconverge on the next natural
// drain, which re-derives them from persisted facts, so no durable park
// state exists.
type PhaseTwoDegraded struct {
	// ParkedConfigRepos is the number of config repos the timed-out phase two
	// did not load this pass. Counts only: block lines never carry IDs.
	ParkedConfigRepos int
	// AffectedPartitions is the number of loaded partitions the withheld
	// fan-in covers. The fallback withholds the whole fan-in (config refs
	// carry only ConfigRepoID, no source partition), so this equals the
	// loaded partition count.
	AffectedPartitions int
}

// degradePhaseTwoOnBudgetTimeout branches a phase-two ArgoCD config load
// error: ONLY an error satisfying ErrRelationshipFactLoadBudgetExceeded (the
// statement_timeout/deadline mapping, never a real byte-budget breach on this
// path) degrades the pass. It ends the fact_load block degraded with the
// parked counts and reports the degraded state. Every other error returns
// false so the caller keeps today's fail-loud path unchanged.
func degradePhaseTwoOnBudgetTimeout(
	block *BlockSpan,
	err error,
	loaded []facts.Envelope,
	catalog []relationships.CatalogEntry,
	partitions int,
	workers int,
) (*PhaseTwoDegraded, bool) {
	if !errors.Is(err, ErrRelationshipFactLoadBudgetExceeded) {
		return nil, false
	}
	parked := len(relationships.ResolveArgoCDGeneratorConfigRepos(loaded, catalog))
	block.EndDegraded(err,
		IntAttr("partitions", partitions),
		IntAttr("workers", workers),
		IntAttr("parked_config_repos", parked),
		IntAttr("affected_partitions", partitions),
	)
	return &PhaseTwoDegraded{ParkedConfigRepos: parked, AffectedPartitions: partitions}, true
}

// deferredBatchResult carries one pass's committed evidence batches to the
// fan-in step: the per-partition contribution sets, the actually-inserted
// row counts for the memo-hit revision, and the batch count and end time for
// the write_batches block the fan-in attributes join. empty reports a pass
// with no active repository generations: nothing committed, and the fan-in
// has nothing to publish.
type deferredBatchResult struct {
	contributions map[ScopeGenerationPartition][]string
	insertedRows  map[ScopeGenerationPartition]int64
	batchCount    int
	workers       int
	batchesEnd    time.Time
	empty         bool
}

// commitDeferredBackfillBatches runs the pass's bounded per-repository batch
// writes and returns the per-partition contributions for the fan-in step. A
// failed batch ends the write_batches block failed and fails the pass, as
// before: publishing readiness over partly committed evidence would claim a
// completion the pass did not achieve. It performs no publication itself,
// so a degraded pass (phase two parked) can commit evidence through it while
// withholding the whole fan-in.
func (s Store) commitDeferredBackfillBatches(
	ctx context.Context,
	evidenceBySourceRepo map[string][]relationships.EvidenceFact,
	snapshotGenerations map[string]string,
	instruments *telemetry.Instruments,
) (deferredBatchResult, *BlockSpan, error) {
	var result deferredBatchResult
	batchBlock := startBlockFromContext(ctx, BlockWriteBatches)
	repoGenerations, err := LoadActiveRepositoryGenerations(ctx, s.DB)
	if err != nil {
		batchBlock.End(err)
		return result, nil, fmt.Errorf("load active repository generations for deferred relationship backfill: %w", err)
	}
	if len(repoGenerations) == 0 {
		result.empty = true
		return result, batchBlock, nil
	}

	repoIDs := make([]string, 0, len(repoGenerations))
	for repoID := range repoGenerations {
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)

	batchSize := s.BatchSize
	if batchSize <= 0 {
		batchSize = DeferredMaintenanceRepoBatchSize
	}

	// Partition the sorted repository list into disjoint per-batch slices.
	// Because the list is sorted and the slices are contiguous and
	// non-overlapping, no two batches request the same repository advisory
	// lock, so concurrent batches cannot deadlock on lock ordering (each
	// batch also sorts its own keys). Each batch transaction holds exactly
	// one pooled connection for its lifetime and never acquires a second, so
	// running W batches concurrently can at most block on Begin when the
	// pool is smaller than W; it cannot deadlock. At
	// ESHU_POSTGRES_MAX_OPEN_CONNS=1 the pool self-serializes the batches.
	bounds := make([][2]int, 0, (len(repoIDs)+batchSize-1)/batchSize)
	for start := 0; start < len(repoIDs); start += batchSize {
		end := start + batchSize
		if end > len(repoIDs) {
			end = len(repoIDs)
		}
		bounds = append(bounds, [2]int{start, end})
	}

	workers := s.Workers
	if workers < 1 {
		workers = 1
	}
	if workers > len(bounds) {
		workers = len(bounds)
	}

	contributions, insertedRows, err := s.runDeferredBackfillBatches(
		ctx, repoIDs, bounds, workers, evidenceBySourceRepo, snapshotGenerations, instruments,
	)
	batchesEnd := time.Now()
	if err != nil {
		batchBlock.EndAt(batchesEnd, err, IntAttr("batches", len(bounds)))
		// A failed or canceled batch means at least one partition's evidence is
		// only partly committed. Publishing readiness or a memo row for ANY
		// partition now would claim completion the pass did not achieve, and
		// the memo in particular is durable: ApplyDeferredPartitionMemoGate
		// would skip that partition's fact load on every later pass until the
		// catalog fingerprint changes. Withhold the whole fan-in; the committed
		// evidence is content-addressed and the next pass re-derives and
		// republishes it.
		return result, nil, err
	}
	if err := ctx.Err(); err != nil {
		batchBlock.EndAt(batchesEnd, err, IntAttr("batches", len(bounds)))
		return result, nil, fmt.Errorf("deferred backfill canceled before readiness publication: %w", err)
	}
	result.contributions = contributions
	result.insertedRows = insertedRows
	result.batchCount = len(bounds)
	result.workers = workers
	result.batchesEnd = batchesEnd
	return result, batchBlock, nil
}

// logDegradedPassCompleted emits the pass-completed line for a degraded pass.
// It never uses the clean deferred_backfill_completed shape: readiness_rows
// is always zero (the whole fan-in was withheld) and the parked counts name
// what the next pass must reconverge.
func logDegradedPassCompleted(
	dur float64,
	totalEvidence int64,
	committed int64,
	degraded *PhaseTwoDegraded,
) {
	log.Printf("deferred_backfill_completed_degraded evidence_facts=%d readiness_rows=0 committed_evidence_rows=%d duration_s=%.2f batch_size=%d parked_config_repos=%d affected_partitions=%d",
		totalEvidence, committed, dur, DeferredMaintenanceRepoBatchSize, degraded.ParkedConfigRepos, degraded.AffectedPartitions)
}
