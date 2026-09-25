// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
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

	categories, err := s.changedSinceCategories(
		ctx,
		scope.scopeID,
		prior.generationID,
		scope.currentGenerationID,
		filter.SampleLimit,
	)
	if err != nil {
		return statuspkg.ChangedSinceSummary{}, err
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
		filter.Scoped,
		array.Of(filter.AllowedRepositoryIDs),
		array.Of(filter.AllowedScopeIDs),
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

// changedSinceCategories evaluates the diff between two generations of one scope
// in a single statement and assembles the per-category counts, bounded ordered
// samples, and truncation flags. Counts are exact; only the samples are capped
// at sampleLimit (the statement fetches sampleLimit+1 to detect truncation).
func (s StatusStore) changedSinceCategories(
	ctx context.Context,
	scopeID, priorGenerationID, currentGenerationID string,
	sampleLimit int,
) ([]statuspkg.ChangedSinceCategoryDelta, error) {
	rows, err := s.queryer.QueryContext(
		ctx,
		changedSinceDeltaQuery,
		scopeID,
		priorGenerationID,
		currentGenerationID,
		sampleLimit+1,
	)
	if err != nil {
		return nil, fmt.Errorf("changed-since diff: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := map[statuspkg.ChangedSinceCategory]statuspkg.ChangedSinceCounts{}
	samples := map[statuspkg.ChangedSinceCategory]map[statuspkg.ChangedSinceClassification][]statuspkg.ChangedSinceSample{}
	for rows.Next() {
		var category, classification string
		var keyCount int64
		var stableFactKey, factKind sql.NullString
		if err := rows.Scan(&category, &classification, &keyCount, &stableFactKey, &factKind); err != nil {
			return nil, fmt.Errorf("changed-since diff: %w", err)
		}
		cat := statuspkg.ChangedSinceCategory(category)
		class := statuspkg.ChangedSinceClassification(classification)
		bucket := counts[cat]
		applyChangedSinceCount(&bucket, classification, int(keyCount))
		counts[cat] = bucket
		if !stableFactKey.Valid {
			continue
		}
		if samples[cat] == nil {
			samples[cat] = map[statuspkg.ChangedSinceClassification][]statuspkg.ChangedSinceSample{}
		}
		samples[cat][class] = append(samples[cat][class], statuspkg.ChangedSinceSample{
			StableFactKey: stableFactKey.String,
			FactKind:      factKind.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("changed-since diff: %w", err)
	}

	categories := make([]statuspkg.ChangedSinceCategoryDelta, 0, len(statuspkg.ChangedSinceCategories))
	for _, category := range statuspkg.ChangedSinceCategories {
		delta := statuspkg.ChangedSinceCategoryDelta{Category: category, Counts: counts[category]}
		truncated := map[statuspkg.ChangedSinceClassification]bool{}
		for classification, bucket := range samples[category] {
			if len(bucket) > sampleLimit {
				bucket = bucket[:sampleLimit]
				truncated[classification] = true
				samples[category][classification] = bucket
			}
		}
		if len(samples[category]) > 0 {
			delta.Samples = samples[category]
		}
		if len(truncated) > 0 {
			delta.Truncated = truncated
		}
		categories = append(categories, delta)
	}
	return categories, nil
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
