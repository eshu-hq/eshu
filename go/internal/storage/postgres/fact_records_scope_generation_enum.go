// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// listScopeGenerationWorkQuery enumerates every distinct (scope_id,
// generation_id) pair recorded in fact_records and hydrates each into the exact
// 16-column projection scanProjectorWork consumes, so the B-12 determinism
// re-drain (issue #5008) reuses the same scanner the projector claim path uses.
//
// The projection mirrors the tail of claimProjectorWorkQuery (see
// projector_queue_claim_sql.go) column-for-column, with two deliberate
// substitutions for a re-drain rather than a live claim:
//   - attempt_count is the literal 0 — a corpus re-drain has no lease/retry
//     history, so there is nothing to carry.
//   - the driving set is `SELECT DISTINCT scope_id, generation_id FROM
//     fact_records` rather than the fact_work_items claim CTE, because the
//     re-drain reconstructs the graph from the persisted facts, not from queue
//     state.
//
// ORDER BY (scope_id, generation_id) makes the enumeration deterministic so a
// re-drain at N=1 and a re-drain at N=4 are handed byte-identical work
// sequences; the determinism assertion downstream compares the resulting
// canonical graphs across N. Full Scope/Generation hydration is load-bearing:
// concurrentreplay.FactSliceSource.Next feeds the whole Scope and Generation
// into collector.FactsFromSlice, so an IDs-only enumeration would re-drain a
// graph stripped of scope/generation metadata.
const listScopeGenerationWorkQuery = `
WITH corpus_generations AS (
    SELECT DISTINCT scope_id, generation_id
    FROM fact_records
)
SELECT
    scope.scope_id,
    scope.source_system,
    scope.scope_kind,
    COALESCE(scope.parent_scope_id, ''),
    COALESCE(scope.active_generation_id, ''),
    EXISTS (
        SELECT 1
        FROM scope_generations AS prior_generation
        WHERE prior_generation.scope_id = scope.scope_id
          AND prior_generation.generation_id <> corpus.generation_id
    ),
    scope.collector_kind,
    scope.partition_key,
    generation.generation_id,
    0,
    generation.observed_at,
    generation.ingested_at,
    generation.status,
    generation.trigger_kind,
    COALESCE(generation.freshness_hint, ''),
    COALESCE(scope.payload, '{}'::jsonb)
FROM corpus_generations AS corpus
JOIN ingestion_scopes AS scope
  ON scope.scope_id = corpus.scope_id
JOIN scope_generations AS generation
  ON generation.generation_id = corpus.generation_id
ORDER BY corpus.scope_id, corpus.generation_id
`

// ListScopeGenerationWork returns one fully hydrated
// projector.ScopeGenerationWork for every distinct scope generation present in
// fact_records, ordered deterministically by (scope_id, generation_id). It is
// the enumeration half of the B-12 determinism composition (issue #5008): the
// returned slice seeds concurrentreplay.FactSliceSource, which re-drains each
// generation's recorded facts into a fresh graph database at a configured
// worker count N so the canonical graph can be compared across N.
func (s FactStore) ListScopeGenerationWork(ctx context.Context) ([]projector.ScopeGenerationWork, error) {
	if s.db == nil {
		return nil, fmt.Errorf("list scope generation work: fact store database is required")
	}

	rows, err := s.db.QueryContext(ctx, listScopeGenerationWorkQuery)
	if err != nil {
		return nil, fmt.Errorf("list scope generation work: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var works []projector.ScopeGenerationWork
	for rows.Next() {
		work, scanErr := scanProjectorWork(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list scope generation work: %w", scanErr)
		}
		works = append(works, work)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list scope generation work: %w", err)
	}

	return works, nil
}
