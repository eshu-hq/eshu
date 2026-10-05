// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// targetedCatalogBaselineQuery reports, over the active partitions, whether
// any memo row exists (the baseline the catalog guard needs) and whether any
// memo row records a catalog fingerprint other than $1.
const targetedCatalogBaselineQuery = latestGenerationCTE + `
SELECT
    EXISTS (
        SELECT 1
        FROM deferred_backfill_partition_memo AS memo
        JOIN latest_generations AS latest
          ON latest.scope_id = memo.scope_id
         AND latest.generation_id = memo.generation_id
    ),
    EXISTS (
        SELECT 1
        FROM deferred_backfill_partition_memo AS memo
        JOIN latest_generations AS latest
          ON latest.scope_id = memo.scope_id
         AND latest.generation_id = memo.generation_id
        WHERE memo.catalog_fingerprint <> $1
    )
`

// TargetedMaintenanceResult reports one partition-scoped pass.
type TargetedMaintenanceResult struct {
	// Outcomes holds one result per requested owed partition, in (scope_id,
	// generation_id) order. It is filled on refusal too, so a superseded or
	// inapplicable owed partition is never reported as refused.
	Outcomes []TargetedMaintenanceOutcome
	// Promoted are dependent partitions processed as owed because they had no
	// committed backward_evidence phase.
	Promoted []OwedPartition
	// Loaded are the partitions handed to the memo-gated fact loader.
	Loaded []OwedPartition
	// Affected are the partitions whose evidence was written, whose phase was
	// published, and whose dependents were reopened.
	Affected []OwedPartition
	// SnapshotConflicts are scopes whose generation advanced between the
	// closure reads; their repositories were skipped this pass.
	SnapshotConflicts []string
	// EvidenceFacts counts evidence facts handed to the batch writer.
	EvidenceFacts int
	// Published counts phase rows the fan-in published.
	Published int
	// Reopened counts reopened work items per reducer domain.
	Reopened map[string]int
	// SkipSetUnavailable is true when the memo gate failed and every
	// relationship-domain candidate was reopened.
	SkipSetUnavailable bool
	// SuppressedRefusal is the catalog guard's refusal reason when every
	// active owed partition was inapplicable: the pass returned nil, but the
	// evidence work their relations would have written was not done and
	// waits for the next whole pass.
	SuppressedRefusal string
}

// RunDeferredRelationshipMaintenanceForPartitions runs deferred relationship
// maintenance for the owed (scope_id, generation_id) partitions only. It is
// composed from the whole pass's own pieces so that, for every partition it
// touches, the committed relationship_evidence_facts rows, the
// graph_projection_phase_state and memo rows, and the reopened fact_work_items
// equal what RunDeferredRelationshipMaintenance produces for that partition:
//
//   - owed classification: a partition whose scope moved on is not_active; an
//     active partition with no repository in the shipped active-repository
//     read is inapplicable (no pass can publish its phase). Both are decided
//     before the catalog guard, so neither is reported as a refusal.
//   - catalog guard: refuse with ErrTargetedMaintenanceNoMemoBaseline when no
//     active partition holds a memo row, and with
//     ErrTargetedMaintenanceCatalogChanged when one records another catalog
//     fingerprint. The epoch whole pass completes those obligations.
//   - affected set: resolveTargetedMaintenanceClosure, which adds inbound and
//     cross-scope sources found by loadAnchorScopedRelationshipFacts.
//   - fact load, writes and publication: the whole pass's memo-gated loader,
//     batch writer (with a repo-bounded under-lock read derived from the
//     shipped query) and unchanged fan-in, all with instruments off so the
//     whole pass's eshu_dp_deferred_backfill_* series stay its own.
//   - reopen: reopenTargetedMaintenanceWorkItems over the exact affected
//     partitions. Correlation reopen is partition-scoped here: the fleet-wide
//     replay of the cross-scope correlation domains stays on the epoch whole
//     pass, and is not part of the activation obligation (#7584 ruling D1).
//
// Partitions outside the affected set are not written, published or reopened.
// Per-owed results are in TargetedMaintenanceResult.Outcomes; the returned
// error is a pass-level refusal (match with errors.Is) or an operational
// failure.
func (s IngestionStore) RunDeferredRelationshipMaintenanceForPartitions(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	owed []OwedPartition,
) (result TargetedMaintenanceResult, err error) {
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
	ctx, span, endSpan := startTargetedMaintenanceSpan(ctx, tracer)
	defer endSpan()
	outcomes := make(map[scopeGenerationPartition]TargetedMaintenanceOutcomeKind, len(requested))
	defer func() {
		result.Outcomes = orderedOutcomes(requested, outcomes)
		recordTargetedMaintenance(ctx, span, instruments, start, requested, result, err)
	}()

	// Classify before any corpus read, so a superseded or inapplicable owed
	// partition is reported as such even when the catalog guard would refuse.
	active := make(map[scopeGenerationPartition]struct{}, len(requested))
	for _, partition := range requested {
		generationID, err := loadActiveGenerationForScope(ctx, s.database, partition.ScopeID)
		if err != nil {
			return result, fmt.Errorf("read active generation for owed scope %q: %w", partition.ScopeID, err)
		}
		if generationID != partition.GenerationID {
			outcomes[partition] = TargetedMaintenanceNotActive
			continue
		}
		active[partition] = struct{}{}
	}
	if len(active) == 0 {
		return result, nil
	}
	owedRepos, err := loadActiveRepositoryGenerationsForPartitions(ctx, s.database, sortedPartitions(active))
	if err != nil {
		return result, fmt.Errorf("load owed repository generations: %w", err)
	}
	applicable := partitionsOfRepos(owedRepos)
	for partition := range active {
		if _, ok := applicable[partition]; !ok {
			outcomes[partition] = TargetedMaintenanceInapplicable
		}
	}

	catalog, _, err := loadRepositoryCatalog(ctx, s.database)
	if err != nil {
		return result, fmt.Errorf("load repository catalog for partition-scoped maintenance: %w", err)
	}
	params, hasAnchors := buildDeferredScopedFactQueryParams(catalog)
	catalogFingerprint := deferredCatalogFingerprint(params)
	switch refusal := s.targetedCatalogRefusal(ctx, catalogFingerprint); {
	case refusal == nil:
	case errors.Is(refusal, ErrTargetedMaintenanceCatalogChanged), errors.Is(refusal, ErrTargetedMaintenanceNoMemoBaseline):
		if len(applicable) == 0 {
			// Every active owed partition is inapplicable: no phase is owed,
			// so there is nothing to refuse. The refusal still suppressed the
			// evidence work these partitions' relations would have written
			// for other repositories; that waits for the next whole pass
			// (review N1), so say so.
			result.SuppressedRefusal = TargetedMaintenanceReason(refusal)
			log.Printf("deferred_backfill_targeted_suppressed reason=%q inapplicable=%d",
				result.SuppressedRefusal, len(active))
			return result, nil
		}
		for _, partition := range sortedPartitions(applicable) {
			log.Printf("deferred_backfill_targeted_refused reason=%q scope_id=%q generation_id=%q",
				TargetedMaintenanceReason(refusal), partition.ScopeID, partition.GenerationID)
		}
		return result, refusal
	default:
		return result, refusal
	}

	closure, err := s.resolveTargetedMaintenanceClosure(ctx, catalog, params, hasAnchors, active)
	if err != nil {
		return result, err
	}
	result.Promoted = owedPartitionsOf(closure.promoted)
	result.Loaded = owedPartitionsOf(sortedPartitions(closure.load))
	affected := closure.affectedPartitions()
	affectedList := sortedPartitions(affected)
	result.Affected = owedPartitionsOf(affectedList)

	var snapshot map[string]string
	var discovered []relationships.EvidenceFact
	skipped := map[scopeGenerationPartition]struct{}{}
	if hasAnchors {
		snapshot, result.SnapshotConflicts = buildTargetedMaintenanceSnapshot(closure.load, affected)
		loaded, loadSkipped, err := s.loadDeferredScopedFactsAcrossPartitions(
			ctx, s.database, params, sortedPartitions(closure.load), nil)
		if err != nil {
			return result, fmt.Errorf("load partition-scoped deferred facts: %w", err)
		}
		if len(loaded) > 0 {
			loaded, err = s.appendArgoCDGeneratorConfigFacts(ctx, s.database, catalog, loaded)
			if err != nil {
				return result, err
			}
		}
		discovered = relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(loaded, catalog))
		skipped = s.targetedMaintenanceSkipSet(ctx, catalogFingerprint, loadSkipped, closure.load, affected)
		result.SkipSetUnavailable = skipped == nil
	}

	evidenceBySourceRepo := make(map[string][]relationships.EvidenceFact)
	for _, evidence := range discovered {
		if strings.TrimSpace(evidence.SourceRepoID) == "" || strings.TrimSpace(evidence.TargetRepoID) == "" {
			continue
		}
		if _, ok := closure.affectedRepos[evidence.SourceRepoID]; !ok {
			continue
		}
		evidenceBySourceRepo[evidence.SourceRepoID] = append(evidenceBySourceRepo[evidence.SourceRepoID], evidence)
		result.EvidenceFacts++
	}

	if result.Published, err = s.writeTargetedMaintenanceEvidence(
		ctx, closure, evidenceBySourceRepo, snapshot, catalogFingerprint,
	); err != nil {
		return result, err
	}
	if result.Reopened, err = s.reopenTargetedMaintenanceWorkItems(ctx, tracer, affectedList, skipped); err != nil {
		return result, err
	}

	for partition := range applicable {
		committed, err := backwardEvidencePhaseCommitted(ctx, s.database, partition)
		if err != nil {
			return result, err
		}
		if committed {
			outcomes[partition] = TargetedMaintenancePublished
		} else {
			outcomes[partition] = TargetedMaintenanceRetry
		}
	}
	return result, nil
}

// targetedCatalogRefusal returns ErrTargetedMaintenanceNoMemoBaseline when no
// active partition holds a memo row, ErrTargetedMaintenanceCatalogChanged when
// one records another fingerprint, nil when the catalog is unchanged, and the
// query error otherwise.
func (s IngestionStore) targetedCatalogRefusal(ctx context.Context, fingerprint string) error {
	rows, err := s.database.QueryContext(ctx, targetedCatalogBaselineQuery, fingerprint)
	if err != nil {
		return fmt.Errorf("check catalog fingerprint for partition-scoped maintenance: %w", err)
	}
	defer func() { _ = rows.Close() }()
	hasBaseline, stale := false, false
	if rows.Next() {
		if err := rows.Scan(&hasBaseline, &stale); err != nil {
			return fmt.Errorf("scan catalog fingerprint check: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("check catalog fingerprint for partition-scoped maintenance: %w", err)
	}
	switch {
	case !hasBaseline:
		return ErrTargetedMaintenanceNoMemoBaseline
	case stale:
		return ErrTargetedMaintenanceCatalogChanged
	}
	return nil
}

// recordTargetedMaintenance emits the pass's metrics, span status and
// completion log. The pass outcome is "completed" or the error's reason.
// startTargetedMaintenanceSpan opens the pass span. Without a tracer it
// installs a non-recording span, so the pass never writes status or
// attributes onto a span its caller's context carries (review N2).
func startTargetedMaintenanceSpan(ctx context.Context, tracer trace.Tracer) (context.Context, trace.Span, func()) {
	if tracer == nil {
		span := trace.SpanFromContext(context.Background())
		return trace.ContextWithSpan(ctx, span), span, func() {}
	}
	ctx, span := tracer.Start(ctx, "relationship.backfill_deferred_targeted")
	return ctx, span, func() { span.End() }
}

func recordTargetedMaintenance(
	ctx context.Context,
	span trace.Span,
	instruments *telemetry.Instruments,
	start time.Time,
	requested []scopeGenerationPartition,
	result TargetedMaintenanceResult,
	err error,
) {
	duration := time.Since(start).Seconds()
	passOutcome := "completed"
	switch {
	case err != nil:
		passOutcome = TargetedMaintenanceReason(err)
		// A typed refusal is a designed hold the consumer retries, not a
		// trace error (review N2); only an untyped failure is.
		if passOutcome == "error" {
			span.RecordError(err)
			span.SetStatus(codes.Error, passOutcome)
		}
	case result.SuppressedRefusal != "":
		passOutcome = "suppressed"
	}
	counts := make(map[TargetedMaintenanceOutcomeKind]int)
	for _, outcome := range result.Outcomes {
		if err != nil && outcome.Kind == TargetedMaintenanceRetry {
			// A refused pass holds its applicable owed partitions; they are
			// not retries of a pass that ran (review N3).
			continue
		}
		counts[outcome.Kind]++
	}
	if instruments != nil {
		// The duration histogram carries the pass outcome (its count is the
		// pass count); outcomes_total counts owed partitions only (review N3).
		instruments.DeferredBackfillTargetedDuration.Record(ctx, duration,
			metric.WithAttributes(telemetry.AttrOutcome(passOutcome)))
		for kind, count := range counts {
			instruments.DeferredBackfillTargetedOutcomes.Add(ctx, int64(count),
				metric.WithAttributes(telemetry.AttrOutcome(string(kind))))
		}
		for domain, count := range result.Reopened {
			instruments.DeferredBackfillTargetedReopened.Add(ctx, int64(count),
				metric.WithAttributes(telemetry.AttrDomain(domain)))
		}
	}
	span.SetAttributes(
		attribute.String("outcome", passOutcome),
		attribute.Int("owed_partition_count", len(requested)),
		attribute.Int("published_owed_count", counts[TargetedMaintenancePublished]),
		attribute.Int("not_active_owed_count", counts[TargetedMaintenanceNotActive]),
		attribute.Int("inapplicable_owed_count", counts[TargetedMaintenanceInapplicable]),
		attribute.Int("retry_owed_count", counts[TargetedMaintenanceRetry]),
		attribute.Int("promoted_partition_count", len(result.Promoted)),
		attribute.Int("loaded_partition_count", len(result.Loaded)),
		attribute.Int("affected_partition_count", len(result.Affected)),
		attribute.Int("evidence_fact_count", result.EvidenceFacts),
		attribute.Int("published_count", result.Published),
	)
	log.Printf(
		"deferred_backfill_targeted_completed outcome=%s owed=%d published_owed=%d not_active=%d inapplicable=%d retry=%d promoted=%d loaded_partitions=%d affected_partitions=%d snapshot_conflicts=%d evidence_facts=%d published=%d skip_set_unavailable=%t duration_s=%.2f",
		passOutcome, len(requested), counts[TargetedMaintenancePublished], counts[TargetedMaintenanceNotActive],
		counts[TargetedMaintenanceInapplicable], counts[TargetedMaintenanceRetry], len(result.Promoted),
		len(result.Loaded), len(result.Affected), len(result.SnapshotConflicts), result.EvidenceFacts,
		result.Published, result.SkipSetUnavailable, duration,
	)
}

// normalizeOwedPartitions trims, drops blank, de-duplicates and sorts owed.
func normalizeOwedPartitions(owed []OwedPartition) []scopeGenerationPartition {
	set := make(map[scopeGenerationPartition]struct{}, len(owed))
	for _, partition := range owed {
		key := scopeGenerationPartition{
			ScopeID:      strings.TrimSpace(partition.ScopeID),
			GenerationID: strings.TrimSpace(partition.GenerationID),
		}
		if key.ScopeID == "" || key.GenerationID == "" {
			continue
		}
		set[key] = struct{}{}
	}
	return sortedPartitions(set)
}

// orderedOutcomes returns one outcome per requested partition, in request
// order; a partition the pass never classified (an operational error stopped
// it first) reports retry.
func orderedOutcomes(
	requested []scopeGenerationPartition,
	kinds map[scopeGenerationPartition]TargetedMaintenanceOutcomeKind,
) []TargetedMaintenanceOutcome {
	outcomes := make([]TargetedMaintenanceOutcome, 0, len(requested))
	for _, partition := range requested {
		kind, ok := kinds[partition]
		if !ok {
			kind = TargetedMaintenanceRetry
		}
		outcomes = append(outcomes, TargetedMaintenanceOutcome{
			Partition: OwedPartition(partition),
			Kind:      kind,
		})
	}
	return outcomes
}

// partitionsOfRepos returns the distinct partitions of identities.
func partitionsOfRepos(identities map[string]repositoryGenerationIdentity) map[scopeGenerationPartition]struct{} {
	partitions := make(map[scopeGenerationPartition]struct{}, len(identities))
	for _, identity := range identities {
		partitions[scopeGenerationPartition{ScopeID: identity.ScopeID, GenerationID: identity.GenerationID}] = struct{}{}
	}
	return partitions
}

// owedPartitionsOf converts internal partitions to the exported shape.
func owedPartitionsOf(partitions []scopeGenerationPartition) []OwedPartition {
	out := make([]OwedPartition, 0, len(partitions))
	for _, partition := range partitions {
		out = append(out, OwedPartition(partition))
	}
	return out
}
