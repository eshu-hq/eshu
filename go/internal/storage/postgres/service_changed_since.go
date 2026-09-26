// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/changedsince"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// ComputeServiceChangedSinceDelta computes one bounded service-scope
// changed-since summary (#1943). It resolves the service and its current active
// materialization generation, resolves the prior generation named by
// SinceGenerationID, then diffs the two evidence snapshot sets keyed by
// (generation_id, service_evidence_key) into per-family, per-classification
// counts and bounded sample handles. It reuses the repository-scope classification
// verbatim; only the lineage table and key space differ.
//
// The caller's grant (filter.Scoped, AllowedRepositoryIDs, AllowedScopeIDs)
// binds in the resolve SQL on each lineage row's scope_id (#6475), so the
// lineages this method can see are exactly the ones the caller may read.
//
// Resolution failures are explicit, never confident emptiness:
//
//   - An unknown service, or one whose every lineage lies outside the grant,
//     returns an empty ServiceID so the handler emits service_not_found. The
//     second case also sets OutsideGrant, for the handler span only.
//   - More than one admitted attributed lineage with no filter.ScopeID returns
//     AmbiguousScopeIDs and no diff; the reader never picks one silently. An
//     unattributed legacy lineage is served only to an unscoped caller and
//     only when no attributed lineage has an active generation: the writer
//     never supersedes it, so beside an active attributed lineage it is stale
//     by construction.
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

	lineages, err := s.resolveServiceChangedSinceLineages(ctx, filter)
	if err != nil {
		return statuspkg.ServiceChangedSinceSummary{}, err
	}
	scope, ambiguous, ok := selectServiceChangedSinceLineage(filter, lineages)
	if len(ambiguous) > 0 {
		truncated := len(ambiguous) > changedsince.MaxServiceScopeCandidates
		if truncated {
			ambiguous = ambiguous[:changedsince.MaxServiceScopeCandidates]
		}
		return statuspkg.ServiceChangedSinceSummary{
			ServiceID:          filter.ServiceID,
			SampleLimit:        filter.SampleLimit,
			AmbiguousScopeIDs:  ambiguous,
			AmbiguousTruncated: truncated,
		}, nil
	}
	if !ok {
		// Unknown or ungranted service: empty ServiceID signals not-found
		// upstream, byte-identically for both.
		summary := statuspkg.ServiceChangedSinceSummary{}
		if filter.Scoped {
			summary.OutsideGrant, err = s.serviceChangedSinceLineageExists(ctx, filter.ServiceID)
			if err != nil {
				return statuspkg.ServiceChangedSinceSummary{}, err
			}
		}
		return summary, nil
	}

	summary := statuspkg.ServiceChangedSinceSummary{
		ServiceID:                 filter.ServiceID,
		ScopeID:                   scope.scopeID,
		Unattributed:              scope.unattributed,
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

	prior, priorOK, err := s.resolveServiceChangedSincePriorGeneration(ctx, filter.ServiceID, scope, filter.SinceGenerationID)
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

// serviceChangedSinceLineage is one lineage the resolve query admitted: a
// scope's generation chain for the service, or the unattributed legacy chain.
type serviceChangedSinceLineage struct {
	scopeID             string
	unattributed        bool
	currentGenerationID string
	currentObservedAt   time.Time
	hasPending          bool
}

// selectServiceChangedSinceLineage picks the lineage a diff reads from the
// admitted rows, or reports the admitted scope ids when the choice is the
// caller's to make. It returns ok=false when nothing the caller may read
// matched.
//
// Attributed lineages with an active generation decide first: exactly one is
// served, and more than one is ambiguous unless the caller named a scope (the
// SQL then returned at most that one). The unattributed legacy lineage is
// served only when no attributed ACTIVE lineage is visible, and never to a
// scoped caller or past an explicit scope selector -- the SQL already
// excludes it for both, and the check here keeps that true if the SQL ever
// regresses. Only when neither side has an active generation does an
// attributed lineage with no active generation answer, as an unavailable diff.
func selectServiceChangedSinceLineage(
	filter statuspkg.ServiceChangedSinceFilter,
	lineages []serviceChangedSinceLineage,
) (serviceChangedSinceLineage, []string, bool) {
	var attributed, active []serviceChangedSinceLineage
	var legacy *serviceChangedSinceLineage
	for i := range lineages {
		if lineages[i].unattributed {
			legacy = &lineages[i]
			continue
		}
		attributed = append(attributed, lineages[i])
		if lineages[i].currentGenerationID != "" {
			active = append(active, lineages[i])
		}
	}
	legacyEligible := legacy != nil && !filter.Scoped && filter.ScopeID == ""
	switch {
	case len(active) > 0:
		return pickServiceChangedSinceLineage(active)
	case legacyEligible && legacy.currentGenerationID != "":
		return *legacy, nil, true
	case len(attributed) > 0:
		return pickServiceChangedSinceLineage(attributed)
	case legacyEligible:
		return *legacy, nil, true
	default:
		return serviceChangedSinceLineage{}, nil, false
	}
}

// pickServiceChangedSinceLineage serves the only candidate, or reports every
// candidate's scope id, sorted, when there is more than one.
func pickServiceChangedSinceLineage(
	candidates []serviceChangedSinceLineage,
) (serviceChangedSinceLineage, []string, bool) {
	if len(candidates) == 1 {
		return candidates[0], nil, true
	}
	ids := make([]string, 0, len(candidates))
	for _, lineage := range candidates {
		ids = append(ids, lineage.scopeID)
	}
	sort.Strings(ids)
	return serviceChangedSinceLineage{}, ids, false
}

func (s StatusStore) resolveServiceChangedSinceLineages(
	ctx context.Context,
	filter statuspkg.ServiceChangedSinceFilter,
) ([]serviceChangedSinceLineage, error) {
	rows, err := s.queryer.QueryContext(
		ctx,
		resolveServiceChangedSinceScopeQuery,
		filter.ServiceID,
		filter.ScopeID,
		filter.Scoped,
		array.Of(filter.AllowedRepositoryIDs),
		array.Of(filter.AllowedScopeIDs),
		// One past the candidate bound reports truncation of the attributed
		// list; one more keeps the unattributed row (sorted last) visible when
		// there are few attributed rows.
		changedsince.MaxServiceScopeCandidates+2,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve service changed-since scope: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var lineages []serviceChangedSinceLineage
	for rows.Next() {
		var lineage serviceChangedSinceLineage
		var currentObserved sql.NullTime
		if err := rows.Scan(
			&lineage.scopeID,
			&lineage.unattributed,
			&lineage.currentGenerationID,
			&currentObserved,
			&lineage.hasPending,
		); err != nil {
			return nil, fmt.Errorf("resolve service changed-since scope: %w", err)
		}
		if currentObserved.Valid {
			lineage.currentObservedAt = currentObserved.Time
		}
		lineages = append(lineages, lineage)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve service changed-since scope: %w", err)
	}
	return lineages, nil
}

func (s StatusStore) serviceChangedSinceLineageExists(ctx context.Context, serviceID string) (bool, error) {
	rows, err := s.queryer.QueryContext(ctx, serviceChangedSinceLineageExistsQuery, serviceID)
	if err != nil {
		return false, fmt.Errorf("probe service changed-since lineage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	exists := false
	if rows.Next() {
		if err := rows.Scan(&exists); err != nil {
			return false, fmt.Errorf("probe service changed-since lineage: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("probe service changed-since lineage: %w", err)
	}
	return exists, nil
}

type serviceChangedSincePrior struct {
	generationID string
	observedAt   time.Time
}

func (s StatusStore) resolveServiceChangedSincePriorGeneration(
	ctx context.Context,
	serviceID string,
	lineage serviceChangedSinceLineage,
	sinceGenerationID string,
) (serviceChangedSincePrior, bool, error) {
	// The unattributed lineage binds as SQL NULL so the IS NOT DISTINCT FROM
	// predicate matches only other unattributed rows.
	lineageScope := sql.NullString{String: lineage.scopeID, Valid: !lineage.unattributed}
	rows, err := s.queryer.QueryContext(
		ctx,
		resolveServiceChangedSincePriorGenerationQuery,
		serviceID,
		sinceGenerationID,
		lineageScope,
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
