// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// ComputeServiceChangedSinceDelta computes one bounded service-scope
// changed-since summary (#1943). It resolves the service and its current active
// materialization generation, resolves the prior generation named by
// SinceGenerationID, then diffs the two evidence snapshot sets keyed by
// (generation_id, service_evidence_key) into per-family, per-classification
// counts and bounded sample handles. It reuses the repository-scope classification
// verbatim; only the lineage table and key space differ.
//
// Resolution failures are explicit, never confident emptiness:
//
//   - An unknown service returns an empty ServiceID so the handler emits
//     service_not_found.
//   - A since reference that resolves to no generation returns an empty
//     SinceGenerationID so the handler emits not_found.
//   - A service with no current active generation returns Unavailable=true so the
//     handler reports an unavailable diff rather than zero deltas.
func (s StatusStore) ComputeServiceChangedSinceDelta(
	ctx context.Context,
	filter statuspkg.ServiceChangedSinceFilter,
) (statuspkg.ServiceChangedSinceSummary, error) {
	if s.queryer == nil {
		return statuspkg.ServiceChangedSinceSummary{}, fmt.Errorf("queryer is required")
	}

	filter = filter.Normalize()

	scope, ok, err := s.resolveServiceChangedSinceScope(ctx, filter.ServiceID)
	if err != nil {
		return statuspkg.ServiceChangedSinceSummary{}, err
	}
	if !ok {
		// Unknown service: empty ServiceID signals not-found upstream.
		return statuspkg.ServiceChangedSinceSummary{}, nil
	}

	summary := statuspkg.ServiceChangedSinceSummary{
		ServiceID:                 scope.serviceID,
		CurrentActiveGenerationID: scope.currentGenerationID,
		CurrentObservedAt:         statuspkg.ChangedSinceTimestamp(scope.currentObservedAt),
		SampleLimit:               filter.SampleLimit,
		Building:                  scope.hasPending,
	}

	if scope.currentGenerationID == "" {
		summary.Unavailable = true
		summary.Categories = unavailableServiceChangedSinceCategories()
		return summary, nil
	}

	prior, priorOK, err := s.resolveServiceChangedSincePriorGeneration(ctx, scope.serviceID, filter.SinceGenerationID)
	if err != nil {
		return statuspkg.ServiceChangedSinceSummary{}, err
	}
	if !priorOK {
		return summary, nil
	}
	summary.SinceGenerationID = prior.generationID
	summary.SinceObservedAt = statuspkg.ChangedSinceTimestamp(prior.observedAt)

	counts, err := s.serviceChangedSinceCounts(ctx, prior.generationID, scope.currentGenerationID)
	if err != nil {
		return statuspkg.ServiceChangedSinceSummary{}, err
	}

	categories := make([]statuspkg.ChangedSinceCategoryDelta, 0, len(statuspkg.ServiceChangedSinceCategories))
	for _, category := range statuspkg.ServiceChangedSinceCategories {
		delta := statuspkg.ChangedSinceCategoryDelta{
			Category: category,
			Counts:   counts[category],
		}
		samples, truncated, sampleErr := s.serviceChangedSinceSamples(
			ctx,
			prior.generationID,
			scope.currentGenerationID,
			category,
			counts[category],
			filter.SampleLimit,
		)
		if sampleErr != nil {
			return statuspkg.ServiceChangedSinceSummary{}, sampleErr
		}
		if len(samples) > 0 {
			delta.Samples = samples
		}
		if len(truncated) > 0 {
			delta.Truncated = truncated
		}
		categories = append(categories, delta)
	}
	summary.Categories = categories

	return summary, nil
}

type serviceChangedSinceScope struct {
	serviceID           string
	currentGenerationID string
	currentObservedAt   time.Time
	hasPending          bool
}

func (s StatusStore) resolveServiceChangedSinceScope(
	ctx context.Context,
	serviceID string,
) (serviceChangedSinceScope, bool, error) {
	rows, err := s.queryer.QueryContext(ctx, resolveServiceChangedSinceScopeQuery, serviceID)
	if err != nil {
		return serviceChangedSinceScope{}, false, fmt.Errorf("resolve service changed-since scope: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return serviceChangedSinceScope{}, false, fmt.Errorf("resolve service changed-since scope: %w", err)
		}
		return serviceChangedSinceScope{}, false, nil
	}

	var scope serviceChangedSinceScope
	var currentObserved sql.NullTime
	if err := rows.Scan(&scope.serviceID, &scope.currentGenerationID, &currentObserved, &scope.hasPending); err != nil {
		return serviceChangedSinceScope{}, false, fmt.Errorf("resolve service changed-since scope: %w", err)
	}
	if err := rows.Err(); err != nil {
		return serviceChangedSinceScope{}, false, fmt.Errorf("resolve service changed-since scope: %w", err)
	}
	if currentObserved.Valid {
		scope.currentObservedAt = currentObserved.Time
	}
	return scope, true, nil
}

type serviceChangedSincePrior struct {
	generationID string
	observedAt   time.Time
}

func (s StatusStore) resolveServiceChangedSincePriorGeneration(
	ctx context.Context,
	serviceID, sinceGenerationID string,
) (serviceChangedSincePrior, bool, error) {
	rows, err := s.queryer.QueryContext(
		ctx,
		resolveServiceChangedSincePriorGenerationQuery,
		serviceID,
		sinceGenerationID,
	)
	if err != nil {
		return serviceChangedSincePrior{}, false, fmt.Errorf("resolve service changed-since prior generation: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return serviceChangedSincePrior{}, false, fmt.Errorf("resolve service changed-since prior generation: %w", err)
		}
		return serviceChangedSincePrior{}, false, nil
	}

	var prior serviceChangedSincePrior
	var observed sql.NullTime
	if err := rows.Scan(&prior.generationID, &observed); err != nil {
		return serviceChangedSincePrior{}, false, fmt.Errorf("resolve service changed-since prior generation: %w", err)
	}
	if err := rows.Err(); err != nil {
		return serviceChangedSincePrior{}, false, fmt.Errorf("resolve service changed-since prior generation: %w", err)
	}
	if observed.Valid {
		prior.observedAt = observed.Time
	}
	return prior, true, nil
}

func (s StatusStore) serviceChangedSinceCounts(
	ctx context.Context,
	priorGenerationID, currentGenerationID string,
) (map[statuspkg.ChangedSinceCategory]statuspkg.ChangedSinceCounts, error) {
	rows, err := s.queryer.QueryContext(
		ctx,
		serviceChangedSinceCountsQuery,
		priorGenerationID,
		currentGenerationID,
	)
	if err != nil {
		return nil, fmt.Errorf("service changed-since counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := map[statuspkg.ChangedSinceCategory]statuspkg.ChangedSinceCounts{}
	for rows.Next() {
		var family string
		var classification string
		var keyCount int64
		if err := rows.Scan(&family, &classification, &keyCount); err != nil {
			return nil, fmt.Errorf("service changed-since counts: %w", err)
		}
		bucket := counts[statuspkg.ChangedSinceCategory(family)]
		applyChangedSinceCount(&bucket, classification, int(keyCount))
		counts[statuspkg.ChangedSinceCategory(family)] = bucket
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("service changed-since counts: %w", err)
	}
	return counts, nil
}

func (s StatusStore) serviceChangedSinceSamples(
	ctx context.Context,
	priorGenerationID, currentGenerationID string,
	category statuspkg.ChangedSinceCategory,
	counts statuspkg.ChangedSinceCounts,
	sampleLimit int,
) (map[statuspkg.ChangedSinceClassification][]statuspkg.ChangedSinceSample, map[statuspkg.ChangedSinceClassification]bool, error) {
	samples := map[statuspkg.ChangedSinceClassification][]statuspkg.ChangedSinceSample{}
	truncated := map[statuspkg.ChangedSinceClassification]bool{}

	for _, classification := range statuspkg.ChangedSinceClassifications {
		if changedSinceClassificationCount(counts, classification) == 0 {
			continue
		}
		bucket, isTruncated, err := s.serviceChangedSinceSampleBucket(
			ctx,
			priorGenerationID,
			currentGenerationID,
			string(category),
			string(classification),
			sampleLimit,
		)
		if err != nil {
			return nil, nil, err
		}
		if len(bucket) > 0 {
			samples[classification] = bucket
		}
		if isTruncated {
			truncated[classification] = true
		}
	}
	return samples, truncated, nil
}

func (s StatusStore) serviceChangedSinceSampleBucket(
	ctx context.Context,
	priorGenerationID, currentGenerationID, family, classification string,
	sampleLimit int,
) ([]statuspkg.ChangedSinceSample, bool, error) {
	fetch := sampleLimit + 1
	rows, err := s.queryer.QueryContext(
		ctx,
		serviceChangedSinceSamplesQuery,
		priorGenerationID,
		currentGenerationID,
		family,
		classification,
		fetch,
	)
	if err != nil {
		return nil, false, fmt.Errorf("service changed-since samples: %w", err)
	}
	defer func() { _ = rows.Close() }()

	bucket := make([]statuspkg.ChangedSinceSample, 0, sampleLimit)
	for rows.Next() {
		var evidenceKey string
		if err := rows.Scan(&evidenceKey); err != nil {
			return nil, false, fmt.Errorf("service changed-since samples: %w", err)
		}
		// FactKind carries the evidence family so a caller can drill into the row
		// the same way repository-scope samples carry fact_kind.
		bucket = append(bucket, statuspkg.ChangedSinceSample{StableFactKey: evidenceKey, FactKind: family})
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("service changed-since samples: %w", err)
	}

	truncated := len(bucket) > sampleLimit
	if truncated {
		bucket = bucket[:sampleLimit]
	}
	return bucket, truncated, nil
}

func unavailableServiceChangedSinceCategories() []statuspkg.ChangedSinceCategoryDelta {
	categories := make([]statuspkg.ChangedSinceCategoryDelta, 0, len(statuspkg.ServiceChangedSinceCategories))
	for _, category := range statuspkg.ServiceChangedSinceCategories {
		categories = append(categories, statuspkg.ChangedSinceCategoryDelta{
			Category:    category,
			Unavailable: true,
		})
	}
	return categories
}
