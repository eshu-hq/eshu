package postgres

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// loadDeferredAnchorScopedRelationshipFacts is the deferred-backfill replacement
// for loadAnchorScopedRelationshipFacts (issue #3659), partitioned per scope and
// fanned out across the deferred-maintenance worker pool (issue #3710).
//
// It loads the active (scope_id, generation_id) pairs, then issues one
// self-exclusion query (listDeferredScopedRelationshipFactRecordsQuery) per
// partition, bounded by $3/$4. The per-scope bound turns the prior single
// O(facts × catalog) corpus scan — the measured ~20min+ long pole — into many
// index-bounded scans that run concurrently. The $1/$2 catalog parameters are
// built once and shared across partitions.
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
) ([]facts.Envelope, error) {
	if queryer == nil {
		return nil, nil
	}

	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		// No anchor and no repo_id value: no content/file/gcp fact can resolve a
		// catalog target, so skip the fact load entirely.
		return nil, nil
	}

	partitions, err := loadActiveRepositoryGenerations(ctx, queryer)
	if err != nil {
		return nil, fmt.Errorf("load active generations for deferred fact partitions: %w", err)
	}
	if len(partitions) == 0 {
		return nil, nil
	}

	loaded, err := s.loadDeferredScopedFactsAcrossPartitions(ctx, queryer, params, partitions)
	if err != nil {
		return nil, err
	}
	if len(loaded) == 0 {
		return nil, nil
	}

	return s.appendArgoCDGeneratorConfigFacts(ctx, queryer, catalog, loaded)
}

// loadDeferredScopedFactsAcrossPartitions fans the per-scope fact load out across
// a bounded worker pool and returns the merged facts in a deterministic order
// (sorted by (observed_at, fact_id)) so the discovery pass and its tests do not
// depend on goroutine scheduling. The merge is order-independent: each partition
// produces evidence only for its own source facts, so concatenating partition
// results and sorting yields the same fact set a single corpus scan would.
func (s IngestionStore) loadDeferredScopedFactsAcrossPartitions(
	ctx context.Context,
	queryer Queryer,
	params deferredScopedFactQueryParams,
	partitions map[string]repositoryGenerationIdentity,
) ([]facts.Envelope, error) {
	keys := make([]string, 0, len(partitions))
	for repoID := range partitions {
		keys = append(keys, repoID)
	}
	sort.Strings(keys)

	workers := s.maintenanceWorkers
	if workers < 1 {
		workers = 1
	}
	if workers > len(keys) {
		workers = len(keys)
	}

	groupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		firstErr error
	)
	perPartition := make([][]facts.Envelope, len(keys))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for i, repoID := range keys {
		mu.Lock()
		stop := firstErr != nil
		mu.Unlock()
		if stop || groupCtx.Err() != nil {
			break
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(index int, identity repositoryGenerationIdentity) {
			defer wg.Done()
			defer func() { <-sem }()

			envelopes, err := loadDeferredScopedRelationshipFactsForPartition(
				groupCtx, queryer, params, identity.ScopeID, identity.GenerationID,
			)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf(
						"load deferred scoped facts for scope %q: %w", identity.ScopeID, err,
					)
					cancel()
				}
				mu.Unlock()
				return
			}
			perPartition[index] = envelopes
		}(i, partitions[repoID])
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	var merged []facts.Envelope
	for _, envelopes := range perPartition {
		merged = append(merged, envelopes...)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].ObservedAt.Equal(merged[j].ObservedAt) {
			return merged[i].FactID < merged[j].FactID
		}
		return merged[i].ObservedAt.Before(merged[j].ObservedAt)
	})
	return merged, nil
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
