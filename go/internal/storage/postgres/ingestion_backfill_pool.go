// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cpubudget"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// runDeferredBackfillBatches executes the partitioned per-repository batches with
// a bounded worker pool and accumulates, per (scope, generation) partition, the
// repositories whose evidence committed. The batches are independent (disjoint
// repository sets, idempotent ON CONFLICT writes, per-batch transaction scope),
// so the only shared mutable state is the contribution map and the first-error
// latch, both guarded. The first failing batch cancels the remaining work through
// ctx so a partial pass stops promptly; the deferred maintenance pass is
// idempotent and re-runs converge.
//
// The returned map is the input to publishDeferredBackfillPartitions. On error it
// is not returned at all: the caller must not publish readiness or a memo row for
// any partition once a batch has failed, because a partition's repositories are
// spread across batches and a survivor's contribution says nothing about whether
// the rest of that partition committed.
func (s IngestionStore) runDeferredBackfillBatches(
	ctx context.Context,
	repoIDs []string,
	bounds [][2]int,
	workers int,
	evidenceBySourceRepo map[string][]relationships.EvidenceFact,
	snapshotGenerations map[string]string,
	instruments *telemetry.Instruments,
) (map[scopeGenerationPartition][]string, error) {
	totalBatches := len(bounds)
	groupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu            sync.Mutex
		contributions = make(map[scopeGenerationPartition][]string)
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
			batchContributions, err := s.writeDeferredBackfillBatch(groupCtx, repoIDs[lo:hi], evidenceBySourceRepo, snapshotGenerations)
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
		return nil, firstErr
	}
	return contributions, nil
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
	deferredBackfillMaxWorkers = 8
)

// publishDeferredBackfillPartitions publishes backward-evidence readiness and
// the partition memo once per (scope, generation) partition, in one transaction
// per partition, AFTER every evidence batch of the pass has committed. It
// returns the number of readiness rows published.
//
// # Why publication is a separate fan-in rather than part of a batch
//
// Evidence batches are fixed-size contiguous slices of the repo-ID-sorted
// corpus, so the repositories of one partition are generally spread across
// several batches. Readiness and the memo are partition-wide claims. Published
// from inside a batch, they assert "this partition's backward evidence is
// committed" while sibling batches carrying the rest of that partition may still
// fail — and the memo is durable, so applyDeferredPartitionMemoGate would then
// skip the partition's fact load on every later pass until the catalog
// fingerprint changes. Waiting for every batch removes that window by
// construction rather than narrowing it.
//
// # Durability contract, including the crash window
//
// Within a partition's transaction the phase row and the memo row commit
// atomically together, and that transaction runs strictly after every evidence
// batch for the partition has committed. The two failure directions are not
// symmetric, deliberately:
//
//   - Memo without complete evidence is FORBIDDEN. It is durable wrong state: a
//     memo row is a claim that the partition's backward evidence is fully
//     determined by the recorded catalog fingerprint, and the read-side gate
//     acts on that claim by skipping the fact load.
//   - Evidence without a memo is TOLERATED and self-healing. It is the state
//     left by a fan-in that failed, by a partition skipped here, and by a
//     process that died anywhere between the last batch commit and this step —
//     all three leave the identical durable state, because a fan-in transaction
//     that does not commit writes nothing at all. A partition with no memo row
//     is a gate MISS in applyDeferredPartitionMemoGate and therefore always
//     reloads; the next pass re-discovers the same evidence, re-upserts it as a
//     no-op (relationship_evidence_facts is content-addressed with
//     ON CONFLICT (evidence_id) DO NOTHING), and publishes then. The phase row
//     is likewise an idempotent ON CONFLICT ... DO UPDATE keyed by generation,
//     so republishing is a no-op rewrite rather than a duplicate.
//
// # Deadlock freedom
//
// The batch phase's argument (disjoint, contiguous, sorted repository slices)
// is unchanged and still covers the batches. The fan-in has its own:
//
//   - Fan-in transactions are per-partition, and partitions are disjoint
//     repository sets. A repository has exactly one active generation, hence
//     exactly one partition, and each batch records a repository under the
//     single partition it observed under that batch's lock. No repository
//     appears in two partitions' lock sets.
//   - Each fan-in transaction acquires its repositories' advisory locks through
//     acquireDeferredMaintenanceRepoExclusiveLocks, which sorts the keys, so
//     every caller in the system takes locks in the same global order.
//   - The fan-in runs only after wg.Wait() in runDeferredBackfillBatches, so it
//     never overlaps a batch transaction of the same pass.
//   - Sorted acquisition of a consistent global order cannot deadlock against a
//     concurrent ingestion commit (which takes the same keys through
//     deferredMaintenanceRepoLockKey) or against another maintenance pass.
//   - Each fan-in transaction holds exactly one pooled connection and never
//     nests a second acquisition, so a worker count above the pool size
//     throttles on Begin rather than deadlocking.
func (s IngestionStore) publishDeferredBackfillPartitions(
	ctx context.Context,
	contributions map[scopeGenerationPartition][]string,
	snapshotGenerations map[string]string,
	catalogFingerprint string,
	workers int,
	instruments *telemetry.Instruments,
) (int, error) {
	if len(contributions) == 0 {
		return 0, nil
	}

	partitions := make([]scopeGenerationPartition, 0, len(contributions))
	for partition := range contributions {
		partitions = append(partitions, partition)
	}
	sort.Slice(partitions, func(i, j int) bool {
		if partitions[i].ScopeID != partitions[j].ScopeID {
			return partitions[i].ScopeID < partitions[j].ScopeID
		}
		return partitions[i].GenerationID < partitions[j].GenerationID
	})

	if workers < 1 {
		workers = 1
	}
	if workers > len(partitions) {
		workers = len(partitions)
	}

	start := time.Now()
	groupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu        sync.Mutex
		published int
		skipped   int
		firstErr  error
	)
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for _, partition := range partitions {
		mu.Lock()
		stop := firstErr != nil
		mu.Unlock()
		if stop || groupCtx.Err() != nil {
			break
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(partition scopeGenerationPartition) {
			defer wg.Done()
			defer func() { <-sem }()

			outcome, err := s.publishDeferredBackfillPartition(
				groupCtx, partition, contributions[partition], snapshotGenerations, catalogFingerprint,
			)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				return
			}
			// Counters increment per partition, in the same critical section that
			// advances the counts the completion log reports, so the metric and the
			// log cannot disagree. Incrementing here rather than once at the end
			// also keeps them accurate when a later partition fails the pass: work
			// that committed stays counted.
			if outcome.Published {
				published++
				if instruments != nil {
					instruments.DeferredBackfillFanInPublished.Add(ctx, 1)
				}
				return
			}
			skipped++
			if instruments != nil {
				instruments.DeferredBackfillFanInSkipped.Add(ctx, 1,
					metric.WithAttributes(attribute.String("reason", outcome.SkipReason)))
			}
		}(partition)
	}

	wg.Wait()

	fanInDuration := time.Since(start).Seconds()
	if instruments != nil {
		instruments.DeferredBackfillFanInDuration.Record(ctx, fanInDuration)
	}
	// Record the publication shape on the active relationship.backfill_deferred
	// span, matching how the fact-load fan-out reports its shape
	// (loadDeferredScopedFactsAcrossPartitions). SpanFromContext returns a no-op
	// span when no tracer started the pass, so this is safe either way, and it
	// keeps the fan-in off a child span of its own -- nothing comparable in this
	// path is separately spanned.
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.Int("fanin_partition_count", len(partitions)),
		attribute.Int("fanin_published_count", published),
		attribute.Int("fanin_skipped_count", skipped),
		attribute.Int("fanin_worker_count", workers),
	)

	if firstErr != nil {
		return published, firstErr
	}

	// Operator-facing progress signal for the publication phase, mirroring
	// deferred_backfill_batch_committed for the evidence phase. published +
	// skipped accounts for every partition the pass committed evidence for, so a
	// non-zero skipped count reads directly as "generations advanced under us"
	// rather than as silently missing readiness.
	log.Printf(
		"deferred_backfill_fanin_completed partitions=%d published=%d skipped=%d duration_s=%.2f workers=%d",
		len(partitions), published, skipped, fanInDuration, workers,
	)

	return published, nil
}

// Closed set of reasons the fan-in declines to publish a partition. They label
// DeferredBackfillFanInSkipped and appear verbatim in the
// deferred_backfill_fanin_partition_skipped log line, so the metric's label
// values and the log's reason field cannot drift apart. Keep the set small and
// bounded: it is a metric label.
const (
	// deferredFanInSkipGenerationAdvanced: the under-lock re-read found the
	// scope on a different active generation than the batch committed against.
	deferredFanInSkipGenerationAdvanced = "generation_advanced_since_batch"
	// deferredFanInSkipSnapshotAdvanced: the partition no longer matches the
	// fact-load snapshot this pass derived its evidence from.
	deferredFanInSkipSnapshotAdvanced = "generation_advanced_since_snapshot"
)

// deferredFanInOutcome reports what one partition's publication attempt did.
// SkipReason is empty when Published is true and otherwise carries one of the
// closed-set reasons above, so the caller can label the skip counter without
// re-deriving why the partition was declined.
type deferredFanInOutcome struct {
	Published  bool
	SkipReason string
}

// publishDeferredBackfillPartition publishes one partition's readiness row and
// memo row in a single transaction, and reports whether it published. It
// acquires the partition's repository locks in the sorted global order, re-reads
// the scope's active generation UNDER those locks, and publishes only when that
// generation still matches the partition the evidence was committed against.
//
// The re-read is the point of taking the locks here. A generation can advance
// between a batch's commit and this step; publishing then would mark a
// superseded generation backward-evidence-committed, and would memoize a
// partition whose facts the next pass must reload. A mismatch is a skip, not an
// error: the partition simply publishes on the pass that observes a stable
// generation.
func (s IngestionStore) publishDeferredBackfillPartition(
	ctx context.Context,
	partition scopeGenerationPartition,
	repoIDs []string,
	snapshotGenerations map[string]string,
	catalogFingerprint string,
) (deferredFanInOutcome, error) {
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return deferredFanInOutcome{}, fmt.Errorf("begin deferred backfill readiness transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	lockKeys := make([]string, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		lockKeys = append(lockKeys, deferredMaintenanceRepoLockKeyFromID(repoID))
	}
	if err := acquireDeferredMaintenanceRepoExclusiveLocks(ctx, tx, lockKeys); err != nil {
		return deferredFanInOutcome{}, fmt.Errorf("acquire deferred backfill readiness locks: %w", err)
	}

	activeGeneration, err := loadActiveGenerationForScope(ctx, tx, partition.ScopeID)
	if err != nil {
		return deferredFanInOutcome{}, fmt.Errorf("reload active generation for scope %q under readiness lock: %w", partition.ScopeID, err)
	}
	if activeGeneration != partition.GenerationID {
		log.Printf(
			"deferred_backfill_fanin_partition_skipped=true scope_id=%q generation_id=%q active_generation_id=%q reason=%q",
			partition.ScopeID, partition.GenerationID, activeGeneration, deferredFanInSkipGenerationAdvanced,
		)
		return deferredFanInOutcome{SkipReason: deferredFanInSkipGenerationAdvanced}, nil
	}
	// Same guard the batch applied (issue #3725), re-applied against the same
	// snapshot so a scope that dropped out of the fact-load snapshot cannot be
	// published by a later step that forgot why the batch had skipped it.
	if snapshotGenerations != nil {
		snapshotGeneration, inSnapshot := snapshotGenerations[partition.ScopeID]
		if !inSnapshot || snapshotGeneration != partition.GenerationID {
			log.Printf(
				"deferred_backfill_fanin_partition_skipped=true scope_id=%q generation_id=%q reason=%q",
				partition.ScopeID, partition.GenerationID, deferredFanInSkipSnapshotAdvanced,
			)
			return deferredFanInOutcome{SkipReason: deferredFanInSkipSnapshotAdvanced}, nil
		}
	}

	now := s.now()
	phaseRow := reducer.GraphProjectionPhaseState{
		Key: reducer.GraphProjectionPhaseKey{
			ScopeID:          partition.ScopeID,
			AcceptanceUnitID: partition.ScopeID,
			SourceRunID:      partition.GenerationID,
			GenerationID:     partition.GenerationID,
			Keyspace:         reducer.GraphProjectionKeyspaceCrossRepoEvidence,
		},
		Phase:       reducer.GraphProjectionPhaseBackwardEvidenceCommitted,
		CommittedAt: now,
		UpdatedAt:   now,
	}
	if err := NewGraphProjectionPhaseStateStore(tx).PublishGraphProjectionPhases(
		ctx, []reducer.GraphProjectionPhaseState{phaseRow},
	); err != nil {
		return deferredFanInOutcome{}, fmt.Errorf("publish backward evidence readiness: %w", err)
	}

	// Partition memo write (issue #3624 Track 1 / B'), committed in the SAME
	// transaction as the phase row above and strictly after every evidence batch
	// for this partition has committed. The invariant this ordering buys is that
	// a memo row never exists unless the partition's evidence AND its readiness
	// are already durable: memo and phase commit or roll back together, and both
	// come last. The reverse direction — committed evidence with no memo row — is
	// the tolerated, self-healing state; publishDeferredBackfillPartitions's doc
	// comment sets out why, and why the crash window between the last batch and
	// this transaction lands there too.
	//
	// ArgoCD-bearing partitions (see listArgoCDBearingPartitionsQuery) are
	// deliberately EXCLUDED from the memo write, not merely gated at read time:
	// their cross-repo evidence can change when a DIFFERENT repo (the external
	// ArgoCD config repo) changes, so writing a memo row for them would still be
	// safe today (the read-side gate always reloads ArgoCD-bearing partitions
	// regardless of memo state) but would misleadingly claim "this partition's
	// evidence is fully determined by its own catalog fingerprint," which is
	// false for the ArgoCD carve-out. Omitting the write keeps that invariant
	// visible in the durable memo state itself, not only in the gate's logic.
	if catalogFingerprint != "" {
		if err := writeDeferredBackfillPartitionMemos(
			ctx, tx, []scopeGenerationPartition{partition}, catalogFingerprint, now,
		); err != nil {
			return deferredFanInOutcome{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return deferredFanInOutcome{}, fmt.Errorf("commit deferred backfill readiness transaction: %w", err)
	}
	committed = true
	return deferredFanInOutcome{Published: true}, nil
}
