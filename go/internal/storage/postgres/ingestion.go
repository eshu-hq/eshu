package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const listRepositoryCatalogQuery = `
SELECT payload
FROM fact_records
WHERE fact_kind = 'repository'
ORDER BY observed_at DESC, fact_id DESC
`

const listLatestRelationshipFactRecordsQuery = `
WITH latest_generations AS (
    SELECT
        generation.scope_id,
        COALESCE(
            scope.active_generation_id,
            (
                SELECT generation_id
                FROM scope_generations AS candidate
                WHERE candidate.scope_id = generation.scope_id
                ORDER BY candidate.ingested_at DESC, candidate.generation_id DESC
                LIMIT 1
            )
        ) AS generation_id
    FROM scope_generations AS generation
    LEFT JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    GROUP BY generation.scope_id, scope.active_generation_id
)
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN latest_generations AS latest
  ON latest.scope_id = fact.scope_id
 AND latest.generation_id = fact.generation_id
WHERE latest.generation_id IS NOT NULL
  AND fact.fact_kind IN ('content', 'file')
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`

const upsertIngestionScopeQuery = `
INSERT INTO ingestion_scopes (
    scope_id,
    scope_kind,
    source_system,
    source_key,
    parent_scope_id,
    collector_kind,
    partition_key,
    observed_at,
    ingested_at,
    status,
    active_generation_id,
    payload
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb
)
ON CONFLICT (scope_id) DO UPDATE SET
    scope_kind = EXCLUDED.scope_kind,
    source_system = EXCLUDED.source_system,
    source_key = EXCLUDED.source_key,
    parent_scope_id = EXCLUDED.parent_scope_id,
    collector_kind = EXCLUDED.collector_kind,
    partition_key = EXCLUDED.partition_key,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    status = CASE
        WHEN ingestion_scopes.active_generation_id IS NOT NULL
            AND EXCLUDED.active_generation_id IS NULL
            AND EXCLUDED.status = 'pending'
        THEN ingestion_scopes.status
        ELSE EXCLUDED.status
    END,
    active_generation_id = CASE
        WHEN EXCLUDED.active_generation_id IS NOT NULL THEN EXCLUDED.active_generation_id
        ELSE ingestion_scopes.active_generation_id
    END,
    payload = EXCLUDED.payload
`

const upsertScopeGenerationQuery = `
INSERT INTO scope_generations (
    generation_id,
    scope_id,
    trigger_kind,
    freshness_hint,
    observed_at,
    ingested_at,
    status,
    activated_at,
    superseded_at,
    payload
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, NULL, '{}'::jsonb
)
ON CONFLICT (generation_id) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    trigger_kind = EXCLUDED.trigger_kind,
    freshness_hint = EXCLUDED.freshness_hint,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    status = EXCLUDED.status,
    activated_at = EXCLUDED.activated_at,
    payload = EXCLUDED.payload
`

const activeGenerationFreshnessQuery = `
SELECT generation.generation_id, COALESCE(generation.freshness_hint, '')
FROM scope_generations AS generation
WHERE generation.scope_id = $1
  AND generation.status IN ('pending', 'active')
  AND COALESCE(generation.freshness_hint, '') <> ''
ORDER BY generation.ingested_at DESC, generation.generation_id DESC
LIMIT 1
`

const activeRepositoryGenerationsQuery = `
WITH latest_generations AS (
    SELECT
        generation.scope_id,
        COALESCE(
            scope.active_generation_id,
            (
                SELECT generation_id
                FROM scope_generations AS candidate
                WHERE candidate.scope_id = generation.scope_id
                ORDER BY candidate.ingested_at DESC, candidate.generation_id DESC
                LIMIT 1
            )
        ) AS generation_id
    FROM scope_generations AS generation
    LEFT JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    GROUP BY generation.scope_id, scope.active_generation_id
)
SELECT DISTINCT ON (repo_id)
    repo_id,
    fact.scope_id,
    fact.generation_id
FROM (
    SELECT
        COALESCE(
            fact.payload->>'repo_id',
            fact.payload->>'graph_id',
            fact.payload->>'name',
            ''
        ) AS repo_id,
        fact.scope_id,
        fact.generation_id,
        fact.observed_at,
        fact.fact_id
    FROM fact_records AS fact
    JOIN latest_generations AS latest
      ON latest.scope_id = fact.scope_id
     AND latest.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'repository'
) AS fact
WHERE repo_id <> ''
ORDER BY repo_id, observed_at DESC, fact_id DESC
`

const listSucceededDeploymentMappingWorkItemsQuery = `
SELECT work_item_id
FROM fact_work_items
WHERE stage = 'reducer'
  AND domain = 'deployment_mapping'
  AND status = 'succeeded'
ORDER BY updated_at ASC, work_item_id ASC
`

// IngestionStore owns the durable commit boundary for scope generations, facts,
// and projector follow-up work.
type IngestionStore struct {
	db                       ExecQueryer
	beginner                 Beginner
	Now                      func() time.Time
	SkipRelationshipBackfill bool
	Logger                   *slog.Logger
}

// NewIngestionStore constructs a transactional storage boundary for projection
// input.
func NewIngestionStore(db ExecQueryer) IngestionStore {
	store := IngestionStore{db: db}
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
	return s.commitScopeGeneration(ctx, workflow.ClaimMutation{}, false, scopeValue, generation, factStream)
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
	return s.commitScopeGeneration(ctx, mutation, true, scopeValue, generation, factStream)
}

func (s IngestionStore) commitScopeGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	requireClaimFence bool,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	if err := validateGenerationInput(scopeValue, generation); err != nil {
		drainFacts(factStream)
		return err
	}
	skip, err := s.shouldSkipUnchangedGeneration(ctx, scopeValue.ScopeID, generation.FreshnessHint)
	if err != nil {
		drainFacts(factStream)
		return fmt.Errorf("check active generation freshness: %w", err)
	}
	if skip {
		drainFacts(factStream)
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
		drainFacts(factStream)
		return fmt.Errorf("transaction beginner is required")
	}

	stageStart := time.Now()
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		drainFacts(factStream)
		return fmt.Errorf("begin ingestion transaction: %w", err)
	}
	s.logCommitStage(ctx, scopeValue, generation, "begin_transaction", stageStart)

	committed := false
	defer func() {
		if !committed {
			drainFacts(factStream)
			_ = tx.Rollback()
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
	catalog, err := loadRepositoryCatalog(ctx, tx)
	if err != nil {
		return fmt.Errorf("load repository catalog: %w", err)
	}
	s.logCommitStage(ctx, scopeValue, generation, "load_repository_catalog", stageStart, slog.Int("repository_count", len(catalog)))
	knownRepoIDs := catalogRepoIDs(catalog)
	currentGenerationRepoIDs := make(map[string]struct{})
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
				repoID := payloadRepoID(envelope.Payload)
				if repoID != "" {
					currentGenerationRepoIDs[repoID] = struct{}{}
				}
			}
			if len(catalog) == 0 {
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
			currentGenerationRepoIDs,
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

// BackfillAllRelationshipEvidence runs a single corpus-wide backward evidence
// discovery pass and publishes readiness for the active repository generations.
func (s IngestionStore) BackfillAllRelationshipEvidence(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	if s.db == nil {
		return fmt.Errorf("ingestion store db is required")
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
	repoGenerations, err := loadActiveRepositoryGenerations(ctx, s.db)
	if err != nil {
		return fmt.Errorf("load active repository generations for deferred relationship backfill: %w", err)
	}
	if len(repoGenerations) == 0 {
		return nil
	}

	activeFacts, err := loadLatestRelationshipFacts(ctx, s.db)
	if err != nil {
		return fmt.Errorf("load latest facts for deferred relationship backfill: %w", err)
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

	relationshipStore := NewRelationshipStore(s.db)
	for repoID, repoEvidence := range evidenceBySourceRepo {
		repoGeneration, ok := repoGenerations[repoID]
		if !ok {
			log.Printf(
				"relationship_backfill_deferred_source_skipped=true source_repo_id=%q reason=%q",
				repoID,
				"missing_active_generation",
			)
			continue
		}
		if err := relationshipStore.UpsertEvidenceFacts(ctx, repoGeneration.GenerationID, repoEvidence); err != nil {
			return fmt.Errorf("persist deferred relationship evidence for repo %q: %w", repoID, err)
		}
	}

	now := s.now()
	phaseRows := make([]reducer.GraphProjectionPhaseState, 0, len(repoGenerations))
	for _, repoGeneration := range repoGenerations {
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
	if err := NewGraphProjectionPhaseStateStore(s.db).PublishGraphProjectionPhases(ctx, phaseRows); err != nil {
		return fmt.Errorf("publish backward evidence readiness: %w", err)
	}

	dur := time.Since(start).Seconds()
	if instruments != nil {
		instruments.DeferredBackfillDuration.Record(ctx, dur)
		instruments.DeferredBackfillEvidence.Add(ctx, totalEvidence)
	}
	log.Printf("deferred_backfill_completed evidence_facts=%d readiness_rows=%d duration_s=%.2f",
		totalEvidence, len(phaseRows), dur)

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

func loadRepositoryCatalog(ctx context.Context, queryer Queryer) ([]relationships.CatalogEntry, error) {
	if queryer == nil {
		return nil, nil
	}

	rows, err := queryer.QueryContext(ctx, listRepositoryCatalogQuery)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[string]struct{})
	catalog := make([]relationships.CatalogEntry, 0)
	for rows.Next() {
		var rawPayload []byte
		if err := rows.Scan(&rawPayload); err != nil {
			return nil, err
		}
		entry, ok := repositoryCatalogEntryFromPayload(rawPayload)
		if !ok {
			continue
		}
		if _, exists := seen[entry.RepoID]; exists {
			continue
		}
		seen[entry.RepoID] = struct{}{}
		catalog = append(catalog, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return catalog, nil
}

func backfillRelationshipEvidenceForNewRepositories(
	ctx context.Context,
	queryer Queryer,
	relationshipStore *RelationshipStore,
	generationID string,
	knownRepoIDs map[string]struct{},
	currentGenerationRepoIDs map[string]struct{},
) error {
	if relationshipStore == nil || len(currentGenerationRepoIDs) == 0 {
		return nil
	}

	newRepoIDs := make(map[string]struct{})
	for repoID := range currentGenerationRepoIDs {
		if _, exists := knownRepoIDs[repoID]; exists {
			continue
		}
		newRepoIDs[repoID] = struct{}{}
	}
	if len(newRepoIDs) == 0 {
		return nil
	}

	refreshedCatalog, err := loadRepositoryCatalog(ctx, queryer)
	if err != nil {
		return fmt.Errorf("reload repository catalog for relationship backfill: %w", err)
	}
	activeFacts, err := loadLatestRelationshipFacts(ctx, queryer)
	if err != nil {
		return fmt.Errorf("load latest facts for relationship backfill: %w", err)
	}
	evidence := filterEvidenceByTargetRepo(
		relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(activeFacts, refreshedCatalog)),
		newRepoIDs,
	)
	if len(evidence) == 0 {
		return nil
	}
	if err := relationshipStore.UpsertEvidenceFacts(ctx, generationID, evidence); err != nil {
		return fmt.Errorf("persist backfilled relationship evidence: %w", err)
	}

	return nil
}

func loadLatestRelationshipFacts(ctx context.Context, queryer Queryer) ([]facts.Envelope, error) {
	rows, err := queryer.QueryContext(ctx, listLatestRelationshipFactRecordsQuery)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var loaded []facts.Envelope
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return loaded, nil
}

func catalogRepoIDs(catalog []relationships.CatalogEntry) map[string]struct{} {
	repoIDs := make(map[string]struct{}, len(catalog))
	for _, entry := range catalog {
		if strings.TrimSpace(entry.RepoID) == "" {
			continue
		}
		repoIDs[entry.RepoID] = struct{}{}
	}
	return repoIDs
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

func payloadRepoID(payload map[string]any) string {
	return catalogString(payload, "repo_id", "graph_id", "name")
}

func filterEvidenceByTargetRepo(
	evidence []relationships.EvidenceFact,
	targetRepoIDs map[string]struct{},
) []relationships.EvidenceFact {
	if len(evidence) == 0 || len(targetRepoIDs) == 0 {
		return nil
	}

	filtered := make([]relationships.EvidenceFact, 0, len(evidence))
	for _, fact := range evidence {
		if _, ok := targetRepoIDs[fact.TargetRepoID]; !ok {
			continue
		}
		filtered = append(filtered, fact)
	}
	return filtered
}

func repositoryCatalogEntryFromPayload(rawPayload []byte) (relationships.CatalogEntry, bool) {
	if len(rawPayload) == 0 {
		return relationships.CatalogEntry{}, false
	}

	var payload map[string]any
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return relationships.CatalogEntry{}, false
	}

	repoID := catalogString(payload, "repo_id", "graph_id", "name")
	if strings.TrimSpace(repoID) == "" {
		return relationships.CatalogEntry{}, false
	}

	aliases := uniqueCatalogAliases(
		repoID,
		catalogString(payload, "name", "repo_name"),
		catalogString(payload, "repo_slug"),
	)

	return relationships.CatalogEntry{
		RepoID:  repoID,
		Aliases: aliases,
	}, true
}

func catalogString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func uniqueCatalogAliases(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	aliases := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		aliases = append(aliases, value)
	}
	return aliases
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
