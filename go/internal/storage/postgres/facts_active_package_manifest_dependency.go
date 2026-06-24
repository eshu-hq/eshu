// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const listActivePackageManifestDependencyFactsQuery = `
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
WHERE fact.fact_kind = 'content_entity'
  AND fact.source_system = 'git'
  AND fact.payload->>'entity_type' = 'Variable'
  AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
  AND fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])
  AND fact.payload->>'entity_name' = ANY($2::text[])
  AND generation.status = 'active'
  AND fact.is_tombstone = FALSE
  AND (
    $3::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $5
`

// ListActivePackageManifestDependencyFacts loads active, non-tombstoned Git
// manifest dependency entities for the specific ecosystem/name set requested by
// a package-registry reducer intent.
//
// The query filters fact.is_tombstone = FALSE so a manifest dependency fact
// tombstoned within a still-active generation is never surfaced as live. The
// active generation pointer keeps tombstoned rows joinable, so the predicate is
// the only guard against returning superseded dependency entities to
// correlation consumers; the package-source and security-alert reconciliation
// handlers do not filter IsTombstone themselves. This matches every sibling
// active source-local reader.
//
// No-Regression Evidence: predicate-only narrowing of an existing WHERE clause
// on the active-generation read. The partial
// fact_records_active_package_dependency_entity_idx index does not cover
// is_tombstone, so the planner applies the new predicate as a residual filter
// on rows the index scan already visits; the index choice, the
// active-generation join, the keyset pagination cursor, ordering, and the scan
// shape are unchanged.
// go test ./internal/storage/postgres ./internal/reducer -count=1.
// No-Observability-Change: reuses the existing FactStore query path and its
// established storage instrumentation; no new query, span, or metric added.
func (s FactStore) ListActivePackageManifestDependencyFacts(
	ctx context.Context,
	ecosystems []string,
	packageNames []string,
) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	if len(ecosystems) == 0 || len(packageNames) == 0 {
		return nil, nil
	}

	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listActivePackageManifestDependencyFactsPage(
			ctx,
			ecosystems,
			packageNames,
			cursorObservedAt,
			cursorFactID,
		)
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

func (s FactStore) listActivePackageManifestDependencyFactsPage(
	ctx context.Context,
	ecosystems []string,
	packageNames []string,
	cursorObservedAt *time.Time,
	cursorFactID string,
) ([]facts.Envelope, error) {
	var cursor any
	if cursorObservedAt != nil {
		cursor = cursorObservedAt.UTC()
	}

	rows, err := s.db.QueryContext(
		ctx,
		listActivePackageManifestDependencyFactsQuery,
		ecosystems,
		packageNames,
		cursor,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active package manifest dependency facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active package manifest dependency facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active package manifest dependency facts: %w", err)
	}
	return loaded, nil
}
