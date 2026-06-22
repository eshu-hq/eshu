package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// deferredMaintenanceBarrierLockKey is the retired single fleet-wide advisory
// lock key that once serialized all deferred relationship maintenance across the
// fleet. Deferred maintenance now partitions its locks per repository (see
// deferred_maintenance_lock.go); this key is retained only as a regression guard
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
	db                       ExecQueryer
	beginner                 Beginner
	Now                      func() time.Time
	SkipRelationshipBackfill bool
	Logger                   *slog.Logger
	// maintenanceBatchSize overrides the deferred-maintenance per-batch
	// repository count. Zero uses deferredMaintenanceRepoBatchSize. It exists so
	// tests can force multiple independent batch transactions deterministically.
	maintenanceBatchSize int
	catalogCache         *repositoryCatalogCache
}

// NewIngestionStore constructs a transactional storage boundary for projection
// input. It installs a shared repository catalog cache so per-commit catalog
// reloads stay O(1) across the lifetime of the store (issue #3481).
func NewIngestionStore(db ExecQueryer) IngestionStore {
	store := IngestionStore{db: db, catalogCache: newRepositoryCatalogCache()}
	if beginner, ok := db.(Beginner); ok {
		store.beginner = beginner
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
	return s.commitScopeGeneration(ctx, workflow.ClaimMutation{}, false, scopeValue, generation, factStream, nil)
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
	return s.commitScopeGeneration(ctx, mutation, true, scopeValue, generation, factStream, nil)
}

func (s IngestionStore) commitScopeGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	requireClaimFence bool,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
) error {
	if err := validateGenerationInput(scopeValue, generation); err != nil {
		return errors.Join(err, drainFactsAndCheckStream(factStream, factStreamErr))
	}
	skip, err := s.shouldSkipUnchangedGeneration(ctx, scopeValue.ScopeID, generation.FreshnessHint)
	if err != nil {
		return errors.Join(
			fmt.Errorf("check active generation freshness: %w", err),
			drainFactsAndCheckStream(factStream, factStreamErr),
		)
	}
	if skip {
		if err := drainFactsAndCheckStream(factStream, factStreamErr); err != nil {
			return fmt.Errorf("read fact stream: %w", err)
		}
		telemetry.RecordSkippedRefresh()
		log.Printf(
			"%s=true %s=%q %s=%q %s=%q %s=%q %s=%q",
			telemetry.LogKeyRefreshSkipped,
			telemetry.LogKeyScopeID,
			scopeValue.ScopeID,
			telemetry.LogKeyScopeKind,
			string(scopeValue.ScopeKind),
			telemetry.LogKeySourceSystem,
			scopeValue.SourceSystem,
			telemetry.LogKeyCollectorKind,
			string(scopeValue.CollectorKind),
			telemetry.LogKeyGenerationID,
			generation.GenerationID,
		)
		return nil
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

	committed := false
	defer func() {
		if !committed {
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
	if err := acquireDeferredMaintenanceRepoSharedLock(ctx, tx, deferredMaintenanceRepoLockKey(scopeValue)); err != nil {
		return fmt.Errorf("acquire deferred maintenance shared barrier: %w", err)
	}
	s.logCommitStage(ctx, scopeValue, generation, "deferred_maintenance_shared_barrier", stageStart)

	stageStart = time.Now()
	if err := upsertIngestionScope(ctx, tx, scopeValue, generation); err != nil {
		return fmt.Errorf("upsert ingestion scope: %w", err)
	}
	s.logCommitStage(ctx, scopeValue, generation, "upsert_ingestion_scope", stageStart)
	stageStart = time.Now()
	if err := upsertScopeGeneration(ctx, tx, generation); err != nil {
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
	// not just the id — is needed so the shared catalog cache can invalidate when
	// an already-known repo's slug/name aliases drift, not only when a new id
	// appears (issue #3521).
	currentGenerationRepos := make(map[string]relationships.CatalogEntry)
	relationshipStore := NewRelationshipStore(tx)
	stageStart = time.Now()
	factStats, err := upsertStreamingFacts(
		ctx,
		tx,
		factStream,
		scopeValue.ScopeID,
		generation.GenerationID,
		func(batch []facts.Envelope) error {
			for _, envelope := range batch {
				if envelope.FactKind != "repository" {
					continue
				}
				entry, ok := repositoryCatalogEntryFromMap(envelope.Payload)
				if ok {
					currentGenerationRepos[entry.RepoID] = entry
				}
			}
			if !shouldDiscoverStreamingRelationshipEvidence(scopeValue) || len(catalog) == 0 {
				return nil
			}
			evidence := relationships.DiscoverEvidence(batch, catalog)
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
	s.logCommitStage(
		ctx,
		scopeValue,
		generation,
		"upsert_facts",
		stageStart,
		slog.Int("fact_count", factStats.Rows),
		slog.Int("batch_count", factStats.Batches),
	)
	if !s.SkipRelationshipBackfill {
		stageStart = time.Now()
		if err := backfillRelationshipEvidenceForNewRepositories(
			ctx,
			tx,
			relationshipStore,
			generation.GenerationID,
			knownRepoIDs,
			catalogEntryIDSet(currentGenerationRepos),
		); err != nil {
			return err
		}
		s.logCommitStage(ctx, scopeValue, generation, "relationship_backfill", stageStart)
	}

	queue := ProjectorQueue{db: tx, Now: s.now}
	stageStart = time.Now()
	if err := queue.Enqueue(ctx, scopeValue.ScopeID, generation.GenerationID); err != nil {
		return err
	}
	s.logCommitStage(ctx, scopeValue, generation, "enqueue_projector_work", stageStart)

	stageStart = time.Now()
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ingestion transaction: %w", err)
	}
	committed = true
	s.logCommitStage(ctx, scopeValue, generation, "commit_transaction", stageStart)

	// Invalidate the shared catalog only after the generation is durably
	// committed, so a rolled-back transaction never evicts a valid snapshot. A
	// generation that introduces a previously unknown repository id, or that
	// changes a known repository's identity aliases (slug/name), forces the next
	// commit to reload a catalog that reflects the change. Commits over known
	// repositories with unchanged identity leave the cache intact (the common
	// hot-path case).
	if s.invalidateCatalogForChangedRepositories(currentGenerationRepos) {
		s.logCommitStage(
			ctx,
			scopeValue,
			generation,
			"repository_catalog_invalidated",
			stageStart,
			slog.Int("current_generation_repo_count", len(currentGenerationRepos)),
		)
	}

	return nil
}

// repositoryCatalog returns the shared repository identity catalog, loading it
// once through the supplied queryer when the cache is cold. The caller MUST pass
// the open ingestion transaction: a cold load must reuse the transaction's
// connection rather than acquiring a second pool connection while the tx is
// open, which would deadlock under a saturated or single-connection pool
// (ESHU_POSTGRES_MAX_OPEN_CONNS=1). Reading on the transaction is also correct:
// the catalog reflects committed global repository facts plus this
// transaction's own writes, and this generation's repository facts are not yet
// written at load time and are not evidence targets for themselves.
func (s IngestionStore) repositoryCatalog(ctx context.Context, queryer Queryer) (catalogSnapshot, error) {
	return s.catalogCache.get(ctx, queryer)
}

// invalidateCatalogForChangedRepositories evicts the shared catalog when a
// committed generation introduced a repository the cache had not seen or changed
// a known repository's identity aliases. It returns true when an eviction
// occurred.
func (s IngestionStore) invalidateCatalogForChangedRepositories(
	currentGenerationRepos map[string]relationships.CatalogEntry,
) bool {
	return s.catalogCache.invalidateForChangedRepositories(currentGenerationRepos)
}

// catalogEntryIDSet projects a repo-id-to-CatalogEntry map down to the set of
// repository ids. The new-repository relationship backfill keys only on ids,
// while cache invalidation needs the full identity.
func catalogEntryIDSet(entries map[string]relationships.CatalogEntry) map[string]struct{} {
	if len(entries) == 0 {
		return nil
	}
	ids := make(map[string]struct{}, len(entries))
	for repoID := range entries {
		ids[repoID] = struct{}{}
	}
	return ids
}

// catalogLoadCount reports how many fresh repository catalog loads the shared
// cache has performed. It feeds the commit-stage log so operators can confirm
// the hot path is not reloading the catalog per commit.
func (s IngestionStore) catalogLoadCount() int64 {
	return s.catalogCache.loadCount()
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
	logAttrs = append(logAttrs,
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

func upsertIngestionScope(
	ctx context.Context,
	db ExecQueryer,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
) error {
	payloadJSON, err := marshalPayload(stringMapToAny(scopeValue.MetadataCopy()))
	if err != nil {
		return fmt.Errorf("marshal scope payload: %w", err)
	}

	_, err = db.ExecContext(
		ctx,
		upsertIngestionScopeQuery,
		scopeValue.ScopeID,
		string(scopeValue.ScopeKind),
		scopeValue.SourceSystem,
		scopeSourceKey(scopeValue),
		emptyToNil(scopeValue.ParentScopeID),
		string(scopeValue.CollectorKind),
		scopeValue.PartitionKey,
		generation.ObservedAt.UTC(),
		generation.IngestedAt.UTC(),
		string(generation.Status),
		activeGenerationID(generation),
		payloadJSON,
	)
	if err != nil {
		return err
	}

	return nil
}

func upsertScopeGeneration(
	ctx context.Context,
	db ExecQueryer,
	generation scope.ScopeGeneration,
) error {
	_, err := db.ExecContext(
		ctx,
		upsertScopeGenerationQuery,
		generation.GenerationID,
		generation.ScopeID,
		string(generation.TriggerKind),
		emptyToNil(generation.FreshnessHint),
		emptyToNil(generation.SourceCommitSHA),
		generation.IsDelta,
		generation.ObservedAt.UTC(),
		generation.IngestedAt.UTC(),
		string(generation.Status),
		activeTimestamp(generation),
	)
	if err != nil {
		return err
	}

	return nil
}

func shouldDiscoverStreamingRelationshipEvidence(scopeValue scope.IngestionScope) bool {
	return scopeValue.ScopeKind == scope.KindRepository
}

func scopeSourceKey(scopeValue scope.IngestionScope) string {
	if scopeValue.Metadata != nil {
		if sourceKey := strings.TrimSpace(scopeValue.Metadata["source_key"]); sourceKey != "" {
			return sourceKey
		}
	}

	return scopeValue.ScopeID
}

func activeGenerationID(generation scope.ScopeGeneration) any {
	if generation.Status == scope.GenerationStatusActive {
		return generation.GenerationID
	}

	return nil
}

func activeTimestamp(generation scope.ScopeGeneration) any {
	if generation.Status == scope.GenerationStatusActive {
		return generation.IngestedAt.UTC()
	}

	return nil
}

func stringMapToAny(input map[string]string) map[string]any {
	if len(input) == 0 {
		return nil
	}

	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}

	return output
}
