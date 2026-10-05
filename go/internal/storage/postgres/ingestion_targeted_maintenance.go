// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// errTargetedMaintenanceCatalogChanged reports that the repository catalog
// fingerprint differs from the one an active partition's backward evidence was
// last committed under. A catalog change can alter evidence between
// repositories the owed partitions never touch (an alias collision resolves
// differently), so a partition-scoped pass cannot bound what it changes and
// refuses instead of committing a partial result. The caller decides how the
// corpus-wide pass runs.
var errTargetedMaintenanceCatalogChanged = errors.New(
	"repository catalog changed since active backward evidence was committed; partition-scoped maintenance refused")

// staleCatalogMemoQuery reports whether any active partition carries a memo
// row written under a different catalog fingerprint.
const staleCatalogMemoQuery = latestGenerationCTE + `
SELECT EXISTS (
    SELECT 1
    FROM deferred_backfill_partition_memo AS memo
    JOIN latest_generations AS latest
      ON latest.scope_id = memo.scope_id
     AND latest.generation_id = memo.generation_id
    WHERE memo.catalog_fingerprint <> $1
)
`

// targetedMaintenanceResult reports what one partition-scoped pass did.
type targetedMaintenanceResult struct {
	// NotActive are requested partitions whose scope had moved to another
	// generation; nothing is published or reopened for them.
	NotActive []scopeGenerationPartition
	// Promoted are dependent partitions processed as owed because they had no
	// committed backward_evidence phase.
	Promoted []scopeGenerationPartition
	// Loaded are the partitions handed to the memo-gated fact loader.
	Loaded []scopeGenerationPartition
	// Affected are the partitions whose evidence was written, whose phase was
	// published, and whose dependents were reopened.
	Affected []scopeGenerationPartition
	// Skipped is the same-pass memo-hit set used to gate the relationship
	// reopens; nil when the memo gate did not run.
	Skipped map[scopeGenerationPartition]struct{}
	// EvidenceFacts counts the evidence facts handed to the batch writer.
	EvidenceFacts int
	// Published counts the phase rows the fan-in published.
	Published int
	// Reopened counts reopened work items per reducer domain.
	Reopened map[string]int
}

// runDeferredRelationshipMaintenanceForPartitions runs deferred relationship
// maintenance for the owed (scope_id, generation_id) partitions only. It is
// composed from the whole pass's own pieces so that, for every partition it
// touches, the committed relationship_evidence_facts rows, the
// graph_projection_phase_state and memo rows, and the reopened fact_work_items
// equal what RunDeferredRelationshipMaintenance produces for that partition:
//
//   - fact load: loadDeferredScopedFactsAcrossPartitions (the memo-gated
//     per-partition loader) over the closure partitions, then
//     appendArgoCDGeneratorConfigFacts against the full catalog;
//   - affected set: resolveTargetedMaintenanceClosure, which adds inbound and
//     cross-scope sources found by loadAnchorScopedRelationshipFacts;
//   - writes: runDeferredBackfillBatchesWith with the repo-bounded under-lock
//     generation read derived from the shipped query, then the unchanged
//     publishDeferredBackfillPartitions fan-in (phase only after every batch
//     of the pass committed, fenced on the scope's active generation);
//   - reopen: reopenTargetedMaintenanceWorkItems over the exact affected
//     partitions, the relationship domains gated by this pass's memo-hit set
//     and the correlation domains not gated by it.
//
// Partitions outside the affected set are not read for writing, published or
// reopened. It returns errTargetedMaintenanceCatalogChanged when the catalog
// moved since the active evidence was committed.
func (s IngestionStore) runDeferredRelationshipMaintenanceForPartitions(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	owed []scopeGenerationPartition,
) (targetedMaintenanceResult, error) {
	var result targetedMaintenanceResult
	if s.database == nil {
		return result, fmt.Errorf("ingestion store db is required")
	}
	if s.beginner == nil {
		return result, fmt.Errorf("transaction beginner is required for partition-scoped maintenance")
	}
	requested := normalizeOwedPartitions(owed)
	if len(requested) == 0 {
		return result, fmt.Errorf("partition-scoped maintenance requires at least one owed partition")
	}

	start := time.Now()
	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "relationship.backfill_deferred_targeted")
		defer span.End()
	}

	catalog, _, err := loadRepositoryCatalog(ctx, s.database)
	if err != nil {
		return result, fmt.Errorf("load repository catalog for partition-scoped maintenance: %w", err)
	}
	params, hasAnchors := buildDeferredScopedFactQueryParams(catalog)
	catalogFingerprint := deferredCatalogFingerprint(params)
	if err := s.refuseStaleCatalog(ctx, catalogFingerprint); err != nil {
		return result, err
	}

	closure, err := s.resolveTargetedMaintenanceClosure(ctx, catalog, params, hasAnchors, requested, instruments)
	if err != nil {
		return result, err
	}
	result.NotActive = closure.notActive
	result.Promoted = closure.promoted
	result.Loaded = sortedPartitions(closure.load)
	affected := closure.affectedPartitions()
	result.Affected = sortedPartitions(affected)

	// The load and the under-lock guard read the same snapshot shape as the
	// whole pass: scope -> generation for every partition the pass loads or
	// writes. A nil snapshot (no anchors) disables the guard exactly as the
	// whole pass's no-anchor contract does.
	var snapshot map[string]string
	var loaded []relationships.EvidenceFact
	skipped := map[scopeGenerationPartition]struct{}{}
	if hasAnchors {
		snapshot = make(map[string]string, len(closure.load)+len(affected))
		for partition := range closure.load {
			snapshot[partition.ScopeID] = partition.GenerationID
		}
		for partition := range affected {
			snapshot[partition.ScopeID] = partition.GenerationID
		}
		facts, loadSkipped, err := s.loadDeferredScopedFactsAcrossPartitions(
			ctx, s.database, params, result.Loaded, instruments)
		if err != nil {
			return result, fmt.Errorf("load partition-scoped deferred facts: %w", err)
		}
		if len(facts) > 0 {
			facts, err = s.appendArgoCDGeneratorConfigFacts(ctx, s.database, catalog, facts)
			if err != nil {
				return result, err
			}
		}
		loaded = relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(facts, catalog))
		skipped, err = s.targetedMaintenanceSkipSet(ctx, catalogFingerprint, loadSkipped, closure.load, affected, instruments)
		if err != nil {
			return result, err
		}
	}
	result.Skipped = skipped

	evidenceBySourceRepo := make(map[string][]relationships.EvidenceFact)
	for _, evidence := range loaded {
		if strings.TrimSpace(evidence.SourceRepoID) == "" || strings.TrimSpace(evidence.TargetRepoID) == "" {
			continue
		}
		if _, ok := closure.affectedRepos[evidence.SourceRepoID]; !ok {
			continue
		}
		evidenceBySourceRepo[evidence.SourceRepoID] = append(evidenceBySourceRepo[evidence.SourceRepoID], evidence)
		result.EvidenceFacts++
	}

	published, err := s.writeTargetedMaintenanceEvidence(ctx, closure, evidenceBySourceRepo, snapshot, catalogFingerprint, instruments)
	if err != nil {
		return result, err
	}
	result.Published = published

	reopened, err := s.reopenTargetedMaintenanceWorkItems(ctx, tracer, instruments, result.Affected, skipped)
	if err != nil {
		return result, err
	}
	result.Reopened = reopened

	duration := time.Since(start).Seconds()
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.Int("owed_partition_count", len(requested)),
		attribute.Int("not_active_partition_count", len(result.NotActive)),
		attribute.Int("promoted_partition_count", len(result.Promoted)),
		attribute.Int("loaded_partition_count", len(result.Loaded)),
		attribute.Int("affected_partition_count", len(result.Affected)),
		attribute.Int("evidence_fact_count", result.EvidenceFacts),
		attribute.Int("published_count", result.Published),
	)
	log.Printf(
		"deferred_backfill_targeted_completed owed=%d not_active=%d promoted=%d loaded_partitions=%d affected_partitions=%d evidence_facts=%d published=%d reopened_deployment_mapping=%d reopened_code_import_repo_edge=%d duration_s=%.2f",
		len(requested), len(result.NotActive), len(result.Promoted), len(result.Loaded), len(result.Affected),
		result.EvidenceFacts, result.Published, reopened["deployment_mapping"], reopened["code_import_repo_edge"], duration,
	)
	return result, nil
}

// normalizeOwedPartitions trims, drops blank, de-duplicates and sorts owed.
func normalizeOwedPartitions(owed []scopeGenerationPartition) []scopeGenerationPartition {
	set := make(map[scopeGenerationPartition]struct{}, len(owed))
	for _, partition := range owed {
		partition.ScopeID = strings.TrimSpace(partition.ScopeID)
		partition.GenerationID = strings.TrimSpace(partition.GenerationID)
		if partition.ScopeID == "" || partition.GenerationID == "" {
			continue
		}
		set[partition] = struct{}{}
	}
	return sortedPartitions(set)
}

// refuseStaleCatalog returns errTargetedMaintenanceCatalogChanged when an
// active partition's memo row records another catalog fingerprint.
func (s IngestionStore) refuseStaleCatalog(ctx context.Context, fingerprint string) error {
	rows, err := s.database.QueryContext(ctx, staleCatalogMemoQuery, fingerprint)
	if err != nil {
		return fmt.Errorf("check catalog fingerprint for partition-scoped maintenance: %w", err)
	}
	defer func() { _ = rows.Close() }()
	stale := false
	if rows.Next() {
		if err := rows.Scan(&stale); err != nil {
			return fmt.Errorf("scan catalog fingerprint check: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("check catalog fingerprint for partition-scoped maintenance: %w", err)
	}
	if stale {
		return errTargetedMaintenanceCatalogChanged
	}
	return nil
}

// targetedMaintenanceSkipSet is the same-pass memo-hit set over every
// partition the pass loads or reopens. Loaded partitions take the decision the
// fact loader already made; affected partitions that were not loaded are
// looked up through the same applyDeferredPartitionMemoGate before any memo row
// of this pass is written. A nil loader decision (gate error) yields nil,
// which reopens every candidate.
func (s IngestionStore) targetedMaintenanceSkipSet(
	ctx context.Context,
	fingerprint string,
	loadSkipped map[scopeGenerationPartition]struct{},
	load map[scopeGenerationPartition]struct{},
	affected map[scopeGenerationPartition]struct{},
	instruments *telemetry.Instruments,
) (map[scopeGenerationPartition]struct{}, error) {
	if loadSkipped == nil {
		return nil, nil
	}
	skipped := make(map[scopeGenerationPartition]struct{}, len(loadSkipped))
	for partition := range loadSkipped {
		skipped[partition] = struct{}{}
	}
	unloaded := make(map[scopeGenerationPartition]struct{})
	for partition := range affected {
		if _, ok := load[partition]; !ok {
			unloaded[partition] = struct{}{}
		}
	}
	if len(unloaded) == 0 {
		return skipped, nil
	}
	gate, err := applyDeferredPartitionMemoGate(
		ctx, newDeferredBackfillPartitionMemoStore(s.database), sortedPartitions(unloaded), fingerprint, instruments)
	if err != nil {
		return nil, nil //nolint:nilerr // a gate error degrades to reopen-all, as in the whole pass
	}
	for _, partition := range gate.Skipped {
		skipped[partition] = struct{}{}
	}
	return skipped, nil
}

// writeTargetedMaintenanceEvidence commits the affected repositories' evidence
// in bounded per-repository batches with the repo-bounded under-lock read, and
// then publishes readiness and memo rows through the unchanged fan-in, which
// runs only after every batch of this pass committed.
func (s IngestionStore) writeTargetedMaintenanceEvidence(
	ctx context.Context,
	closure targetedMaintenanceClosure,
	evidenceBySourceRepo map[string][]relationships.EvidenceFact,
	snapshot map[string]string,
	catalogFingerprint string,
	instruments *telemetry.Instruments,
) (int, error) {
	repoIDs := make([]string, 0, len(closure.affectedRepos))
	for repoID := range closure.affectedRepos {
		repoIDs = append(repoIDs, repoID)
	}
	if len(repoIDs) == 0 {
		return 0, nil
	}
	sort.Strings(repoIDs)
	batchSize := s.maintenanceBatchSize
	if batchSize <= 0 {
		batchSize = deferredMaintenanceRepoBatchSize
	}
	bounds := make([][2]int, 0, (len(repoIDs)+batchSize-1)/batchSize)
	for start := 0; start < len(repoIDs); start += batchSize {
		bounds = append(bounds, [2]int{start, min(start+batchSize, len(repoIDs))})
	}
	workers := max(s.maintenanceWorkers, 1)
	workers = min(workers, len(bounds))

	contributions, err := s.runDeferredBackfillBatchesWith(
		ctx, repoIDs, bounds, workers, evidenceBySourceRepo, snapshot, instruments,
		loadActiveRepositoryGenerationsForRepos,
	)
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("partition-scoped maintenance canceled before readiness publication: %w", err)
	}
	return s.publishDeferredBackfillPartitions(ctx, contributions, snapshot, catalogFingerprint, workers, instruments)
}

// reopenTargetedMaintenanceWorkItems reopens, in ONE transaction, the
// succeeded reducer work items of the exact affected partitions: the
// deployment_mapping and code_import_repo_edge items gated by the same-pass
// memo-hit set, and every CrossScopeCorrelationReopenDomains item NOT gated by
// it (see reopenMaintenanceWorkItemsInTransaction for why). Each listing is the
// whole pass's shipped listing AND an exact-partition predicate.
func (s IngestionStore) reopenTargetedMaintenanceWorkItems(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	affected []scopeGenerationPartition,
	skipped map[scopeGenerationPartition]struct{},
) (map[string]int, error) {
	reopened := make(map[string]int)
	if len(affected) == 0 {
		return reopened, nil
	}
	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "bootstrap.reopen_targeted_work_items")
		defer span.End()
	}
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin partition-scoped reopen transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	queue := ReducerQueue{database: tx, Now: s.Now}

	for _, relationship := range []struct {
		domain string
		query  string
	}{
		{"deployment_mapping", listSucceededDeploymentMappingWorkItemsForPartitionsQuery},
		{"code_import_repo_edge", listSucceededCodeImportRepoEdgeWorkItemsForPartitionsQuery},
	} {
		items, err := listSucceededRelationshipWorkItemsForPartitions(ctx, tx, relationship.query, relationship.domain, affected)
		if err != nil {
			return nil, err
		}
		gate := applyReopenPartitionMemoGate(ctx, relationship.domain, items, skipped, instruments)
		for _, item := range gate.ToReopen {
			if _, err := queue.ReopenSucceeded(ctx, item.WorkItemID); err != nil {
				return nil, fmt.Errorf("reopen %s work items: %w", relationship.domain, err)
			}
		}
		reopened[relationship.domain] = len(gate.ToReopen)
	}
	for _, domain := range CrossScopeCorrelationReopenDomains() {
		workItemIDs, err := listSucceededReducerWorkItemIDsForDomainAndPartitions(ctx, tx, domain, affected)
		if err != nil {
			return nil, err
		}
		for _, workItemID := range workItemIDs {
			if _, err := queue.ReopenSucceeded(ctx, workItemID); err != nil {
				return nil, fmt.Errorf("reopen %s work items: %w", domain, err)
			}
		}
		reopened[domain] = len(workItemIDs)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit partition-scoped reopen transaction: %w", err)
	}
	committed = true

	if instruments != nil {
		instruments.DeploymentMappingReopened.Add(ctx, int64(reopened["deployment_mapping"]))
		instruments.CodeImportRepoEdgeReopened.Add(ctx, int64(reopened["code_import_repo_edge"]))
		for _, domain := range CrossScopeCorrelationReopenDomains() {
			instruments.CorrelationReopened.Add(ctx, int64(reopened[domain]),
				metric.WithAttributes(attribute.String("domain", domain)))
		}
	}
	return reopened, nil
}
