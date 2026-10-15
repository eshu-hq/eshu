// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// PublishDeferredBackfillPartitions publishes backward-evidence readiness and
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
// fail — and the memo is durable, so ApplyDeferredPartitionMemoGate would then
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
//     is a gate MISS in ApplyDeferredPartitionMemoGate and therefore always
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
//     lockstore.AcquireDeferredMaintenanceRepoExclusiveLocks, which sorts the keys, so
//     every caller in the system takes locks in the same global order.
//   - The fan-in runs only after wg.Wait() in runDeferredBackfillBatches, so it
//     never overlaps a batch transaction of the same pass.
//   - Sorted acquisition of a consistent global order cannot deadlock against a
//     concurrent ingestion commit (which takes the same keys through
//     lockstore.DeferredMaintenanceRepoLockKey) or against another maintenance pass.
//   - Each fan-in transaction holds exactly one pooled connection and never
//     nests a second acquisition, so a worker count above the pool size
//     throttles on Begin rather than deadlocking.
//
// # Unchanged partitions (#8082)
//
// This exported entry point publishes every contributed partition; the
// partition-scoped pass (#7584) calls it. The whole pass calls
// publishChangedDeferredBackfillPartitions instead, with the set of partitions
// it provably did not change, and leaves those untouched when their readiness
// row is present (see splitUnchangedDeferredPartitions for the invariant).
// Skipped partitions take no lock and open no transaction, so the deadlock
// argument above covers exactly the partitions that still publish.
func (s Store) PublishDeferredBackfillPartitions(
	ctx context.Context,
	contributions map[ScopeGenerationPartition][]string,
	snapshotGenerations map[string]string,
	catalogFingerprint string,
	workers int,
	instruments *telemetry.Instruments,
) (int, error) {
	result, err := s.publishChangedDeferredBackfillPartitions(
		ctx, contributions, nil, snapshotGenerations, catalogFingerprint, workers, instruments,
	)
	return result.published, err
}

// deferredFanInResult counts what one fan-in did. skipped is every partition
// not published: the generation-advanced skips plus skippedUnchanged.
type deferredFanInResult struct {
	published             int
	skipped               int
	skippedUnchanged      int
	unchangedMissingPhase int
}

// publishChangedDeferredBackfillPartitions is PublishDeferredBackfillPartitions
// minus the partitions in unchanged whose readiness row already exists. A nil
// or empty unchanged publishes every partition, exactly like the exported
// entry point.
func (s Store) publishChangedDeferredBackfillPartitions(
	ctx context.Context,
	contributions map[ScopeGenerationPartition][]string,
	unchanged map[ScopeGenerationPartition]struct{},
	snapshotGenerations map[string]string,
	catalogFingerprint string,
	workers int,
	instruments *telemetry.Instruments,
) (deferredFanInResult, error) {
	if len(contributions) == 0 {
		return deferredFanInResult{}, nil
	}

	partitions := make([]ScopeGenerationPartition, 0, len(contributions))
	for partition := range contributions {
		partitions = append(partitions, partition)
	}
	sort.Slice(partitions, func(i, j int) bool {
		if partitions[i].ScopeID != partitions[j].ScopeID {
			return partitions[i].ScopeID < partitions[j].ScopeID
		}
		return partitions[i].GenerationID < partitions[j].GenerationID
	})

	start := time.Now()
	split := s.splitUnchangedDeferredPartitions(ctx, partitions, unchanged)
	if split.skippedUnchanged > 0 && instruments != nil {
		instruments.DeferredBackfillFanInSkipped.Add(ctx, int64(split.skippedUnchanged),
			metric.WithAttributes(attribute.String("reason", deferredFanInSkipUnchanged)))
	}

	if workers < 1 {
		workers = 1
	}
	if workers > len(split.toPublish) {
		workers = len(split.toPublish)
	}

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

	for _, partition := range split.toPublish {
		mu.Lock()
		stop := firstErr != nil
		mu.Unlock()
		if stop || groupCtx.Err() != nil {
			break
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(partition ScopeGenerationPartition) {
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

	result := deferredFanInResult{
		published:             published,
		skipped:               skipped + split.skippedUnchanged,
		skippedUnchanged:      split.skippedUnchanged,
		unchangedMissingPhase: split.unchangedMissingPhase,
	}
	fanInDuration := time.Since(start).Seconds()
	if instruments != nil {
		instruments.DeferredBackfillFanInDuration.Record(ctx, fanInDuration)
	}
	// Record the publication shape on the active relationship.backfill_deferred
	// span, matching how the fact-load fan-out reports its shape
	// (loadDeferredScopedFactsAcrossPartitions). SpanFromContext returns a no-op
	// span when no tracer started the pass, so this is safe either way. The
	// caller also times the fan-in as its own relationship.backfill.fanin child
	// span (#8082); these attributes stay on the pass span for existing readers.
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.Int("fanin_partition_count", len(partitions)),
		attribute.Int("fanin_published_count", result.published),
		attribute.Int("fanin_skipped_count", result.skipped),
		attribute.Int("fanin_skipped_unchanged_count", result.skippedUnchanged),
		attribute.Int("fanin_unchanged_missing_phase_count", result.unchangedMissingPhase),
		attribute.Int("fanin_worker_count", workers),
	)

	if firstErr != nil {
		return result, firstErr
	}

	// Operator-facing progress signal for the publication phase, mirroring
	// deferred_backfill_batch_committed for the evidence phase. Its shape is
	// pinned. published + skipped accounts for every partition the pass
	// committed evidence for. On the whole pass skipped includes the unchanged
	// partitions; the deferred_backfill_fanin_unchanged_checked line before it
	// gives that part, and the rest reads as "generations advanced under us".
	log.Printf(
		"deferred_backfill_fanin_completed partitions=%d published=%d skipped=%d duration_s=%.2f workers=%d",
		len(partitions), result.published, result.skipped, fanInDuration, workers,
	)

	return result, nil
}

// Closed set of reasons the fan-in declines to publish a partition. They label
// DeferredBackfillFanInSkipped. The two generation-advanced reasons also appear
// verbatim in the deferred_backfill_fanin_partition_skipped log line, so the
// metric's label values and the log's reason field cannot drift apart. Keep
// the set small and bounded: it is a metric label.
const (
	// deferredFanInSkipGenerationAdvanced: the under-lock re-read found the
	// scope on a different active generation than the batch committed against.
	deferredFanInSkipGenerationAdvanced = "generation_advanced_since_batch"
	// deferredFanInSkipSnapshotAdvanced: the partition no longer matches the
	// fact-load snapshot this pass derived its evidence from.
	deferredFanInSkipSnapshotAdvanced = "generation_advanced_since_snapshot"
	// deferredFanInSkipUnchanged: the whole pass did not change the
	// partition's backward evidence, its scope is still on the partition's
	// generation, and its readiness row is already committed (#8082,
	// splitUnchangedDeferredPartitions). It is not a withheld partition (its
	// readiness is open), so it logs no
	// deferred_backfill_fanin_partition_skipped line; the
	// deferred_backfill_fanin_unchanged_checked line counts it. A memo hit
	// whose scope moved to a new generation before that check keeps
	// generation_advanced_since_batch. The check takes no lock, so a move that
	// commits after it can still be counted here; the pass writes nothing for
	// the superseded generation in either case.
	deferredFanInSkipUnchanged = "unchanged_since_memo"
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
func (s Store) publishDeferredBackfillPartition(
	ctx context.Context,
	partition ScopeGenerationPartition,
	repoIDs []string,
	snapshotGenerations map[string]string,
	catalogFingerprint string,
) (deferredFanInOutcome, error) {
	tx, err := s.Beginner.Begin(ctx)
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
		lockKeys = append(lockKeys, lockstore.DeferredMaintenanceRepoLockKeyFromID(repoID))
	}
	if err := lockstore.AcquireDeferredMaintenanceRepoExclusiveLocks(ctx, tx, lockKeys); err != nil {
		return deferredFanInOutcome{}, fmt.Errorf("acquire deferred backfill readiness locks: %w", err)
	}

	activeGeneration, err := LoadActiveGenerationForScope(ctx, tx, partition.ScopeID)
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
	if s.NewPhaseStore == nil {
		return deferredFanInOutcome{}, fmt.Errorf("phase store constructor is required for deferred backfill readiness publication")
	}
	if err := s.NewPhaseStore(tx).PublishGraphProjectionPhases(
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
	// the tolerated, self-healing state; PublishDeferredBackfillPartitions's doc
	// comment sets out why, and why the crash window between the last batch and
	// this transaction lands there too.
	//
	// ArgoCD-bearing partitions (see ListArgoCDBearingPartitionsQuery) are
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
			ctx, tx, []ScopeGenerationPartition{partition}, catalogFingerprint, now,
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
