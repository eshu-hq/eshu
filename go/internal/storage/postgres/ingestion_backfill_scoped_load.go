// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// scopeGenerationPartition identifies one (scope_id, generation_id) the deferred
// relationship backfill's fact load fans out over (issue #3710). Each partition is
// one scope's latest generation; the pair is the partition key, so two scopes that
// would collapse to the same derived repo_id stay distinct.
type scopeGenerationPartition struct {
	ScopeID      string
	GenerationID string
}

// deferredScopedRepoIDChunkSize keeps the repo-id self-exclusion arm bounded for
// oversized catalogs without multiplying the representative 896-repository
// corpus into repeated per-scope scans.
const deferredScopedRepoIDChunkSize = 1024

type deferredScopedFactLoadTask struct {
	partition scopeGenerationPartition
	params    deferredScopedFactQueryParams
}

// loadActiveScopeGenerationPartitions returns the (scope_id, generation_id) pairs
// for every scope's latest generation, sorted by (scope_id, generation_id) for a
// deterministic fan-out order. It is the partition source for the deferred
// relationship backfill's per-scope fact load (issue #3710): unlike
// loadActiveRepositoryGenerations it does not filter to fact_kind = 'repository',
// so cloud scopes that carry gcp_cloud_relationship facts but no repository fact
// are still partitioned, and it keys on the (scope_id, generation_id) pair so no
// two scopes collapse. The set is exactly the latest_generations the deferred
// query joins against, so every content/file/gcp_cloud_relationship fact the prior
// single corpus scan covered is reachable through some partition.
//
// The partition set is a start-of-pass snapshot (a single read, not a serializable
// transaction). A generation that activates after the snapshot but before the
// per-scope queries run is picked up on the next deferred pass, since readiness is
// republished each pass; this matches the visibility window of the prior single
// corpus scan and is not a regression.
func loadActiveScopeGenerationPartitions(
	ctx context.Context,
	queryer Queryer,
) ([]scopeGenerationPartition, error) {
	if queryer == nil {
		return nil, nil
	}

	rows, err := queryer.QueryContext(ctx, activeScopeGenerationPartitionsQuery)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var partitions []scopeGenerationPartition
	for rows.Next() {
		var partition scopeGenerationPartition
		if err := rows.Scan(&partition.ScopeID, &partition.GenerationID); err != nil {
			return nil, err
		}
		if strings.TrimSpace(partition.ScopeID) == "" || strings.TrimSpace(partition.GenerationID) == "" {
			continue
		}
		partitions = append(partitions, partition)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return partitions, nil
}

// loadDeferredAnchorScopedRelationshipFacts is the deferred-backfill replacement
// for loadAnchorScopedRelationshipFacts (issue #3659), partitioned per scope and
// fanned out across the deferred-maintenance worker pool (issue #3710).
//
// It loads the active (scope_id, generation_id) partitions, then issues one
// self-exclusion query (listDeferredScopedRelationshipFactRecordsQuery) per
// partition, bounded by $3/$4. The per-scope bound turns the prior single
// O(facts × catalog) corpus scan — the pass long pole — into many partition-bounded
// scans that run concurrently (corpus wall-time pending the remote run). The $1/$2
// catalog parameters are built once and shared across partitions.
//
// The partition source is loadActiveScopeGenerationPartitions, NOT
// loadActiveRepositoryGenerations. The latter filters fact_kind = 'repository' and
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
func (s IngestionStore) loadDeferredAnchorScopedRelationshipFacts(
	ctx context.Context,
	queryer Queryer,
	catalog []relationships.CatalogEntry,
	instruments *telemetry.Instruments,
) ([]facts.Envelope, map[string]string, error) {
	if queryer == nil {
		return nil, nil, nil
	}

	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		// No anchor and no repo_id value: no content/file/gcp fact can resolve a
		// catalog target, so skip the fact load entirely. A nil snapshot disables
		// the write-phase generation guard so readiness still publishes for every
		// active repository (the legacy no-anchor contract).
		return nil, nil, nil
	}

	partitions, err := loadActiveScopeGenerationPartitions(ctx, queryer)
	if err != nil {
		return nil, nil, fmt.Errorf("load scope-generation partitions for deferred fact load: %w", err)
	}
	if len(partitions) == 0 {
		return nil, nil, nil
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

	loaded, err := s.loadDeferredScopedFactsAcrossPartitions(ctx, queryer, params, partitions, instruments)
	if err != nil {
		return nil, nil, err
	}
	if len(loaded) == 0 {
		return nil, snapshotGenerations, nil
	}

	merged, err := s.appendArgoCDGeneratorConfigFacts(ctx, queryer, catalog, loaded)
	if err != nil {
		return nil, nil, err
	}
	return merged, snapshotGenerations, nil
}

// loadDeferredScopedFactsAcrossPartitions fans the per-scope fact load out across
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
func (s IngestionStore) loadDeferredScopedFactsAcrossPartitions(
	ctx context.Context,
	queryer Queryer,
	params deferredScopedFactQueryParams,
	partitions []scopeGenerationPartition,
	instruments *telemetry.Instruments,
) ([]facts.Envelope, error) {
	// Partition memo gate (issue #3624 Track 1 / B'): skip re-loading and
	// re-deriving evidence for a partition whose backward evidence already
	// committed under the CURRENT catalog fingerprint. ArgoCD-bearing partitions
	// are excluded on the WRITE side (they never get a memo row), so they never
	// match here and always reload — the read gate is a single indexed memo
	// lookup with no payload scan (see applyDeferredPartitionMemoGate). A gate
	// error degrades to "load everything" — the legacy full-load behavior —
	// rather than aborting the pass, because the gate is a performance
	// optimization, never a correctness dependency: the fact load it guards is
	// idempotent and safe to re-run.
	loadPartitions := partitions
	if s.db != nil {
		memoStore := newDeferredBackfillPartitionMemoStore(s.db)
		fingerprint := deferredCatalogFingerprint(params)
		gateResult, err := applyDeferredPartitionMemoGate(ctx, memoStore, partitions, fingerprint, instruments)
		if err != nil {
			log.Printf("deferred_backfill_partition_memo_gate_failed error=%q partitions=%d falling_back=true", err, len(partitions))
		} else {
			loadPartitions = gateResult.ToLoad
		}
	}

	tasks := buildDeferredScopedFactLoadTasks(loadPartitions, params)
	if len(tasks) == 0 {
		return nil, nil
	}
	workers := s.maintenanceWorkers
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
			if instruments != nil {
				instruments.DeferredBackfillPartitionLoadDuration.Record(groupCtx, time.Since(started).Seconds())
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
			perTask[index] = envelopes
		}(i, task)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := groupCtx.Err(); err != nil {
		return nil, err
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
	return merged, nil
}

func mergeDeferredScopedTaskFacts(perTask [][]facts.Envelope) []facts.Envelope {
	total := 0
	for _, envelopes := range perTask {
		total += len(envelopes)
	}
	if total == 0 {
		return nil
	}
	seen := make(map[string]struct{}, total)
	merged := make([]facts.Envelope, 0, total)
	for _, envelopes := range perTask {
		for _, envelope := range envelopes {
			if envelope.FactID != "" {
				if _, ok := seen[envelope.FactID]; ok {
					continue
				}
				seen[envelope.FactID] = struct{}{}
			}
			merged = append(merged, envelope)
		}
	}
	return merged
}

func buildDeferredScopedFactLoadTasks(
	partitions []scopeGenerationPartition,
	params deferredScopedFactQueryParams,
) []deferredScopedFactLoadTask {
	tasks := make([]deferredScopedFactLoadTask, 0, len(partitions))
	for _, partition := range partitions {
		repoIDValues := []string(params.repoIDValues)
		if len(repoIDValues) == 0 {
			tasks = append(tasks, deferredScopedFactLoadTask{partition: partition, params: params})
			continue
		}
		for start := 0; start < len(repoIDValues); start += deferredScopedRepoIDChunkSize {
			end := start + deferredScopedRepoIDChunkSize
			if end > len(repoIDValues) {
				end = len(repoIDValues)
			}
			taskParams := deferredScopedFactQueryParams{
				repoIDValues: pq.StringArray(repoIDValues[start:end]),
			}
			if start == 0 {
				taskParams.nonRepoIDLike = params.nonRepoIDLike
			} else {
				taskParams.nonRepoIDLike = pq.StringArray{}
			}
			tasks = append(tasks, deferredScopedFactLoadTask{
				partition: partition,
				params:    taskParams,
			})
		}
	}
	return tasks
}

// appendArgoCDGeneratorConfigFacts runs the deferred backfill's phase-two ArgoCD
// config-repo reload over the merged per-scope facts. An ArgoCD ApplicationSet's
// git generator references its deploy repo only through template parameters that
// are not in the ApplicationSet fact's own payload, so the external config repo's
// files are reloaded by repo_id and merged (de-duplicated by FactID) into the
// loaded facts. The config catalog is the full catalog so a config repo not in
// the anchor set is still resolvable.
func (s IngestionStore) appendArgoCDGeneratorConfigFacts(
	ctx context.Context,
	queryer Queryer,
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
	configFacts, err := loadArgoCDGeneratorConfigFacts(ctx, queryer, configRepoIDs)
	if err != nil {
		return nil, fmt.Errorf("load argocd generator config facts for deferred relationship backfill: %w", err)
	}
	return mergeRelationshipFacts(loaded, configFacts), nil
}
