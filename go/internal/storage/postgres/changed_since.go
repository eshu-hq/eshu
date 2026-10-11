// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/changedsince"
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
//   - A window whose since generation or current active generation is a delta
//     generation returns Unavailable=true with UnavailableReason
//     baseline_not_comparable and no diff (#7282). A delta generation holds
//     only the changed files' facts plus tombstones, so a raw fact-set diff
//     against it reports every key it did not re-emit as removed or added. A
//     delta generation strictly between two full endpoints does not trigger
//     the refusal: both endpoints are complete snapshots.
//
// The counts are exact; only the per-classification sample lists are capped at
// the normalized SampleLimit, with a Truncated flag when more keys matched.
func (s StatusStore) ComputeChangedSinceDelta(
	ctx context.Context,
	filter changedsince.Filter,
) (changedsince.Summary, error) {
	if s.queryer == nil {
		return changedsince.Summary{}, fmt.Errorf("queryer is required")
	}

	filter = filter.Normalize()
	if !filter.HasScopeSelector() {
		return changedsince.Summary{}, fmt.Errorf("scope_id or repository is required")
	}
	if filter.HasConflictingScopeSelectors() {
		return changedsince.Summary{}, fmt.Errorf("scope_id and repository are mutually exclusive")
	}

	scope, ok, err := s.resolveChangedSinceScope(ctx, filter)
	if err != nil {
		return changedsince.Summary{}, err
	}
	if !ok {
		// Unknown scope/repository: empty ScopeID signals not-found upstream.
		return changedsince.Summary{}, nil
	}
	repository := ""
	if scope.scopeKind == "repository" {
		repository = scope.repository
	}

	summary := changedsince.Summary{
		ScopeID:                   scope.scopeID,
		ScopeKind:                 scope.scopeKind,
		Repository:                repository,
		CurrentActiveGenerationID: scope.currentGenerationID,
		CurrentObservedAt:         changedsince.Timestamp(scope.currentObservedAt),
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
		return changedsince.Summary{}, err
	}
	if !priorOK {
		expired, expiredObservedAt, expiredErr := s.changedSinceRetentionExpired(ctx, scope.scopeID, filter)
		if expiredErr != nil {
			return changedsince.Summary{}, expiredErr
		}
		if expired {
			summary.Unavailable = true
			summary.UnavailableReason = changedsince.UnavailableRetentionExpired
			summary.SinceGenerationID = filter.SinceGenerationID
			summary.SinceObservedAt = changedsince.Timestamp(expiredObservedAt)
			summary.Categories = unavailableChangedSinceCategories()
			return summary, nil
		}
		// The since reference matched no generation for this scope.
		return summary, nil
	}
	summary.SinceGenerationID = prior.generationID
	summary.SinceObservedAt = changedsince.Timestamp(prior.observedAt)

	if prior.isDelta || scope.currentIsDelta {
		summary.Unavailable = true
		summary.UnavailableReason = changedsince.UnavailableBaselineNotComparable
		summary.SinceIsDelta = prior.isDelta
		summary.CurrentIsDelta = scope.currentIsDelta
		summary.Categories = unavailableChangedSinceCategories()
		return summary, nil
	}

	categories, err := s.changedSinceCategories(
		ctx,
		scope.scopeID,
		prior.generationID,
		scope.currentGenerationID,
		filter.SampleLimit,
	)
	if err != nil {
		return changedsince.Summary{}, err
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
	currentIsDelta      bool
	hasPending          bool
}

func (s StatusStore) resolveChangedSinceScope(
	ctx context.Context,
	filter changedsince.Filter,
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
		&scope.currentIsDelta,
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
	isDelta      bool
}

func (s StatusStore) resolveChangedSincePriorGeneration(
	ctx context.Context,
	scopeID string,
	filter changedsince.Filter,
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
	if err := rows.Scan(&prior.generationID, &observed, &prior.isDelta); err != nil {
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
	filter changedsince.Filter,
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
) ([]changedsince.CategoryDelta, error) {
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

	counts := map[changedsince.Category]changedsince.Counts{}
	samples := map[changedsince.Category]map[changedsince.Classification][]changedsince.Sample{}
	for rows.Next() {
		var category, classification string
		var keyCount int64
		var stableFactKey, factKind sql.NullString
		if err := rows.Scan(&category, &classification, &keyCount, &stableFactKey, &factKind); err != nil {
			return nil, fmt.Errorf("changed-since diff: %w", err)
		}
		cat := changedsince.Category(category)
		class := changedsince.Classification(classification)
		bucket := counts[cat]
		applyChangedSinceCount(&bucket, classification, int(keyCount))
		counts[cat] = bucket
		if !stableFactKey.Valid {
			continue
		}
		if samples[cat] == nil {
			samples[cat] = map[changedsince.Classification][]changedsince.Sample{}
		}
		samples[cat][class] = append(samples[cat][class], changedsince.Sample{
			StableFactKey: stableFactKey.String,
			FactKind:      factKind.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("changed-since diff: %w", err)
	}

	categories := make([]changedsince.CategoryDelta, 0, len(changedsince.Categories))
	for _, category := range changedsince.Categories {
		delta := changedsince.CategoryDelta{Category: category, Counts: counts[category]}
		truncated := map[changedsince.Classification]bool{}
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

func applyChangedSinceCount(bucket *changedsince.Counts, classification string, value int) {
	switch changedsince.Classification(classification) {
	case changedsince.Added:
		bucket.Added = value
	case changedsince.Updated:
		bucket.Updated = value
	case changedsince.Unchanged:
		bucket.Unchanged = value
	case changedsince.Retired:
		bucket.Retired = value
	case changedsince.Superseded:
		bucket.Superseded = value
	}
}

func changedSinceClassificationCount(counts changedsince.Counts, classification changedsince.Classification) int {
	switch classification {
	case changedsince.Added:
		return counts.Added
	case changedsince.Updated:
		return counts.Updated
	case changedsince.Unchanged:
		return counts.Unchanged
	case changedsince.Retired:
		return counts.Retired
	case changedsince.Superseded:
		return counts.Superseded
	default:
		return 0
	}
}

func unavailableChangedSinceCategories() []changedsince.CategoryDelta {
	categories := make([]changedsince.CategoryDelta, 0, len(changedsince.Categories))
	for _, category := range changedsince.Categories {
		categories = append(categories, changedsince.CategoryDelta{
			Category:    category,
			Unavailable: true,
		})
	}
	return categories
}
