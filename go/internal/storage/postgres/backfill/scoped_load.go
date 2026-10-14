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

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// LoadDeferredAnchorScopedRelationshipFacts is the deferred-backfill replacement
// for LoadAnchorScopedRelationshipFacts (issue #3659), partitioned per scope and
// fanned out across the deferred-maintenance worker pool (issue #3710).
//
// It loads the active (scope_id, generation_id) partitions, then issues one
// self-exclusion query (ListDeferredScopedRelationshipFactRecordsQuery) per
// partition, bounded by $3/$4. The per-scope bound turns the prior single
// O(facts × catalog) corpus scan — the pass long pole — into many partition-bounded
// scans that run concurrently (corpus wall-time pending the remote run). The $1/$2
// catalog parameters are built once and shared across partitions.
//
// The partition source is LoadActiveScopeGenerationPartitions, NOT
// LoadActiveRepositoryGenerations. The latter filters fact_kind = 'repository' and
// keys on a derived repo_id, so it covers only git scopes and collapses scopes
// whose COALESCE(repo_id, graph_id, name) collides. Partitioning on that set would
// (a) drop EVERY gcp_cloud_relationship fact, which lives in cloud scopes that
// carry no repository fact, and (b) lose one of any two collapsing scopes. The
// scope-generation source covers exactly the latest_generations the deferred query
// joins to — every scope with a content/file/gcp_cloud_relationship fact in its
// latest generation — keyed on the (scope_id, generation_id) pair, so the loaded
// fact set is the superset the single corpus scan produced.
//
// The fan-out mirrors the deferred write pool (runDeferredBackfillBatches): a
// bounded worker pool, a first-error latch that cancels the remaining work
// through ctx, and per-partition result slices merged after all workers finish.
// Each query holds one pooled connection for its lifetime and never nests a
// second acquisition, so a worker count above the pool size throttles on the
// query rather than deadlocking. Partitions are disjoint by (scope_id,
// generation_id), so the reads never contend.
//
// Phase two (ArgoCD config-repo reload) runs once after the per-scope facts are
// merged: an ApplicationSet's external git-generator config repo may live in any
// scope, so it is resolved against the full merged fact set and the full catalog.
//
// The whole set of loaded facts is fed to relationships.DiscoverEvidence by the
// caller, which applies the in-memory catalogMatcher (self-match drop on
// entry.RepoID == sourceRepoID plus boundary-safe token matching). That matcher
// is the single refinement point, so the LIKE-superset SQL (issue #3710) cannot
// leak extra evidence: every loaded fact is re-matched against the catalog before
// any evidence is emitted.
//
// The fourth return value is the set of (scope_id, generation_id) partitions this
// pass's Track-1 memo gate (ApplyDeferredPartitionMemoGate) SKIPPED because they
// were already a memo hit at the START of this pass — i.e. partitions whose
// backward evidence is genuinely unchanged this pass, not merely partitions that
// happen to have a memo row after this pass finishes writing. This is the only
// set the deferred reopen gate (issue #4770 same-pass fix) may treat as
// reopen-redundant; see RunDeferredRelationshipMaintenance. It is nil (not an
// empty, non-nil map) whenever the gate never ran (no queryer, no buildable
// params, or a gate lookup error), so callers must treat nil as "no partition is
// provably redundant" rather than "every partition is redundant".
func (s Store) LoadDeferredAnchorScopedRelationshipFacts(
	ctx context.Context,
	queryer db.Queryer,
	catalog []relationships.CatalogEntry,
	instruments *telemetry.Instruments,
) ([]facts.Envelope, map[string]string, map[ScopeGenerationPartition]struct{}, error) {
	if queryer == nil {
		return nil, nil, nil, nil
	}

	paramsBlock := startBlockFromContext(ctx, BlockParamsBuild)
	params, ok := BuildDeferredScopedFactQueryParams(catalog)
	paramsBlock.End(nil, IntAttr("anchor_terms", len(params.NonRepoIDLike)+len(params.RepoIDValues)))
	if !ok {
		// No anchor and no repo_id value: no content/file/gcp fact can resolve a
		// catalog target, so skip the fact load entirely. A nil snapshot disables
		// the write-phase generation guard so readiness still publishes for every
		// active repository (the legacy no-anchor contract).
		return nil, nil, nil, nil
	}

	listBlock := startBlockFromContext(ctx, BlockPartitionList)
	partitions, err := LoadActiveScopeGenerationPartitions(ctx, queryer)
	listBlock.End(err, IntAttr("partitions", len(partitions)))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load scope-generation partitions for deferred fact load: %w", err)
	}
	if len(partitions) == 0 {
		return nil, nil, nil, nil
	}

	// snapshotGenerations records the (scope_id -> generation_id) pairs this load
	// queried. The write phase compares it against the generations re-read under
	// the repo lock and skips publishing readiness for any scope whose generation
	// advanced after this snapshot (issue #3725): without it, a per-scope query
	// whose generation moved between this partition read and its own execution
	// loads nothing, yet readiness would still be published for the new generation
	// with no relationship evidence and no guaranteed repair pass.
	snapshotGenerations := make(map[string]string, len(partitions))
	for _, partition := range partitions {
		snapshotGenerations[partition.ScopeID] = partition.GenerationID
	}

	// The gate and the load are the two steps LoadDeferredScopedFactsAcrossPartitions
	// runs, called separately so fact_load (load, merge, and the phase-two ArgoCD
	// config reload) is timed without the memo gate inside it.
	loadPartitions, skippedPartitions := s.applyDeferredMemoGate(ctx, params, partitions, instruments)
	factBlock := startBlockFromContext(ctx, BlockFactLoad)
	loaded, workers, err := s.loadDeferredScopedFactsForPartitions(
		ctx, queryer, params, partitions, loadPartitions, instruments,
	)
	if err != nil {
		factBlock.End(err, IntAttr("partitions", len(loadPartitions)), IntAttr("workers", workers))
		return nil, nil, nil, err
	}
	if len(loaded) == 0 {
		factBlock.End(nil, IntAttr("partitions", len(loadPartitions)), IntAttr("facts", 0), IntAttr("workers", workers))
		return nil, snapshotGenerations, skippedPartitions, nil
	}

	merged, err := s.AppendArgoCDGeneratorConfigFacts(ctx, queryer, catalog, loaded)
	if err != nil {
		factBlock.End(err, IntAttr("partitions", len(loadPartitions)), IntAttr("workers", workers))
		return nil, nil, nil, err
	}
	factBlock.End(nil, IntAttr("partitions", len(loadPartitions)), IntAttr("facts", len(merged)), IntAttr("workers", workers))
	return merged, snapshotGenerations, skippedPartitions, nil
}

// LoadDeferredScopedFactsAcrossPartitions fans the per-scope fact load out across
// a bounded worker pool and returns the merged facts in a deterministic order
// (sorted by (observed_at, fact_id)) so the discovery pass and its tests do not
// depend on goroutine scheduling. The merge is order-independent: each partition
// produces evidence only for its own source facts, so concatenating partition
// results and sorting yields the same fact set a single corpus scan would.
//
// The fan-out preserves the (scope_id, generation_id) partition key (the caller
// pre-sorted the slice), so two scopes that would collapse to the same derived
// repo_id are loaded as independent partitions. Large repo-id arms are split into
// bounded query tasks for the same partition; per-query load duration, the
// partition count, query task count, and worker saturation are emitted as
// aggregate signals so an operator can size the fan-out's contribution without
// per-fact cardinality.
//
// The second return value is the set of partitions ApplyDeferredPartitionMemoGate
// actually skipped THIS call (a start-of-call memo hit) — see the doc comment on
// loadDeferredAnchorScopedRelationshipFacts for why this must be captured here,
// at read time, rather than re-derived later from the memo table.
func (s Store) LoadDeferredScopedFactsAcrossPartitions(
	ctx context.Context,
	queryer db.Queryer,
	params DeferredScopedFactQueryParams,
	partitions []ScopeGenerationPartition,
	instruments *telemetry.Instruments,
) ([]facts.Envelope, map[ScopeGenerationPartition]struct{}, error) {
	loadPartitions, skippedPartitions := s.applyDeferredMemoGate(ctx, params, partitions, instruments)
	merged, _, err := s.loadDeferredScopedFactsForPartitions(
		ctx, queryer, params, partitions, loadPartitions, instruments,
	)
	if err != nil {
		return nil, nil, err
	}
	return merged, skippedPartitions, nil
}

// applyDeferredMemoGate runs the Track-1 partition memo gate and returns the
// partitions to load plus the same-pass skip-set (nil when the gate never ran;
// see LoadDeferredScopedFactsAcrossPartitions). It is the pass's memo_gate
// block: candidate_partitions is every partition considered, skipped the memo
// hits, loaded the partitions the fact load will query.
func (s Store) applyDeferredMemoGate(
	ctx context.Context,
	params DeferredScopedFactQueryParams,
	partitions []ScopeGenerationPartition,
	instruments *telemetry.Instruments,
) ([]ScopeGenerationPartition, map[ScopeGenerationPartition]struct{}) {
	// Partition memo gate (issue #3624 Track 1 / B'): skip re-loading and
	// re-deriving evidence for a partition whose backward evidence already
	// committed under the CURRENT catalog fingerprint. ArgoCD-bearing partitions
	// are excluded on the WRITE side (they never get a memo row), so they never
	// match here and always reload — the read gate is a single indexed memo
	// lookup with no payload scan (see ApplyDeferredPartitionMemoGate). A gate
	// error degrades to "load everything" — the legacy full-load behavior —
	// rather than aborting the pass, because the gate is a performance
	// optimization, never a correctness dependency: the fact load it guards is
	// idempotent and safe to re-run.
	loadPartitions := partitions
	// skippedPartitions is nil only when the Track-1 gate never ran at all (no
	// s.db, or a gate lookup error) — the "no same-pass data available" case
	// applyReopenPartitionMemoGateFromSkipSet's caller must distinguish from
	// "the gate ran and found zero redundant partitions this pass," which is a
	// real, common same-pass outcome (e.g. every partition was a memo miss) and
	// MUST stay a non-nil, empty map so the reopen gate still recognizes it as
	// authoritative same-pass data and reopens every candidate — rather than
	// falling back to nil, which would make the reopen gate re-derive its own
	// answer from the memo table this same pass's backfill just wrote fresh
	// rows into (the exact issue #4770/#4816 bug this return value exists to
	// prevent).
	var skippedPartitions map[ScopeGenerationPartition]struct{}
	if s.DB != nil {
		memoStore := NewDeferredBackfillPartitionMemoStore(s.DB)
		fingerprint := DeferredCatalogFingerprint(params)
		gateBlock := startBlockFromContext(ctx, BlockMemoGate)
		gateResult, err := ApplyDeferredPartitionMemoGate(ctx, memoStore, partitions, fingerprint, instruments)
		if err != nil {
			// The gate is a performance optimization, never a correctness
			// dependency: a lookup error degrades to a full load and the pass
			// continues. The block ends DEGRADED, not failed: its span carries
			// the error and fallback=true, it logs deferred_backfill_block_degraded
			// (a failed line means the pass failed), and the existing gate_failed
			// line below stays the failure signal.
			gateBlock.EndDegraded(err,
				IntAttr("candidate_partitions", len(partitions)),
				IntAttr("skipped", 0),
				IntAttr("loaded", len(partitions)),
			)
			log.Printf("deferred_backfill_partition_memo_gate_failed error=%q partitions=%d falling_back=true", err, len(partitions))
		} else {
			gateBlock.End(nil,
				IntAttr("candidate_partitions", len(partitions)),
				IntAttr("skipped", len(gateResult.Skipped)),
				IntAttr("loaded", len(gateResult.ToLoad)),
			)
			loadPartitions = gateResult.ToLoad
			skippedPartitions = make(map[ScopeGenerationPartition]struct{}, len(gateResult.Skipped))
			for _, partition := range gateResult.Skipped {
				skippedPartitions[partition] = struct{}{}
			}
		}
	}

	return loadPartitions, skippedPartitions
}

// loadDeferredScopedFactsForPartitions is the fan-out half of
// LoadDeferredScopedFactsAcrossPartitions: it loads loadPartitions (the memo
// gate's survivors) across the bounded worker pool and returns the merged,
// ordered facts plus the worker count it used (0 when nothing needed loading).
// partitions is the full candidate set, kept for the existing partition metric
// and log fields.
func (s Store) loadDeferredScopedFactsForPartitions(
	ctx context.Context,
	queryer db.Queryer,
	params DeferredScopedFactQueryParams,
	partitions []ScopeGenerationPartition,
	loadPartitions []ScopeGenerationPartition,
	instruments *telemetry.Instruments,
) ([]facts.Envelope, int, error) {
	tasks := buildDeferredScopedFactLoadTasks(loadPartitions, params)
	if len(tasks) == 0 {
		return nil, 0, nil
	}
	workers := s.Workers
	if workers < 1 {
		workers = 1
	}
	if workers > len(tasks) {
		workers = len(tasks)
	}
	if instruments != nil {
		instruments.DeferredBackfillPartitions.Add(ctx, int64(len(partitions)))
		instruments.DeferredBackfillPartitionWorkers.Record(ctx, int64(workers))
	}

	// Record the fan-out shape on the active relationship.backfill_deferred span
	// (issue #3710) so an operator reads partition cardinality and worker
	// saturation off the trace, not only the deferred_backfill_fact_load_completed
	// log. SpanFromContext returns a no-op span when no tracer started the pass, so
	// SetAttributes is safe whether or not the caller passed a tracer.
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("partition_count", len(partitions)),
		attribute.Int("query_task_count", len(tasks)),
		attribute.Int("worker_count", workers),
	)

	groupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		firstErr error
	)
	// perTask is indexed 1:1 with tasks: worker i writes only perTask[i], so no
	// two workers touch the same slot and the merge below is a lock-free
	// deterministic union. Large repo_id catalogs are split into multiple tasks
	// for the same partition; mergeDeferredScopedTaskFacts drops duplicate fact_id
	// rows when a fact matches more than one chunk.
	perTask := make([][]facts.Envelope, len(tasks))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for i, task := range tasks {
		// Best-effort pre-check: skip dispatching new partitions once a worker has
		// latched the first error (or the shared context is cancelled). It is a
		// best-effort fast-exit, not a correctness gate — the authoritative
		// first-error handling and the cancel() live inside the worker below, so an
		// in-flight worker that errors after this check still aborts the pass.
		mu.Lock()
		stop := firstErr != nil
		mu.Unlock()
		if stop || groupCtx.Err() != nil {
			break
		}

		acquired := false
		select {
		case sem <- struct{}{}:
			acquired = true
		case <-groupCtx.Done():
		}
		if !acquired {
			break
		}
		wg.Add(1)
		go func(index int, task deferredScopedFactLoadTask) {
			defer wg.Done()
			defer func() { <-sem }()

			started := time.Now()
			envelopes, err := loadDeferredScopedRelationshipFactsForPartition(
				groupCtx, queryer, task.params, task.partition.ScopeID, task.partition.GenerationID,
			)
			duration := time.Since(started)
			if instruments != nil {
				instruments.DeferredBackfillPartitionLoadDuration.Record(groupCtx, duration.Seconds())
				instruments.DeferredBackfillPartitionLoadFactCount.Record(groupCtx, int64(len(envelopes)))
			}
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf(
						"load deferred scoped facts for scope %q: %w", task.partition.ScopeID, err,
					)
					// Name the partition that aborted the pass on the span (issue
					// #3710) so an operator sees which scope failed from the trace, not
					// only the returned error string. Recorded once for the causal
					// failure; cancellation cascades on the remaining partitions do not
					// overwrite firstErr and so add no follow-on events.
					span.AddEvent("partition_load_failed", trace.WithAttributes(
						attribute.String("scope_id", task.partition.ScopeID),
					))
					cancel()
				}
				mu.Unlock()
				return
			}
			// partition_load_completed (#5096) promotes the #5094 log to the
			// trace. It is capped and FIFO-evicted, so the log below stays the
			// complete record -- see the "capped and lossy" note in
			// docs/public/reference/telemetry/index.md.
			log.Printf(
				"deferred_backfill_fact_load_task_completed task=%d query_tasks=%d scope_id=%q repo_terms=%d non_repo_terms=%d loaded_facts=%d duration_s=%.2f workers=%d",
				index+1, len(tasks), task.partition.ScopeID, len(task.params.RepoIDValues), len(task.params.NonRepoIDLike),
				len(envelopes), duration.Seconds(), workers,
			)
			span.AddEvent("partition_load_completed", trace.WithAttributes(
				attribute.Int("task", index+1),
				attribute.Int("query_tasks", len(tasks)),
				attribute.String("scope_id", task.partition.ScopeID),
				attribute.Int("repo_terms", len(task.params.RepoIDValues)),
				attribute.Int("non_repo_terms", len(task.params.NonRepoIDLike)),
				attribute.Int("loaded_facts", len(envelopes)),
				attribute.Float64("duration_s", duration.Seconds()),
				attribute.Int("workers", workers),
			))
			perTask[index] = envelopes
		}(i, task)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, workers, firstErr
	}
	if err := groupCtx.Err(); err != nil {
		return nil, workers, err
	}

	merged := mergeDeferredScopedTaskFacts(perTask)
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].ObservedAt.Equal(merged[j].ObservedAt) {
			return merged[i].FactID < merged[j].FactID
		}
		return merged[i].ObservedAt.Before(merged[j].ObservedAt)
	})
	log.Printf(
		"deferred_backfill_fact_load_completed partitions=%d loaded_partitions=%d query_tasks=%d workers=%d loaded_facts=%d",
		len(partitions), len(loadPartitions), len(tasks), workers, len(merged),
	)
	return merged, workers, nil
}

// AppendArgoCDGeneratorConfigFacts runs the deferred backfill's phase-two ArgoCD
// config-repo reload over the merged per-scope facts. An ArgoCD ApplicationSet's
// git generator references its deploy repo only through template parameters that
// are not in the ApplicationSet fact's own payload, so the external config repo's
// files are reloaded by repo_id and merged (de-duplicated by FactID) into the
// loaded facts. The config catalog is the full catalog so a config repo not in
// the anchor set is still resolvable.
func (s Store) AppendArgoCDGeneratorConfigFacts(
	ctx context.Context,
	queryer db.Queryer,
	catalog []relationships.CatalogEntry,
	loaded []facts.Envelope,
) ([]facts.Envelope, error) {
	configRefs := relationships.ResolveArgoCDGeneratorConfigRepos(loaded, catalog)
	if len(configRefs) == 0 {
		return loaded, nil
	}

	configRepoIDs := make([]string, 0, len(configRefs))
	for _, ref := range configRefs {
		configRepoIDs = append(configRepoIDs, ref.ConfigRepoID)
	}
	configFacts, err := LoadArgoCDGeneratorConfigFacts(ctx, queryer, configRepoIDs)
	if err != nil {
		return nil, fmt.Errorf("load argocd generator config facts for deferred relationship backfill: %w", err)
	}
	return MergeRelationshipFacts(loaded, configFacts), nil
}
