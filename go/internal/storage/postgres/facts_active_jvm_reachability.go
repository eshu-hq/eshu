// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const listActiveJVMReachabilityFactsQuery = `
WITH api_packages AS (
	SELECT LOWER(BTRIM(api_package)) AS api_package
	FROM UNNEST($2::text[]) AS api_package(api_package)
	WHERE BTRIM(api_package) <> ''
)
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
WHERE fact.fact_kind = 'file'
  AND fact.source_system = 'git'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'repo_id' = ANY($1::text[])
  AND (
      LOWER(COALESCE(fact.payload->'parsed_file_data'->>'lang', '')) IN ('java', 'kotlin', 'scala')
      OR fact.payload->>'relative_path' LIKE '%.java'
      OR fact.payload->>'relative_path' LIKE '%.kt'
      OR fact.payload->>'relative_path' LIKE '%.kts'
      OR fact.payload->>'relative_path' LIKE '%.scala'
      OR fact.payload->>'relative_path' LIKE '%.sc'
  )
  AND EXISTS (
      SELECT 1
      FROM api_packages
      WHERE EXISTS (
          SELECT 1
          FROM jsonb_array_elements(
              CASE
                WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'imports') = 'array'
                THEN fact.payload->'parsed_file_data'->'imports'
                ELSE '[]'::jsonb
              END
          ) AS item(value)
          CROSS JOIN LATERAL (
              VALUES
                (LOWER(REPLACE(REPLACE(REPLACE(COALESCE(item.value->>'source', ''), '/', '.'), '#', '.'), ' ', '.'))),
                (LOWER(REPLACE(REPLACE(REPLACE(COALESCE(item.value->>'name', ''), '/', '.'), '#', '.'), ' ', '.'))),
                (LOWER(REPLACE(REPLACE(REPLACE(COALESCE(item.value->>'full_import_name', ''), '/', '.'), '#', '.'), ' ', '.')))
          ) AS extracted(jvm_reachability_value)
          WHERE jvm_reachability_value = api_package
             OR jvm_reachability_value LIKE api_package || '.%'
             OR jvm_reachability_value LIKE '%.' || api_package || '.%'
      )
      OR EXISTS (
          SELECT 1
          FROM jsonb_array_elements(
              CASE
                WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'function_calls') = 'array'
                THEN fact.payload->'parsed_file_data'->'function_calls'
                ELSE '[]'::jsonb
              END
          ) AS item(value)
          CROSS JOIN LATERAL (
              VALUES
                (LOWER(REPLACE(REPLACE(REPLACE(COALESCE(item.value->>'full_name', ''), '/', '.'), '#', '.'), ' ', '.'))),
                (LOWER(REPLACE(REPLACE(REPLACE(COALESCE(item.value->>'inferred_obj_type', ''), '/', '.'), '#', '.'), ' ', '.'))),
                (LOWER(REPLACE(REPLACE(REPLACE(COALESCE(item.value->>'name', ''), '/', '.'), '#', '.'), ' ', '.')))
          ) AS extracted(jvm_reachability_value)
          WHERE jvm_reachability_value = api_package
             OR jvm_reachability_value LIKE api_package || '.%'
             OR jvm_reachability_value LIKE '%.' || api_package || '.%'
      )
      OR EXISTS (
          SELECT 1
          FROM jsonb_array_elements(
              CASE
                WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'function_calls_scip') = 'array'
                THEN fact.payload->'parsed_file_data'->'function_calls_scip'
                ELSE '[]'::jsonb
              END
          ) AS item(value)
          CROSS JOIN LATERAL (
              VALUES
                (LOWER(REPLACE(REPLACE(REPLACE(COALESCE(item.value->>'callee_symbol', ''), '/', '.'), '#', '.'), ' ', '.'))),
                (LOWER(REPLACE(REPLACE(REPLACE(COALESCE(item.value->>'callee_name', ''), '/', '.'), '#', '.'), ' ', '.')))
          ) AS extracted(jvm_reachability_value)
          WHERE jvm_reachability_value = api_package
             OR jvm_reachability_value LIKE api_package || '.%'
             OR jvm_reachability_value LIKE '%.' || api_package || '.%'
      )
  )
  AND (
    $3::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $5
`

// ListActiveJVMReachabilityFacts loads active Java/Kotlin/Scala parser and
// SCIP file facts for repositories with Maven or Gradle package evidence. The
// caller owns API-prefix matching so repository file facts cannot by themselves
// claim vulnerability reachability.
func (s FactStore) ListActiveJVMReachabilityFacts(
	ctx context.Context,
	filter reducer.JVMReachabilityFactFilter,
) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	filter.RepositoryIDs = cleanStringFilterValues(filter.RepositoryIDs)
	filter.APIPackages = cleanStringFilterValues(filter.APIPackages)
	if len(filter.RepositoryIDs) == 0 || len(filter.APIPackages) == 0 {
		return nil, nil
	}

	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listActiveJVMReachabilityFactsPage(
			ctx,
			filter.RepositoryIDs,
			filter.APIPackages,
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

func (s FactStore) listActiveJVMReachabilityFactsPage(
	ctx context.Context,
	repositoryIDs []string,
	apiPackages []string,
	cursorObservedAt *time.Time,
	cursorFactID string,
) ([]facts.Envelope, error) {
	var cursor any
	if cursorObservedAt != nil {
		cursor = cursorObservedAt.UTC()
	}

	rows, err := s.db.QueryContext(
		ctx,
		listActiveJVMReachabilityFactsQuery,
		repositoryIDs,
		apiPackages,
		cursor,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active JVM reachability facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active JVM reachability facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active JVM reachability facts: %w", err)
	}
	return loaded, nil
}
