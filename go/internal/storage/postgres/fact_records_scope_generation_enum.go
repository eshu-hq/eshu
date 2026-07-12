// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// listScopeGenerationWorkQuery enumerates the ACTIVE generation of every scope
// that has facts in fact_records and hydrates each into the exact 16-column
// projection scanProjectorWork consumes, so the B-12 determinism re-drain (issue
// #5008) reuses the same scanner the projector claim path uses.
//
// Active generation, not every generation with lingering facts (codex #5136 P2):
// a scope's superseded historical generations keep their fact_records after the
// projector marks them superseded, so enumerating raw `DISTINCT scope_id,
// generation_id FROM fact_records` would re-drain those superseded generations.
// That is not just a fidelity problem — IngestionStore.CommitScopeGeneration
// rejects any generation whose status IsTerminal (superseded/completed/failed)
// with "must not be terminal before projection", so `ifa drive -from-facts`
// would abort at commit time on any multi-generation source. The query therefore
// drives off the shared latestGenerationCTE (the same
// COALESCE(active_generation_id, newest) selection every relationship-fact loader
// uses) and keeps only scopes whose latest generation is non-terminal and has
// facts. The explicit `status NOT IN (terminal)` filter is belt-and-suspenders:
// when a scope has no active pointer, the CTE falls back to the newest generation
// by ingested_at, which could itself be terminal — the filter drops that scope
// (nothing live to re-drain) rather than letting the commit path abort.
//
// The projection mirrors the tail of claimProjectorWorkQuery (see
// projector_queue_claim_sql.go) column-for-column, with attempt_count the literal
// 0 — a corpus re-drain has no lease/retry history to carry.
//
// ORDER BY (scope_id, generation_id) makes the enumeration deterministic so a
// re-drain at N=1 and a re-drain at N=4 are handed byte-identical work
// sequences; the determinism assertion downstream compares the resulting
// canonical graphs across N. Full Scope/Generation hydration is load-bearing:
// concurrentreplay.FactSliceSource.Next feeds the whole Scope and Generation
// into collector.FactsFromSlice, so an IDs-only enumeration would re-drain a
// graph stripped of scope/generation metadata.
const listScopeGenerationWorkQuery = latestGenerationCTE + `
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
FROM latest_generations AS corpus
JOIN ingestion_scopes AS scope
  ON scope.scope_id = corpus.scope_id
JOIN scope_generations AS generation
  ON generation.generation_id = corpus.generation_id
WHERE generation.status NOT IN ('superseded', 'completed', 'failed')
  AND EXISTS (
    SELECT 1
    FROM fact_records AS fr
    WHERE fr.scope_id = corpus.scope_id
      AND fr.generation_id = corpus.generation_id
)
ORDER BY corpus.scope_id, corpus.generation_id
`

// ListScopeGenerationWork returns one fully hydrated
// projector.ScopeGenerationWork for the active generation of every scope that
// has facts in fact_records, ordered deterministically by
// (scope_id, generation_id). Superseded historical generations whose facts still
// linger are skipped — only the active generation is projected, matching what the
// reducer materializes on a fresh ingest. It is the enumeration half of the B-12
// determinism composition (issue #5008): the returned slice seeds
// concurrentreplay.FactSliceSource, which re-drains each generation's recorded
// facts into a fresh graph database at a configured worker count N so the
// canonical graph can be compared across N.
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
