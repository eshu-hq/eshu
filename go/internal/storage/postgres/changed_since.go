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

// ComputeChangedSinceDelta computes one bounded changed-since summary for a
// repository-kind scope. It resolves the scope and its current active
// generation, resolves the prior generation named by SinceGenerationID or the
// generation observed at or before SinceObservedAt, then diffs the two fact sets
// keyed by (scope_id, generation_id, stable_fact_key) into per-category,
// per-classification counts and bounded sample handles.
//
// Resolution failures are explicit, never confident emptiness:
//
//   - An unknown scope/repository returns Unavailable=false with an empty
//     ScopeID so the handler emits scope_not_found.
//   - A since reference that resolves to no generation returns an empty
//     SinceGenerationID so the handler emits not_found.
//   - A scope with no current active generation returns Unavailable=true so the
//     handler reports an unavailable diff rather than zero deltas.
//
// The counts are exact; only the per-classification sample lists are capped at
// the normalized SampleLimit, with a Truncated flag when more keys matched.
func (s StatusStore) ComputeChangedSinceDelta(
	ctx context.Context,
	filter statuspkg.ChangedSinceFilter,
) (statuspkg.ChangedSinceSummary, error) {
	if s.queryer == nil {
		return statuspkg.ChangedSinceSummary{}, fmt.Errorf("queryer is required")
	}

	filter = filter.Normalize()
	if !filter.HasScopeSelector() {
		return statuspkg.ChangedSinceSummary{}, fmt.Errorf("scope_id or repository is required")
	}
	if filter.HasConflictingScopeSelectors() {
		return statuspkg.ChangedSinceSummary{}, fmt.Errorf("scope_id and repository are mutually exclusive")
	}

	scope, ok, err := s.resolveChangedSinceScope(ctx, filter)
	if err != nil {
		return statuspkg.ChangedSinceSummary{}, err
	}
	if !ok {
		// Unknown scope/repository: empty ScopeID signals not-found upstream.
		return statuspkg.ChangedSinceSummary{}, nil
	}
	repository := ""
	if scope.scopeKind == "repository" {
		repository = scope.repository
	}

	summary := statuspkg.ChangedSinceSummary{
		ScopeID:                   scope.scopeID,
		ScopeKind:                 scope.scopeKind,
		Repository:                repository,
		CurrentActiveGenerationID: scope.currentGenerationID,
		CurrentObservedAt:         statuspkg.ChangedSinceTimestamp(scope.currentObservedAt),
		SampleLimit:               filter.SampleLimit,
		Building:                  scope.hasPending,
	}

	if scope.currentGenerationID == "" {
		// No committed current snapshot: the diff cannot be computed. Report it
		// explicitly instead of returning all-unchanged zero deltas.
		summary.Unavailable = true
		summary.Categories = unavailableChangedSinceCategories()
		return summary, nil
	}

	prior, priorOK, err := s.resolveChangedSincePriorGeneration(ctx, scope.scopeID, filter)
	if err != nil {
		return statuspkg.ChangedSinceSummary{}, err
	}
	if !priorOK {
		expired, expiredObservedAt, expiredErr := s.changedSinceRetentionExpired(ctx, scope.scopeID, filter)
		if expiredErr != nil {
			return statuspkg.ChangedSinceSummary{}, expiredErr
		}
		if expired {
			summary.Unavailable = true
			summary.UnavailableReason = statuspkg.ChangedSinceUnavailableRetentionExpired
			summary.SinceGenerationID = filter.SinceGenerationID
			summary.SinceObservedAt = statuspkg.ChangedSinceTimestamp(expiredObservedAt)
			summary.Categories = unavailableChangedSinceCategories()
			return summary, nil
		}
		// The since reference matched no generation for this scope.
		return summary, nil
	}
	summary.SinceGenerationID = prior.generationID
	summary.SinceObservedAt = statuspkg.ChangedSinceTimestamp(prior.observedAt)

	counts, err := s.changedSinceCounts(ctx, scope.scopeID, prior.generationID, scope.currentGenerationID)
	if err != nil {
		return statuspkg.ChangedSinceSummary{}, err
	}

	categories := make([]statuspkg.ChangedSinceCategoryDelta, 0, len(statuspkg.ChangedSinceCategories))
	for _, category := range statuspkg.ChangedSinceCategories {
		delta := statuspkg.ChangedSinceCategoryDelta{
			Category: category,
			Counts:   counts[category],
		}
		samples, truncated, sampleErr := s.changedSinceSamples(
			ctx,
			scope.scopeID,
			prior.generationID,
			scope.currentGenerationID,
			category,
			counts[category],
			filter.SampleLimit,
		)
		if sampleErr != nil {
			return statuspkg.ChangedSinceSummary{}, sampleErr
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

type changedSinceScope struct {
	scopeID             string
	scopeKind           string
	repository          string
	currentGenerationID string
	currentObservedAt   time.Time
	hasPending          bool
}

func (s StatusStore) resolveChangedSinceScope(
	ctx context.Context,
	filter statuspkg.ChangedSinceFilter,
) (changedSinceScope, bool, error) {
	rows, err := s.queryer.QueryContext(
		ctx,
		resolveChangedSinceScopeQuery,
		filter.ScopeID,
		filter.Repository,
	)
	if err != nil {
		return changedSinceScope{}, false, fmt.Errorf("resolve changed-since scope: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return changedSinceScope{}, false, fmt.Errorf("resolve changed-since scope: %w", err)
		}
		return changedSinceScope{}, false, nil
	}

	var scope changedSinceScope
	var currentObserved sql.NullTime
	if err := rows.Scan(
		&scope.scopeID,
		&scope.scopeKind,
		&scope.repository,
		&scope.currentGenerationID,
		&currentObserved,
		&scope.hasPending,
	); err != nil {
		return changedSinceScope{}, false, fmt.Errorf("resolve changed-since scope: %w", err)
	}
	if err := rows.Err(); err != nil {
		return changedSinceScope{}, false, fmt.Errorf("resolve changed-since scope: %w", err)
	}
	if currentObserved.Valid {
		scope.currentObservedAt = currentObserved.Time
	}
	return scope, true, nil
}

type changedSincePrior struct {
	generationID string
	observedAt   time.Time
}

func (s StatusStore) resolveChangedSincePriorGeneration(
	ctx context.Context,
	scopeID string,
	filter statuspkg.ChangedSinceFilter,
) (changedSincePrior, bool, error) {
	sinceObserved := filter.SinceObservedAt
	rows, err := s.queryer.QueryContext(
		ctx,
		resolveChangedSinceGenerationQuery,
		scopeID,
		filter.SinceGenerationID,
		sinceObserved.UTC(),
	)
	if err != nil {
		return changedSincePrior{}, false, fmt.Errorf("resolve changed-since prior generation: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return changedSincePrior{}, false, fmt.Errorf("resolve changed-since prior generation: %w", err)
		}
		return changedSincePrior{}, false, nil
	}

	var prior changedSincePrior
	var observed sql.NullTime
	if err := rows.Scan(&prior.generationID, &observed); err != nil {
		return changedSincePrior{}, false, fmt.Errorf("resolve changed-since prior generation: %w", err)
	}
	if err := rows.Err(); err != nil {
		return changedSincePrior{}, false, fmt.Errorf("resolve changed-since prior generation: %w", err)
	}
	if observed.Valid {
		prior.observedAt = observed.Time
	}
	return prior, true, nil
}

func (s StatusStore) changedSinceRetentionExpired(
	ctx context.Context,
	scopeID string,
	filter statuspkg.ChangedSinceFilter,
) (bool, time.Time, error) {
	generationHash := ""
	if filter.SinceGenerationID != "" {
		generationHash = retentionHashID("generation", filter.SinceGenerationID)
	}
	rows, err := s.queryer.QueryContext(
		ctx,
		resolveChangedSinceRetentionExpiredQuery,
		retentionHashID("scope", scopeID),
		generationHash,
		filter.SinceObservedAt.UTC(),
	)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("resolve changed-since retention expiry: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, time.Time{}, fmt.Errorf("resolve changed-since retention expiry: %w", err)
		}
		return false, time.Time{}, nil
	}

	var expired bool
	var observedAt sql.NullTime
	if err := rows.Scan(&expired, &observedAt); err != nil {
		return false, time.Time{}, fmt.Errorf("resolve changed-since retention expiry: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, time.Time{}, fmt.Errorf("resolve changed-since retention expiry: %w", err)
	}
	if observedAt.Valid {
		return expired, observedAt.Time, nil
	}
	return expired, filter.SinceObservedAt, nil
}

func (s StatusStore) changedSinceCounts(
	ctx context.Context,
	scopeID, priorGenerationID, currentGenerationID string,
) (map[statuspkg.ChangedSinceCategory]statuspkg.ChangedSinceCounts, error) {
	rows, err := s.queryer.QueryContext(
		ctx,
		changedSinceCountsQuery,
		scopeID,
		priorGenerationID,
		currentGenerationID,
	)
	if err != nil {
		return nil, fmt.Errorf("changed-since counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := map[statuspkg.ChangedSinceCategory]statuspkg.ChangedSinceCounts{}
	for rows.Next() {
		var category string
		var classification string
		var keyCount int64
		if err := rows.Scan(&category, &classification, &keyCount); err != nil {
			return nil, fmt.Errorf("changed-since counts: %w", err)
		}
		bucket := counts[statuspkg.ChangedSinceCategory(category)]
		applyChangedSinceCount(&bucket, classification, int(keyCount))
		counts[statuspkg.ChangedSinceCategory(category)] = bucket
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("changed-since counts: %w", err)
	}
	return counts, nil
}

func applyChangedSinceCount(bucket *statuspkg.ChangedSinceCounts, classification string, value int) {
	switch statuspkg.ChangedSinceClassification(classification) {
	case statuspkg.ChangedSinceAdded:
		bucket.Added = value
	case statuspkg.ChangedSinceUpdated:
		bucket.Updated = value
	case statuspkg.ChangedSinceUnchanged:
		bucket.Unchanged = value
	case statuspkg.ChangedSinceRetired:
		bucket.Retired = value
	case statuspkg.ChangedSinceSuperseded:
		bucket.Superseded = value
	}
}

func (s StatusStore) changedSinceSamples(
	ctx context.Context,
	scopeID, priorGenerationID, currentGenerationID string,
	category statuspkg.ChangedSinceCategory,
	counts statuspkg.ChangedSinceCounts,
	sampleLimit int,
) (map[statuspkg.ChangedSinceClassification][]statuspkg.ChangedSinceSample, map[statuspkg.ChangedSinceClassification]bool, error) {
	samples := map[statuspkg.ChangedSinceClassification][]statuspkg.ChangedSinceSample{}
	truncated := map[statuspkg.ChangedSinceClassification]bool{}

	for _, classification := range statuspkg.ChangedSinceClassifications {
		// Only query buckets that have keys, so an empty diff makes no sample reads.
		if changedSinceClassificationCount(counts, classification) == 0 {
			continue
		}
		bucket, isTruncated, err := s.changedSinceSampleBucket(
			ctx,
			scopeID,
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

func changedSinceClassificationCount(counts statuspkg.ChangedSinceCounts, classification statuspkg.ChangedSinceClassification) int {
	switch classification {
	case statuspkg.ChangedSinceAdded:
		return counts.Added
	case statuspkg.ChangedSinceUpdated:
		return counts.Updated
	case statuspkg.ChangedSinceUnchanged:
		return counts.Unchanged
	case statuspkg.ChangedSinceRetired:
		return counts.Retired
	case statuspkg.ChangedSinceSuperseded:
		return counts.Superseded
	default:
		return 0
	}
}

func (s StatusStore) changedSinceSampleBucket(
	ctx context.Context,
	scopeID, priorGenerationID, currentGenerationID, category, classification string,
	sampleLimit int,
) ([]statuspkg.ChangedSinceSample, bool, error) {
	fetch := sampleLimit + 1
	rows, err := s.queryer.QueryContext(
		ctx,
		changedSinceSamplesQuery,
		scopeID,
		priorGenerationID,
		currentGenerationID,
		category,
		classification,
		fetch,
	)
	if err != nil {
		return nil, false, fmt.Errorf("changed-since samples: %w", err)
	}
	defer func() { _ = rows.Close() }()

	bucket := make([]statuspkg.ChangedSinceSample, 0, sampleLimit)
	for rows.Next() {
		var sample statuspkg.ChangedSinceSample
		if err := rows.Scan(&sample.StableFactKey, &sample.FactKind); err != nil {
			return nil, false, fmt.Errorf("changed-since samples: %w", err)
		}
		bucket = append(bucket, sample)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("changed-since samples: %w", err)
	}

	truncated := len(bucket) > sampleLimit
	if truncated {
		bucket = bucket[:sampleLimit]
	}
	return bucket, truncated, nil
}

func unavailableChangedSinceCategories() []statuspkg.ChangedSinceCategoryDelta {
	categories := make([]statuspkg.ChangedSinceCategoryDelta, 0, len(statuspkg.ChangedSinceCategories))
	for _, category := range statuspkg.ChangedSinceCategories {
		categories = append(categories, statuspkg.ChangedSinceCategoryDelta{
			Category:    category,
			Unavailable: true,
		})
	}
	return categories
}
