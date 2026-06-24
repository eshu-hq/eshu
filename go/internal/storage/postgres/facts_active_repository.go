// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const listActiveRepositoryFactsQuery = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'repository'
  AND fact.source_system = 'git'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
    $1::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($1::timestamptz, $2::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $3
`

// ListActiveRepositoryFacts loads active, non-tombstoned repository facts across
// Git-owned scopes. Reducer correlation domains use this as the cross-scope
// repository catalog instead of scanning source-local package-registry
// generations.
//
// The query filters fact.is_tombstone = FALSE so a repository fact tombstoned
// within a still-active generation is never surfaced as live. The active
// generation pointer keeps tombstoned rows joinable, so the predicate is the
// only guard against returning superseded repository identities to correlation
// consumers. This matches every sibling active source-local reader.
//
// No-Regression Evidence: predicate-only narrowing of an existing WHERE clause
// on the active-generation read. The partial fact_records_active_repository_idx
// (observed_at, fact_id, generation_id WHERE fact_kind='repository' AND
// source_system='git') does not cover is_tombstone, so the planner applies the
// new predicate as a residual filter on rows the index scan already visits; the
// index choice, the active-generation join, and the scan shape are unchanged.
// go test ./internal/storage/postgres ./internal/reducer -count=1.
// No-Observability-Change: reuses the existing FactStore query path and its
// established storage instrumentation; no new query, span, or metric added.
func (s FactStore) ListActiveRepositoryFacts(ctx context.Context) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listActiveRepositoryFactsPage(ctx, cursorObservedAt, cursorFactID)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, page...)
		if len(page) < listFactsByKindPageSize {
			return loaded, nil
		}

		last := page[len(page)-1]
		observedAt := last.ObservedAt.UTC()
		cursorObservedAt = &observedAt
		cursorFactID = last.FactID
	}
}

func (s FactStore) listActiveRepositoryFactsPage(
	ctx context.Context,
	cursorObservedAt *time.Time,
	cursorFactID string,
) ([]facts.Envelope, error) {
	var cursor any
	if cursorObservedAt != nil {
		cursor = cursorObservedAt.UTC()
	}

	rows, err := s.db.QueryContext(
		ctx,
		listActiveRepositoryFactsQuery,
		cursor,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active repository facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active repository facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active repository facts: %w", err)
	}

	return loaded, nil
}
