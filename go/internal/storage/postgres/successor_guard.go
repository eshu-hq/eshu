// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// successorGuardCountQuery sizes one generation for the #7926
// successor-emptiness backstop in a single index-only pass over
// fact_records_scope_generation_idx (#8750): the rows a collector stream can
// replace, and every row. Reducer domains write reducer_-prefixed facts into
// the source generation after it activates; no collector stream carries them,
// so counting them inflated the prior and refused ordinary successors.
const successorGuardCountQuery = `
SELECT COUNT(*) FILTER (WHERE ` + linksfreshnessstore.ExcludeReducerDerivedKinds + `),
       COUNT(*)
FROM fact_records
WHERE scope_id = $1
  AND generation_id = $2
`

// guardFactCounts is one generation's size as the backstop sees it.
type guardFactCounts struct {
	// Collector counts the rows a collector stream emits: every row except
	// reducer-derived ones.
	Collector int
	// Total counts every row, reducer-derived ones included.
	Total int
}

func (c guardFactCounts) add(other guardFactCounts) guardFactCounts {
	return guardFactCounts{Collector: c.Collector + other.Collector, Total: c.Total + other.Total}
}

// countGuardFacts sizes one generation for the backstop. A delta generation
// with a projected base reports base plus own rows, the same upper bound
// CountFacts reports; a delta whose base is missing returns
// projector.DeltaBaseMissingError so the caller can size it on its own rows
// (#8362).
func (s FactStore) countGuardFacts(ctx context.Context, scopeID, generationID string) (guardFactCounts, error) {
	if s.database == nil {
		return guardFactCounts{}, fmt.Errorf("fact store database is required")
	}
	baseGenerationID, _, wantUnion, err := s.resolveDeltaUnionBase(ctx, scopeID, generationID)
	if err != nil {
		return guardFactCounts{}, err
	}
	own, err := s.countGuardFactsGeneration(ctx, scopeID, generationID)
	if err != nil || !wantUnion {
		return own, err
	}
	base, err := s.countGuardFactsGeneration(ctx, scopeID, baseGenerationID)
	if err != nil {
		return guardFactCounts{}, err
	}
	return base.add(own), nil
}

// countGuardFactsGeneration runs successorGuardCountQuery for one
// (scope, generation), with no delta-union resolution.
func (s FactStore) countGuardFactsGeneration(ctx context.Context, scopeID, generationID string) (guardFactCounts, error) {
	rows, err := s.database.QueryContext(ctx, successorGuardCountQuery, scopeID, generationID)
	if err != nil {
		return guardFactCounts{}, fmt.Errorf("count guard facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var counts guardFactCounts
	if rows.Next() {
		if err := rows.Scan(&counts.Collector, &counts.Total); err != nil {
			return guardFactCounts{}, fmt.Errorf("count guard facts scan: %w", err)
		}
	}
	return counts, rows.Err()
}

// successorPrior carries the candidate past the duplicate check so the
// guard compares without re-reading. counts are the candidate's guard
// counts; the predicate compares counts.Collector with the successor.
// orphanFallback (#8362): counts holds only the candidate's own rows; an
// empty successor still refuses.
type successorPrior struct {
	candidate      backfill.UnchangedGenerationCandidate
	found          bool
	counts         guardFactCounts
	orphanFallback bool
}

// sizing names how counts was derived, for the refusal log.
func (p successorPrior) sizing() string {
	if p.orphanFallback {
		return "own_rows_base_missing"
	}
	return "candidate"
}

// successorPriorForGuard reads the guard counts on the base connection
// before the commit transaction opens. Missing-base (#8362) falls back to
// the candidate's own rows; other failures fail closed.
func (s IngestionStore) successorPriorForGuard(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	candidate backfill.UnchangedGenerationCandidate,
	found bool,
	generation scope.ScopeGeneration,
) (successorPrior, error) {
	prior := successorPrior{candidate: candidate, found: found}
	if !found || generation.TrustedDeletion {
		return prior, nil
	}
	store := NewFactStore(s.database)
	counts, err := store.countGuardFacts(ctx, scopeValue.ScopeID, candidate.GenerationID)
	if err != nil {
		var baseMissing *projector.DeltaBaseMissingError
		if errors.As(err, &baseMissing) {
			// The prior is a delta with no base full generation: the union
			// size is unknowable, so size the guard on the candidate's own
			// fact_records rows instead. A fallback failure fails closed.
			fallback, fallbackErr := store.countGuardFactsGeneration(ctx, scopeValue.ScopeID, candidate.GenerationID)
			if fallbackErr != nil {
				return successorPrior{}, fmt.Errorf("count prior generation facts fallback for scope %s: %w", scopeValue.ScopeID, fallbackErr)
			}
			s.logCommitStage(ctx, scopeValue, generation, "successor_prior_orphan_fallback", time.Now(),
				slog.String("candidate_generation_id", candidate.GenerationID),
				slog.Int("prior_fact_count_fallback", fallback.Collector))
			return successorPrior{candidate: candidate, found: found, counts: fallback, orphanFallback: true}, nil
		}
		return successorPrior{}, fmt.Errorf("count prior generation facts for scope %s: %w", scopeValue.ScopeID, err)
	}
	prior.counts = counts
	return prior, nil
}
