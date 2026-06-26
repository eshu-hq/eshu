// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// deferredMaintenanceRepoBatchSize bounds how many source repositories one
// deferred-maintenance write transaction locks and commits together. Each batch
// is an independent transaction that takes only its own repositories' exclusive
// locks, so a stalled or slow batch holds at most this many repository locks and
// releases them on commit before the next batch starts. The size trades
// transaction/round-trip overhead (smaller batches => more commits) against lock
// hold time and conflict surface (larger batches => longer holds, more
// repositories blocked at once). 32 keeps per-batch lock hold time small while
// amortizing transaction overhead across the corpus.
const deferredMaintenanceRepoBatchSize = 32

// BackfillAllRelationshipEvidence runs a corpus-wide backward evidence discovery
// pass and publishes readiness for the active repository generations. Evidence
// discovery reads the whole committed fact corpus (cross-repo relationships need
// every repository's facts), but the writes are split into independent,
// per-repository-batch transactions so the pass never holds a fleet-wide lock.
// Each batch transaction acquires only its own repositories' exclusive
// maintenance locks (sorted, deadlock-free), re-reads those repositories' active
// generations under the lock so evidence attaches to the current generation, and
// commits to release the locks before the next batch. A stall on one batch
// therefore blocks only that batch's repositories, never unrelated commits.
func (s IngestionStore) BackfillAllRelationshipEvidence(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	if s.db == nil {
		return fmt.Errorf("ingestion store db is required")
	}
	if s.beginner == nil {
		return fmt.Errorf("transaction beginner is required for batched deferred backfill")
	}

	start := time.Now()
	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "relationship.backfill_deferred")
		defer span.End()
	}

	catalog, err := loadRepositoryCatalog(ctx, s.db)
	if err != nil {
		return fmt.Errorf("load repository catalog for deferred relationship backfill: %w", err)
	}

	// Scope the corpus-wide source-fact load to the content anchors of the full
	// catalog instead of streaming every committed fact (issue #3569), and
	// partition the load per (scope_id, generation_id) so it fans out across the
	// deferred-maintenance worker pool (issue #3710). The deferred backfill treats
	// every repository as an eligible relationship target, so anchors derive from
	// the whole catalog rather than an onboarding delta. The dedicated deferred
	// loader (issue #3659) splits the anchor set into non-repo_id terms ($1) and
	// repo_id terms ($2 self-exclusion) so facts that only match because their own
	// repo_id appears as an anchor are excluded at the SQL layer, while facts
	// referencing ANOTHER repo's repo_id in their content are still loaded. No
	// evidence the full-corpus load would have produced is dropped
	// (truth-equivalence: the in-memory matcher already skips self-matches on
	// entry.RepoID == sourceRepoID and re-applies boundary-safe token matching).
	// With no usable anchors no fact can resolve a catalog target, so the fact
	// load short-circuits and the pass still publishes readiness for the active
	// generations below.
	activeFacts, snapshotGenerations, err := s.loadDeferredAnchorScopedRelationshipFacts(ctx, s.db, catalog, instruments)
	if err != nil {
		return fmt.Errorf("load anchor-scoped facts for deferred relationship backfill: %w", err)
	}

	discoveredEvidence := relationships.DedupeEvidenceFacts(
		relationships.DiscoverEvidence(activeFacts, catalog),
	)

	var totalEvidence int64
	evidenceBySourceRepo := make(map[string][]relationships.EvidenceFact)
	for _, fact := range discoveredEvidence {
		if strings.TrimSpace(fact.SourceRepoID) == "" || strings.TrimSpace(fact.TargetRepoID) == "" {
			continue
		}
		evidenceBySourceRepo[fact.SourceRepoID] = append(evidenceBySourceRepo[fact.SourceRepoID], fact)
		totalEvidence++
	}

	readinessRows, err := s.writeDeferredBackfillInBatches(ctx, evidenceBySourceRepo, snapshotGenerations, instruments)
	if err != nil {
		return err
	}

	dur := time.Since(start).Seconds()
	if instruments != nil {
		instruments.DeferredBackfillDuration.Record(ctx, dur)
		instruments.DeferredBackfillEvidence.Add(ctx, totalEvidence)
	}
	log.Printf("deferred_backfill_completed evidence_facts=%d readiness_rows=%d duration_s=%.2f batch_size=%d",
		totalEvidence, readinessRows, dur, deferredMaintenanceRepoBatchSize)

	return nil
}

// writeDeferredBackfillInBatches commits deferred backward-evidence and the
// matching readiness rows in bounded per-repository batches, each in its own
// transaction holding only that batch's exclusive maintenance locks. It returns
// the number of readiness rows published. Every active repository is published
// as backward-evidence-ready even when it discovered no new evidence, preserving
// the prior corpus-wide readiness contract; repositories whose active generation
// disappears between batches are skipped idempotently.
func (s IngestionStore) writeDeferredBackfillInBatches(
	ctx context.Context,
	evidenceBySourceRepo map[string][]relationships.EvidenceFact,
	snapshotGenerations map[string]string,
	instruments *telemetry.Instruments,
) (int, error) {
	repoGenerations, err := loadActiveRepositoryGenerations(ctx, s.db)
	if err != nil {
		return 0, fmt.Errorf("load active repository generations for deferred relationship backfill: %w", err)
	}
	if len(repoGenerations) == 0 {
		return 0, nil
	}

	repoIDs := make([]string, 0, len(repoGenerations))
	for repoID := range repoGenerations {
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)

	batchSize := s.maintenanceBatchSize
	if batchSize <= 0 {
		batchSize = deferredMaintenanceRepoBatchSize
	}

	// Partition the sorted repository list into disjoint per-batch slices. Because
	// the list is sorted and the slices are contiguous and non-overlapping, no two
	// batches request the same repository advisory lock, so concurrent batches
	// cannot deadlock on lock ordering (each batch also sorts its own keys). Each
	// batch transaction holds exactly one pooled connection for its lifetime and
	// never acquires a second, so running W batches concurrently can at most block
	// on Begin when the pool is smaller than W; it cannot deadlock. At
	// ESHU_POSTGRES_MAX_OPEN_CONNS=1 the pool self-serializes the batches.
	bounds := make([][2]int, 0, (len(repoIDs)+batchSize-1)/batchSize)
	for start := 0; start < len(repoIDs); start += batchSize {
		end := start + batchSize
		if end > len(repoIDs) {
			end = len(repoIDs)
		}
		bounds = append(bounds, [2]int{start, end})
	}

	workers := s.maintenanceWorkers
	if workers < 1 {
		workers = 1
	}
	if workers > len(bounds) {
		workers = len(bounds)
	}

	return s.runDeferredBackfillBatches(ctx, repoIDs, bounds, workers, evidenceBySourceRepo, snapshotGenerations, instruments)
}

// writeDeferredBackfillBatch processes one bounded batch of source repositories
// in its own transaction. It acquires the batch's exclusive maintenance locks in
// sorted order, re-reads the active generations under the lock so evidence and
// readiness attach to the generation current at lock time, persists each
// repository's evidence, publishes its readiness row, and commits to release the
// locks. The batch is idempotent: evidence inserts are content-addressed
// (ON CONFLICT DO NOTHING) and readiness upserts are keyed by generation.
func (s IngestionStore) writeDeferredBackfillBatch(
	ctx context.Context,
	batchRepoIDs []string,
	evidenceBySourceRepo map[string][]relationships.EvidenceFact,
	snapshotGenerations map[string]string,
) (int, error) {
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin deferred backfill batch transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	lockKeys := make([]string, 0, len(batchRepoIDs))
	for _, repoID := range batchRepoIDs {
		lockKeys = append(lockKeys, deferredMaintenanceRepoLockKeyFromID(repoID))
	}
	if err := acquireDeferredMaintenanceRepoExclusiveLocks(ctx, tx, lockKeys); err != nil {
		return 0, fmt.Errorf("acquire deferred backfill batch locks: %w", err)
	}

	currentGenerations, err := loadActiveRepositoryGenerations(ctx, tx)
	if err != nil {
		return 0, fmt.Errorf("reload active repository generations under batch lock: %w", err)
	}

	relationshipStore := NewRelationshipStore(tx)
	phaseRows := make([]reducer.GraphProjectionPhaseState, 0, len(batchRepoIDs))
	now := s.now()
	for _, repoID := range batchRepoIDs {
		repoGeneration, ok := currentGenerations[repoID]
		if !ok {
			log.Printf(
				"relationship_backfill_deferred_source_skipped=true source_repo_id=%q reason=%q",
				repoID,
				"missing_active_generation",
			)
			continue
		}
		// Generation-consistency guard (issue #3725): when a fact load ran
		// (snapshotGenerations is non-nil), skip any repository whose active
		// generation under the batch lock differs from the generation the fact
		// load queried for that scope — or whose scope was absent from the
		// snapshot. Publishing readiness here would mark the new generation
		// backward-evidence-committed even though its facts were never loaded
		// (the per-scope query rejected the stale generation), reopening
		// deployment mapping with missing relationship evidence. The next periodic
		// RunDeferredRelationshipMaintenance pass re-snapshots and processes the
		// advanced generation. A nil snapshot (no anchors/no partitions) disables
		// the guard so the legacy publish-for-every-active-repository contract
		// holds when no fact load occurred.
		if snapshotGenerations != nil {
			snapshotGen, inSnapshot := snapshotGenerations[repoGeneration.ScopeID]
			if !inSnapshot || snapshotGen != repoGeneration.GenerationID {
				log.Printf(
					"relationship_backfill_deferred_source_skipped=true source_repo_id=%q reason=%q",
					repoID,
					"generation_advanced_since_snapshot",
				)
				continue
			}
		}
		if repoEvidence := evidenceBySourceRepo[repoID]; len(repoEvidence) > 0 {
			if err := relationshipStore.UpsertEvidenceFacts(ctx, repoGeneration.GenerationID, repoEvidence); err != nil {
				return 0, fmt.Errorf("persist deferred relationship evidence for repo %q: %w", repoID, err)
			}
		}
		phaseRows = append(phaseRows, reducer.GraphProjectionPhaseState{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          repoGeneration.ScopeID,
				AcceptanceUnitID: repoGeneration.ScopeID,
				SourceRunID:      repoGeneration.GenerationID,
				GenerationID:     repoGeneration.GenerationID,
				Keyspace:         reducer.GraphProjectionKeyspaceCrossRepoEvidence,
			},
			Phase:       reducer.GraphProjectionPhaseBackwardEvidenceCommitted,
			CommittedAt: now,
			UpdatedAt:   now,
		})
	}
	if err := NewGraphProjectionPhaseStateStore(tx).PublishGraphProjectionPhases(ctx, phaseRows); err != nil {
		return 0, fmt.Errorf("publish backward evidence readiness: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit deferred backfill batch transaction: %w", err)
	}
	committed = true
	return len(phaseRows), nil
}

// RunDeferredRelationshipMaintenance runs the ingester's relationship backfill
// and deployment-mapping reopen. The backfill commits in bounded
// per-repository-batch transactions that each hold only their own repositories'
// exclusive maintenance locks, and the reopen runs in its own transaction. No
// step holds a fleet-wide lock, so a stall on one repository batch blocks only
// that batch's repositories; generation commits take the matching per-repository
// shared lock and wait only for maintenance touching their own repository.
func (s IngestionStore) RunDeferredRelationshipMaintenance(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	if s.beginner == nil {
		return fmt.Errorf("transaction beginner is required")
	}
	if err := s.BackfillAllRelationshipEvidence(ctx, tracer, instruments); err != nil {
		return err
	}
	// One reopen transaction replays both deployment_mapping and
	// code_import_repo_edge work items — they share the same after-the-fact
	// dependency (cross-scope evidence committed by the backfill above).
	return s.reopenDeploymentMappingWorkItemsInTransaction(ctx, tracer, instruments)
}

// reopenDeploymentMappingWorkItemsInTransaction runs the corpus-wide
// deployment-mapping reopen in its own transaction. Reopen is not partitioned by
// repository, so it takes no per-repository maintenance lock; it commits
// independently of the per-batch evidence writes. Reopen is idempotent, so a
// re-run after partial maintenance failure converges to the same queue state.
func (s IngestionStore) reopenDeploymentMappingWorkItemsInTransaction(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin deployment mapping reopen transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	reopenStore := NewIngestionStore(tx)
	reopenStore.Now = s.Now
	reopenStore.Logger = s.Logger
	if err := reopenStore.ReopenDeploymentMappingWorkItems(ctx, tracer, instruments); err != nil {
		return err
	}
	// Replay code_import_repo_edge in the same transaction: it shares the
	// after-the-fact dependency on cross-scope evidence and must re-run once that
	// evidence is committed, just like deployment_mapping.
	if err := reopenStore.ReopenCodeImportRepoEdgeWorkItems(ctx, tracer, instruments); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit deployment mapping reopen transaction: %w", err)
	}
	committed = true
	return nil
}

// ReopenDeploymentMappingWorkItems replays succeeded deployment_mapping work
// items after deferred backward evidence is committed.
func (s IngestionStore) ReopenDeploymentMappingWorkItems(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	if s.db == nil {
		return fmt.Errorf("ingestion store db is required")
	}

	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "bootstrap.reopen_deployment_mapping")
		defer span.End()
	}

	workItemIDs, err := listSucceededDeploymentMappingWorkItemIDs(ctx, s.db)
	if err != nil {
		return err
	}
	queue := ReducerQueue{db: s.db, Now: s.Now}
	for _, workItemID := range workItemIDs {
		if _, err := queue.ReopenSucceeded(ctx, workItemID); err != nil {
			return fmt.Errorf("reopen deployment_mapping work items: %w", err)
		}
	}

	if instruments != nil {
		instruments.DeploymentMappingReopened.Add(ctx, int64(len(workItemIDs)))
	}
	log.Printf("deployment_mapping_reopened count=%d", len(workItemIDs))

	return nil
}

func (s IngestionStore) shouldSkipUnchangedGeneration(
	ctx context.Context,
	scopeID string,
	freshnessHint string,
) (bool, error) {
	if s.db == nil {
		return false, nil
	}
	if strings.TrimSpace(scopeID) == "" || strings.TrimSpace(freshnessHint) == "" {
		return false, nil
	}

	rows, err := s.db.QueryContext(ctx, activeGenerationFreshnessQuery, scopeID)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, err
		}
		return false, nil
	}

	var generationID string
	var activeFreshnessHint string
	if err := rows.Scan(&generationID, &activeFreshnessHint); err != nil {
		return false, err
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	return strings.TrimSpace(activeFreshnessHint) == strings.TrimSpace(freshnessHint), nil
}

// validateGenerationInput checks scope/generation preconditions before
// opening a transaction. Per-fact validation (scope_id, generation_id match)
// happens inside upsertStreamingFacts as facts arrive from the channel.
func validateGenerationInput(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
) error {
	if err := generation.ValidateForScope(scopeValue); err != nil {
		return err
	}
	if generation.IsTerminal() {
		return fmt.Errorf("generation %q must not be terminal before projection", generation.GenerationID)
	}

	return nil
}

type repositoryGenerationIdentity struct {
	RepoID       string
	ScopeID      string
	GenerationID string
}

func loadActiveRepositoryGenerations(
	ctx context.Context,
	queryer Queryer,
) (map[string]repositoryGenerationIdentity, error) {
	if queryer == nil {
		return nil, nil
	}

	rows, err := queryer.QueryContext(ctx, activeRepositoryGenerationsQuery)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]repositoryGenerationIdentity)
	for rows.Next() {
		var identity repositoryGenerationIdentity
		if err := rows.Scan(&identity.RepoID, &identity.ScopeID, &identity.GenerationID); err != nil {
			return nil, err
		}
		if strings.TrimSpace(identity.RepoID) == "" {
			continue
		}
		result[identity.RepoID] = identity
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func listSucceededDeploymentMappingWorkItemIDs(
	ctx context.Context,
	queryer Queryer,
) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, listSucceededDeploymentMappingWorkItemsQuery)
	if err != nil {
		return nil, fmt.Errorf("list succeeded deployment_mapping work items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	workItemIDs := make([]string, 0)
	for rows.Next() {
		var workItemID string
		if err := rows.Scan(&workItemID); err != nil {
			return nil, fmt.Errorf("scan succeeded deployment_mapping work item: %w", err)
		}
		if strings.TrimSpace(workItemID) == "" {
			continue
		}
		workItemIDs = append(workItemIDs, workItemID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list succeeded deployment_mapping work items: %w", err)
	}
	return workItemIDs, nil
}
