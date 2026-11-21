// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// The first page and the cursor pages are separate statements on purpose,
// mirroring listFactsByKindQuery/listFactsByKindCursorQuery (facts_filtered.go
// #6154): folding both cases into one statement with `$1 IS NULL OR
// (observed_at, fact_id) > (...)` demotes the comparison to a filter under a
// generic plan -- where these statements land, because the services prepare
// them through database/sql -- and, with the generation-driven nested-loop
// plan the join form invites, every page rescans the corpus once per active
// generation (#11082).
//
// Both statements drive from fact_records through the partial index
// fact_records_package_ownership_active_idx (migration 327) inside a
// MATERIALIZED CTE with no inner LIMIT: the CTE holds every matching row past
// the cursor in (observed_at, fact_id) order, the outer join keeps the active
// generation's rows, and the outer LIMIT takes one page. MATERIALIZED is
// load-bearing, not decoration: without it Postgres flattens the CTE back
// into the generation-driven nested loop (60.7ms vs 7.5ms per page on the
// #11082 fixture) and the partial index goes unused. Omitting the inner LIMIT
// is a correctness requirement, not a tuning choice: the Go loop below stops
// on a short post-join page, so a pre-join LIMIT would silently drop every
// active row past the truncated pre-join set (#11082 arm C measured 204 of
// 1200 rows before stopping).
const listActivePackageOwnershipFactsFirstQuery = `
WITH matching AS MATERIALIZED (
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
        COALESCE(fact.source_uri, '') AS source_uri,
        COALESCE(fact.source_record_id, '') AS source_record_id,
        fact.observed_at,
        fact.is_tombstone,
        fact.payload
    FROM fact_records AS fact
    WHERE fact.fact_kind IN (
        'package_registry.package',
        'reducer_package_ownership_correlation',
        'reducer_package_publication_correlation'
      )
      AND fact.is_tombstone = FALSE
    ORDER BY fact.observed_at ASC, fact.fact_id ASC
)
SELECT
    matching.fact_id,
    matching.scope_id,
    matching.generation_id,
    matching.fact_kind,
    matching.stable_fact_key,
    matching.schema_version,
    matching.collector_kind,
    matching.fencing_token,
    matching.source_confidence,
    matching.source_system,
    matching.source_fact_key,
    matching.source_uri,
    matching.source_record_id,
    matching.observed_at,
    matching.is_tombstone,
    matching.payload
FROM matching
JOIN ingestion_scopes AS scope
  ON scope.scope_id = matching.scope_id
 AND scope.active_generation_id = matching.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = matching.scope_id
 AND generation.generation_id = matching.generation_id
WHERE generation.status = 'active'
ORDER BY matching.observed_at ASC, matching.fact_id ASC
LIMIT $1
`

const listActivePackageOwnershipFactsCursorQuery = `
WITH matching AS MATERIALIZED (
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
        COALESCE(fact.source_uri, '') AS source_uri,
        COALESCE(fact.source_record_id, '') AS source_record_id,
        fact.observed_at,
        fact.is_tombstone,
        fact.payload
    FROM fact_records AS fact
    WHERE fact.fact_kind IN (
        'package_registry.package',
        'reducer_package_ownership_correlation',
        'reducer_package_publication_correlation'
      )
      AND fact.is_tombstone = FALSE
      AND (fact.observed_at, fact.fact_id) > ($1::timestamptz, $2::text)
    ORDER BY fact.observed_at ASC, fact.fact_id ASC
)
SELECT
    matching.fact_id,
    matching.scope_id,
    matching.generation_id,
    matching.fact_kind,
    matching.stable_fact_key,
    matching.schema_version,
    matching.collector_kind,
    matching.fencing_token,
    matching.source_confidence,
    matching.source_system,
    matching.source_fact_key,
    matching.source_uri,
    matching.source_record_id,
    matching.observed_at,
    matching.is_tombstone,
    matching.payload
FROM matching
JOIN ingestion_scopes AS scope
  ON scope.scope_id = matching.scope_id
 AND scope.active_generation_id = matching.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = matching.scope_id
 AND generation.generation_id = matching.generation_id
WHERE generation.status = 'active'
  AND (matching.observed_at, matching.fact_id) > ($1::timestamptz, $2::text)
ORDER BY matching.observed_at ASC, matching.fact_id ASC
LIMIT $3
`

// ListActivePackageOwnershipFacts loads active, non-tombstoned package-registry
// identity facts plus the reducer-owned ownership and publication correlation
// facts across package-registry scopes. The code-import repo-edge reducer domain
// uses this as the bounded cross-scope owner catalog: it joins import
// coordinates to owning repositories through the same (ecosystem, name) key the
// package-consumption path uses, so a git-scope projection can resolve owners
// without scanning the file corpus.
//
// The query is bounded by the active package-registry generation (the three
// listed fact kinds only) and keyset-paginated on (observed_at, fact_id) exactly
// like the sibling active readers; it never touches `file` facts, so it does not
// regress the broad active-file-scan invariant.
//
// Observability Evidence: the code-import repo-edge completion log carries
// file-load vs ownership-load durations and the ownership row count
// (code_import_repo_edge_handler.go logCompleted), and the handler span
// carries the same three attributes, so the next ownership-load hang names
// its load without pg archaeology (#11082). No new metric family is added.
func (s FactStore) ListActivePackageOwnershipFacts(ctx context.Context) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listActivePackageOwnershipFactsPage(ctx, cursorObservedAt, cursorFactID)
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

func (s FactStore) listActivePackageOwnershipFactsPage(
	ctx context.Context,
	cursorObservedAt *time.Time,
	cursorFactID string,
) ([]facts.Envelope, error) {
	// Separate first-page and cursor statements keep the row comparison a
	// plain index qual under the generic plan database/sql prepares
	// (facts_filtered.go #6154); see the const comments for why the CTE must
	// stay MATERIALIZED with no inner LIMIT.
	query := listActivePackageOwnershipFactsFirstQuery
	args := []any{listFactsByKindPageSize}
	if cursorObservedAt != nil {
		query = listActivePackageOwnershipFactsCursorQuery
		args = []any{
			cursorObservedAt.UTC(),
			cursorFactID,
			listFactsByKindPageSize,
		}
	}

	rows, err := s.database.QueryContext(
		ctx,
		query,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list active package ownership facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := backfill.ScanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active package ownership facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active package ownership facts: %w", err)
	}

	return loaded, nil
}
