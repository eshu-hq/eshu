package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

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

// RunDeferredRelationshipMaintenance runs the ingester's global relationship
// backfill and deployment-mapping reopen behind an exclusive transaction-level
// Postgres advisory lock. Generation commits take the matching shared lock, so
// this pass waits for in-flight source fact commits and blocks new ones until
// both maintenance phases finish.
func (s IngestionStore) RunDeferredRelationshipMaintenance(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	if s.beginner == nil {
		return fmt.Errorf("transaction beginner is required")
	}

	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin deferred relationship maintenance transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := acquireDeferredMaintenanceExclusiveBarrier(ctx, tx); err != nil {
		return fmt.Errorf("acquire deferred maintenance exclusive barrier: %w", err)
	}

	maintenanceStore := NewIngestionStore(tx)
	maintenanceStore.Now = s.Now
	maintenanceStore.Logger = s.Logger
	if err := maintenanceStore.BackfillAllRelationshipEvidence(ctx, tracer, instruments); err != nil {
		return err
	}
	if err := maintenanceStore.ReopenDeploymentMappingWorkItems(ctx, tracer, instruments); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit deferred relationship maintenance transaction: %w", err)
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
