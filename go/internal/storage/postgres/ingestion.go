// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// deferredMaintenanceBarrierLockKey is the retired single fleet-wide advisory
// lock key that once serialized all deferred relationship maintenance across the
// fleet. Deferred maintenance now partitions its locks per repository (see
// lock/deferred_maintenance.go); this key is retained only as a regression guard
// so tests can assert the global serialization point is not reintroduced.
const deferredMaintenanceBarrierLockKey int64 = 0x45534855444d42

// IngestionStore owns the durable commit boundary for scope generations, facts,
// and projector follow-up work.
//
// catalogCache is a pointer so that value copies of the store (the commit
// methods use value receivers, and the store is shared as an interface value
// across concurrent collector goroutines) all observe the same shared
// repository catalog cache. It is nil only for stores constructed without
// NewIngestionStore, in which case the catalog falls back to a per-commit load.
type IngestionStore struct {
	database    db.ExecQueryer
	beginner    db.Beginner
	Now         func() time.Time
	Logger      *slog.Logger
	Instruments *telemetry.Instruments
	// SkipRelationshipBackfill disables the per-commit new-repository
	// relationship backfill. The ingester and bootstrap-index runtimes set this
	// true because they run the corpus-wide deferred relationship backfill as a
	// separate batch phase, so per-commit backfill there would be duplicate work
	// (issue #4451, § T8; part of the bootstrap phase contract — see the package
	// doc comment). Callers that leave it false get the post-commit backfill.
	SkipRelationshipBackfill bool
	// maintenanceBatchSize overrides the deferred-maintenance per-batch
	// repository count. Zero uses backfill.DeferredMaintenanceRepoBatchSize.
	// It exists so tests can force multiple independent batch transactions
	// deterministically.
	maintenanceBatchSize int
	// maintenanceWorkers overrides the number of deferred-maintenance batch
	// transactions processed concurrently. Zero or one keeps the pass serial
	// (one in-flight batch transaction), the path for deployments pinned to
	// ESHU_POSTGRES_MAX_OPEN_CONNS=1 via ESHU_DEFERRED_BACKFILL_CONCURRENCY=1.
	// NewIngestionStore seeds it from backfill.DeferredBackfillWorkerCount;
	// tests set it directly.
	maintenanceWorkers int
	catalogCache       *repositoryCatalogCache
	// idleMaintenanceLogGate rate-limits the deferred-maintenance barrier's
	// "idle; no epoch to join" INFO log (see
	// RunDeferredRelationshipMaintenanceAfterShardDrain in
	// deferred_maintenance_barrier.go) so an empty shard connected to
	// Postgres does not emit one line per PollInterval forever. It is a
	// pointer for the same reason as catalogCache: value copies of the store
	// share one gate. A nil gate (a store built without NewIngestionStore)
	// always logs, preserving the unthrottled default.
	idleMaintenanceLogGate *deferredMaintenanceIdleLogGate
}

// NewIngestionStore constructs a transactional storage boundary for projection
// input. It installs a shared repository catalog cache so per-commit catalog
// reloads stay O(1) across the lifetime of the store (issue #3481).
func NewIngestionStore(database db.ExecQueryer) IngestionStore {
	return NewIngestionStoreWithMaintenanceConfig(database, 0, 0)
}

// NewIngestionStoreWithMaintenanceConfig constructs the same ingestion store
// with optional positive overrides for repositories per deferred-maintenance
// batch and concurrent batch workers. Zero or negative values use the same
// defaults as NewIngestionStore. The database handle supplies db.Beginner when
// it implements that interface; the configured pass keeps that exact handle.
func NewIngestionStoreWithMaintenanceConfig(database db.ExecQueryer, batchSize, workers int) IngestionStore {
	store := IngestionStore{
		database:               database,
		catalogCache:           newRepositoryCatalogCache(),
		maintenanceWorkers:     backfill.DeferredBackfillWorkerCount(),
		idleMaintenanceLogGate: &deferredMaintenanceIdleLogGate{},
	}
	if beginner, ok := database.(db.Beginner); ok {
		store.beginner = beginner
	}
	if batchSize > 0 {
		store.maintenanceBatchSize = batchSize
	}
	if workers > 0 {
		store.maintenanceWorkers = workers
	}

	return store
}

// drainFacts reads and discards all remaining facts from the channel.
// This prevents the producer goroutine from leaking when the consumer
// must abort early (skip, validation error, rollback).
func drainFacts(factStream <-chan facts.Envelope) {
	if factStream == nil {
		return
	}
	for range factStream {
	}
}

func drainFactsAndCheckStream(
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
) error {
	drainFacts(factStream)
	if factStreamErr == nil {
		return nil
	}
	return factStreamErr()
}

// CommitScopeGeneration persists one scope generation worth of facts and
// enqueues one projector work item for the same durable boundary. Facts
// arrive through a channel and are committed in batched multi-row INSERTs
// so memory stays proportional to the batch size, not the total fact count.
func (s IngestionStore) CommitScopeGeneration(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	return s.commitScopeGeneration(ctx, workflow.ClaimMutation{}, false, scopeValue, generation, factStream, nil, nil)
}

// CommitClaimedScopeGeneration persists one claimed generation only while the
// workflow claim fence is still current. The claim row is locked in the same
// transaction as fact persistence so stale workers cannot write after another
// owner reclaims the item.
func (s IngestionStore) CommitClaimedScopeGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	if err := validateClaimMutation(mutation); err != nil {
		drainFacts(factStream)
		return err
	}
	return s.commitScopeGeneration(ctx, mutation, true, scopeValue, generation, factStream, nil, nil)
}

func (s IngestionStore) commitScopeGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	requireClaimFence bool,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
	stateWriter collector.GenerationStateWriter,
) error {
	if err := backfill.ValidateGenerationInput(scopeValue, generation); err != nil {
		return errors.Join(err, drainFactsAndCheckStream(factStream, factStreamErr))
	}
	handled, prior, err := s.skipUnchangedGeneration(ctx, mutation, requireClaimFence, scopeValue, generation, factStream, factStreamErr)
	if handled || err != nil {
		return err
	}
	if s.beginner == nil {
		return errors.Join(
			fmt.Errorf("transaction beginner is required"),
			drainFactsAndCheckStream(factStream, factStreamErr),
		)
	}

	stageStart := time.Now()
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return errors.Join(
			fmt.Errorf("begin ingestion transaction: %w", err),
			drainFactsAndCheckStream(factStream, factStreamErr),
		)
	}
	s.logCommitStage(ctx, scopeValue, generation, "begin_transaction", stageStart)

	transactionClosed := false
	defer func() {
		if !transactionClosed {
			_ = tx.Rollback()
			drainFacts(factStream)
		}
	}()

	if requireClaimFence {
		mutation.ObservedAt = s.now()
		controlStore := NewWorkflowControlStore(tx)
		if err := controlStore.HeartbeatClaim(ctx, mutation); err != nil {
			return fmt.Errorf("verify active workflow claim before ingestion commit: %w", err)
		}
	}

	stageStart = time.Now()
	if err := lockstore.AcquireDeferredMaintenanceRepoSharedLock(ctx, tx, lockstore.DeferredMaintenanceRepoLockKey(scopeValue)); err != nil {
		return fmt.Errorf("acquire deferred maintenance shared barrier: %w", err)
	}
	if _, err := tx.ExecContext(ctx, setPackageManifestConsumptionKeysWriterSQL); err != nil {
		return fmt.Errorf("mark package manifest consumption key writer: %w", err)
	}
	// Held from here until tx.Commit() releases it (recordSharedLockHoldDuration
	// below measures that window; issue #4451, § T8).
	sharedLockAcquiredAt := time.Now()
	s.logCommitStage(ctx, scopeValue, generation, "deferred_maintenance_shared_barrier", stageStart)

	stageStart = time.Now()
	gateOutcome, retirementID, err := upsertIngestionScope(ctx, tx, scopeValue, generation)
	if err != nil {
		return fmt.Errorf("upsert ingestion scope: %w", err)
	}
	s.logCommitStage(ctx, scopeValue, generation, "upsert_ingestion_scope", stageStart)
	admitted, err := s.applyScopeGateOutcome(ctx, tx, scopeValue, generation, gateOutcome, retirementID, factStream, factStreamErr, &transactionClosed)
	if err != nil {
		return err
	}
	if !admitted {
		return nil
	}
	// The scope row is locked: re-size the guard prior if the scope changed
	// since the pre-tx sizing (#8750 window; see recheckSuccessorPrior).
	if prior, err = s.recheckSuccessorPrior(ctx, tx, scopeValue, generation, prior); err != nil {
		return err
	}
	stageStart = time.Now()
	if err := upsertScopeGeneration(ctx, tx, generation); err != nil {
		if errors.Is(err, errGenerationAlreadyFinalized) {
			// The scope lock and generation conflict check are in this
			// transaction. Roll back the scope upsert before draining the
			// stream; no facts or projector work from a stale retry can land.
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				return fmt.Errorf("rollback finalized generation replay: %w", rollbackErr)
			}
			transactionClosed = true
			if streamErr := drainFactsAndCheckStream(factStream, factStreamErr); streamErr != nil {
				return fmt.Errorf("read skipped finalized generation stream: %w", streamErr)
			}
			s.backfillStore().RecordSkippedRefresh(ctx, scopeValue, generation, generation.GenerationID, telemetry.RefreshSkipReasonFinalizedGenerationReplay)
			s.logCommitStage(ctx, scopeValue, generation, "skip_finalized_generation", stageStart)
			return nil
		}
		return fmt.Errorf("upsert scope generation: %w", err)
	}
	s.logCommitStage(ctx, scopeValue, generation, "upsert_scope_generation", stageStart)
	stageStart = time.Now()
	catalogState, err := s.repositoryCatalog(ctx, tx)
	if err != nil {
		return fmt.Errorf("load repository catalog: %w", err)
	}
	catalog := catalogState.Entries
	knownRepoIDs := catalogState.RepoIDs
	s.logCommitStage(
		ctx,
		scopeValue,
		generation,
		"load_repository_catalog",
		stageStart,
		slog.Int("repository_count", len(catalog)),
		slog.Bool("catalog_cache_hit", catalogState.CacheHit),
		slog.Int64("catalog_loads_total", s.catalogLoadCount()),
	)
	// currentGenerationRepos maps each repository id this generation commits to
	// its computed catalog identity (RepoID plus aliases). The full identity —
	// not just the id — is needed so the shared catalog cache can detect when
	// an already-known repo's slug/name aliases drift, not only when a new id
	// appears (issue #3521). currentGenerationRepoObservedAt carries each
	// identity's observation time: within a generation the newest observed_at
	// wins (matching reload's ORDER BY observed_at DESC dedup), and the cache
	// uses it as the freshness key so a replayed older generation cannot
	// regress a newer cached identity (#5134 review).
	currentGenerationRepos := make(map[string]relationships.CatalogEntry)
	currentGenerationRepoObservedAt := make(map[string]time.Time)
	relationshipStore := NewRelationshipStore(tx)
	successorObserver := newSuccessorObserver(generation)
	stageStart = time.Now()
	factStats, err := upsertStreamingFacts(
		ctx,
		tx,
		factStream,
		scopeValue.ScopeID,
		generation.GenerationID,
		func(batch []facts.Envelope) error {
			if err := refreshRelationshipReferenceCandidateKeys(ctx, tx, batch); err != nil {
				return err
			}
			if err := refreshPackageManifestConsumptionKeys(ctx, tx, batch); err != nil {
				return err
			}
			if err := refreshPackageRegistryIdentityKeys(ctx, tx, batch); err != nil {
				return err
			}
			for _, envelope := range batch {
				successorObserver.observe(envelope)
				if envelope.FactKind != "repository" {
					continue
				}
				entry, ok := backfill.RepositoryCatalogEntryFromMap(envelope.Payload)
				if ok {
					previousObservedAt, seen := currentGenerationRepoObservedAt[entry.RepoID]
					if !seen || !envelope.ObservedAt.Before(previousObservedAt) {
						currentGenerationRepos[entry.RepoID] = entry
						currentGenerationRepoObservedAt[entry.RepoID] = envelope.ObservedAt
					}
				}
			}
			if !shouldDiscoverStreamingRelationshipEvidence(scopeValue) || len(catalog) == 0 {
				return nil
			}
			evidence, discoveryStats := relationships.DiscoverEvidenceWithStats(batch, catalog)
			recordFluxCrossRepoURLResolutionStats(ctx, s.Instruments, discoveryStats.FluxCrossRepoURLResolution)
			recordArgoCDApplicationSetTemplateSourceStats(ctx, s.Instruments, discoveryStats.ApplicationSetTemplateSource)
			if len(evidence) == 0 {
				return nil
			}
			log.Printf(
				"%s=%q %s=%q evidence_facts_discovered=%d",
				telemetry.LogKeyScopeID,
				scopeValue.ScopeID,
				telemetry.LogKeyGenerationID,
				generation.GenerationID,
				len(evidence),
			)
			if s.Instruments != nil && s.Instruments.EvidenceFactsDiscovered != nil {
				s.Instruments.EvidenceFactsDiscovered.Add(ctx, int64(len(evidence)))
			}
			if err := relationshipStore.UpsertEvidenceFacts(ctx, generation.GenerationID, evidence); err != nil {
				return fmt.Errorf("persist relationship evidence: %w", err)
			}
			return nil
		},
	)
	if err != nil {
		return err
	}
	if factStreamErr != nil {
		if err := factStreamErr(); err != nil {
			return fmt.Errorf("read fact stream: %w", err)
		}
	}
	if streamDropped(generation) {
		markStart := time.Now()
		if err := recordDroppedPreparseIdentity(ctx, tx, generation); err != nil {
			return err
		}
		s.logCommitStage(ctx, scopeValue, generation, "mark_stream_dropped", markStart, slog.Bool("stream_dropped", true))
	}
	s.logCommitStage(
		ctx,
		scopeValue,
		generation,
		"upsert_facts",
		stageStart,
		slog.Int("fact_count", factStats.Rows),
		slog.Int("batch_count", factStats.Batches),
	)
	// The #7926 successor-emptiness backstop; see refuseDestructiveSuccessor.
	refused, err := s.refuseDestructiveSuccessor(ctx, tx, scopeValue, generation, prior, factStats.Rows, successorObserver)
	if err != nil {
		return err
	}
	if refused {
		transactionClosed = true
		return nil
	}
	// Collector-owned state commits with its generation or not at all (#8943).
	if stateWriter != nil {
		stageStart = time.Now()
		if err := writeGenerationState(ctx, tx, stateWriter); err != nil {
			return err
		}
		s.logCommitStage(ctx, scopeValue, generation, "write_generation_state", stageStart)
	}
	queue := ProjectorQueue{database: tx, Now: s.now}
	stageStart = time.Now()
	if err := queue.Enqueue(ctx, scopeValue.ScopeID, generation.GenerationID); err != nil {
		return err
	}
	s.logCommitStage(ctx, scopeValue, generation, "enqueue_projector_work", stageStart)

	stageStart = time.Now()
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ingestion transaction: %w", err)
	}
	transactionClosed = true
	s.logCommitStage(ctx, scopeValue, generation, "commit_transaction", stageStart)
	s.recordSharedLockHoldDuration(ctx, time.Since(sharedLockAcquiredAt))

	// Merge the committed repository identities into the shared catalog only
	// after the generation is durably committed, so a rolled-back transaction
	// never alters a valid snapshot. A generation that introduces a previously
	// unknown repository id, or that changes a known repository's identity
	// aliases (slug/name), is merged in place so the next commit observes the
	// change without reloading the whole catalog (#5129 — the pre-merge
	// eviction forced a full serialized reload per onboarding commit).
	// Commits over known repositories with unchanged identity leave the cache
	// untouched (the steady-state hot path).
	if s.mergeCatalogForChangedRepositories(currentGenerationRepos, currentGenerationRepoObservedAt) {
		s.logCommitStage(
			ctx,
			scopeValue,
			generation,
			"repository_catalog_merged",
			stageStart,
			slog.Int("current_generation_repo_count", len(currentGenerationRepos)),
		)
	}

	// Relationship backfill for any newly onboarded repository runs AFTER the
	// barrier above is released, in its own short transaction (issue #4451,
	// § T8; see runPostCommitRelationshipBackfill for the lock-split rationale
	// and why its errors are logged rather than returned here). Skipped for
	// runtimes that run the corpus-wide deferred backfill separately
	// (ingester, bootstrap-index) so per-commit backfill is not duplicated.
	if !s.SkipRelationshipBackfill {
		s.runPostCommitRelationshipBackfill(ctx, scopeValue, generation, knownRepoIDs, currentGenerationRepos)
	}

	return nil
}

// logCommitStage emits one low-cardinality timing record for the durable
// ingestion transaction. These records intentionally sit at transaction
// boundaries so dogfood runs can distinguish slow Postgres inserts from queue
// enqueue, relationship evidence, or commit latency.
func (s IngestionStore) logCommitStage(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	stage string,
	start time.Time,
	attrs ...any,
) {
	if s.Logger == nil {
		return
	}

	scopeAttrs := telemetry.ScopeAttrs(scopeValue.ScopeID, generation.GenerationID, scopeValue.SourceSystem)
	logAttrs := make([]any, 0, len(scopeAttrs)+len(attrs)+3)
	for _, attr := range scopeAttrs {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(
		logAttrs,
		slog.String("stage", stage),
		slog.Float64("duration_seconds", time.Since(start).Seconds()),
		telemetry.PhaseAttr(telemetry.PhaseEmission),
	)
	logAttrs = append(logAttrs, attrs...)

	s.Logger.InfoContext(ctx, "ingestion commit stage completed", logAttrs...)
}

func (s IngestionStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}

	return time.Now().UTC()
}
