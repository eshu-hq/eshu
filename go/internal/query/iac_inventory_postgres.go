// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type iacInventoryQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresIaCInventoryStore reads active-generation IaC identities and facets.
type PostgresIaCInventoryStore struct {
	db iacInventoryQueryer
}

// NewPostgresIaCInventoryStore constructs an active IaC inventory reader.
func NewPostgresIaCInventoryStore(db iacInventoryQueryer) PostgresIaCInventoryStore {
	return PostgresIaCInventoryStore{db: db}
}

const iacInventoryAuthorizationSQL = `
  AND (
    NOT $1::boolean
    OR fact.payload->>'repo_id' = ANY($2::text[])
    OR fact.scope_id = ANY($3::text[])
  )`

const iacCurrentInventoryCTE = `
WITH current_iac AS MATERIALIZED (
  SELECT DISTINCT ON (fact.payload->>'entity_id')
    fact.payload->>'entity_id' AS entity_id,
    fact.payload->>'entity_name' AS entity_name,
    fact.payload->>'entity_type' AS entity_type,
		fact.generation_id AS generation_id,
    fact.payload->>'relative_path' AS relative_path,
    fact.payload->>'repo_id' AS repo_id,
    COALESCE(
      fact.payload->'entity_metadata'->>'resource_type',
      fact.payload->'entity_metadata'->>'data_type',
      ''
    ) AS item_type,
    COALESCE(fact.payload->'entity_metadata'->>'provider', '') AS provider,
    CASE
      WHEN fact.payload->>'entity_type' = 'TerraformModule'
        THEN fact.payload->>'entity_name'
      WHEN fact.payload->>'entity_name' LIKE 'module."%'
        THEN split_part(fact.payload->>'entity_name', '"', 2)
      WHEN fact.payload->>'entity_name' LIKE 'module.%'
        THEN split_part(split_part(substr(fact.payload->>'entity_name', 8), '.', 1), '[', 1)
      ELSE ''
    END AS module_name,
    generation.ingested_at
  FROM scope_generations AS generation
  JOIN fact_records AS fact
    ON fact.scope_id = generation.scope_id
   AND fact.generation_id = generation.generation_id
  WHERE generation.status = 'active'
    AND fact.fact_kind = 'content_entity'
    AND fact.is_tombstone = FALSE
    AND fact.payload->>'entity_type' IN (
      'TerraformResource',
      'TerraformModule',
      'TerraformDataSource'
    )` + iacInventoryAuthorizationSQL + `
  ORDER BY
    fact.payload->>'entity_id',
    generation.ingested_at DESC,
    fact.fact_id DESC
)`

const iacInventorySearchSQL = iacCurrentInventoryCTE + `
SELECT current_iac.entity_id, current_iac.entity_name, current_iac.generation_id
FROM current_iac
WHERE current_iac.entity_type = $4
  AND (
    $6::text = ''
    OR strpos(lower(current_iac.entity_name), lower($6)) > 0
    OR strpos(lower(current_iac.relative_path), lower($6)) > 0
    OR strpos(lower(current_iac.item_type), lower($6)) > 0
    OR strpos(lower(current_iac.provider), lower($6)) > 0
    OR strpos(lower(current_iac.module_name), lower($6)) > 0
    OR strpos(lower(current_iac.repo_id), lower($6)) > 0
    OR strpos(lower(current_iac.entity_type), lower($6)) > 0
    OR strpos(lower($5), lower($6)) > 0
  )
  AND ($7::text = '' OR current_iac.item_type = $7)
  AND ($8::text = '' OR current_iac.provider = $8)
  AND ($9::text = '' OR current_iac.module_name = $9)
  AND ($10::text = '' OR current_iac.repo_id = $10)
  AND (
    ($11::text = '' AND $12::text = '')
    OR current_iac.entity_name > $11
    OR (current_iac.entity_name = $11 AND current_iac.entity_id > $12)
  )
ORDER BY current_iac.entity_name, current_iac.entity_id
LIMIT $13`

const iacInventorySummarySQL = iacCurrentInventoryCTE + `,
facet_counts AS (
  SELECT 'kind' AS dimension,
         CASE entity_type
           WHEN 'TerraformResource' THEN 'resource'
           WHEN 'TerraformModule' THEN 'module'
           ELSE 'data-source'
         END AS kind,
         CASE entity_type
           WHEN 'TerraformResource' THEN 'resource'
           WHEN 'TerraformModule' THEN 'module'
           ELSE 'data-source'
         END AS value,
         count(*)::bigint AS item_count
  FROM current_iac
  GROUP BY entity_type
  UNION ALL
  SELECT 'type',
         CASE entity_type WHEN 'TerraformResource' THEN 'resource' WHEN 'TerraformModule' THEN 'module' ELSE 'data-source' END,
         item_type,
         count(*)::bigint
  FROM current_iac
  WHERE item_type <> ''
  GROUP BY entity_type, item_type
  UNION ALL
  SELECT 'provider',
         CASE entity_type WHEN 'TerraformResource' THEN 'resource' WHEN 'TerraformModule' THEN 'module' ELSE 'data-source' END,
         provider,
         count(*)::bigint
  FROM current_iac
  WHERE provider <> ''
  GROUP BY entity_type, provider
  UNION ALL
  SELECT 'module', '', module_name, count(*)::bigint
  FROM current_iac
  WHERE module_name <> ''
  GROUP BY module_name
  UNION ALL
  SELECT 'repository', '', repo_id, count(*)::bigint
  FROM current_iac
  WHERE repo_id <> ''
  GROUP BY repo_id
),
ranked_facets AS (
  SELECT dimension, kind, value, item_count,
         ROW_NUMBER() OVER (PARTITION BY dimension, kind ORDER BY item_count DESC, value) AS facet_rank
  FROM facet_counts
)
SELECT dimension, kind, value, item_count, facet_rank
FROM ranked_facets
WHERE dimension IN ('kind', 'type', 'provider', 'module', 'repository')
  AND facet_rank <= $4 + 1
ORDER BY dimension, facet_rank`

// SearchActive returns a bounded, stable page of current candidate identities.
func (s PostgresIaCInventoryStore) SearchActive(
	ctx context.Context,
	search iacInventorySearch,
	access repositoryAccessFilter,
) ([]iacInventoryCandidate, error) {
	if s.db == nil {
		return nil, fmt.Errorf("IaC inventory database is required")
	}
	label, ok := iacResourceKindLabels[search.Kind]
	if !ok {
		return nil, fmt.Errorf("unknown IaC inventory kind %q", search.Kind)
	}
	rows, err := s.db.QueryContext(
		ctx,
		iacInventorySearchSQL,
		access.scoped(),
		access.grantedRepositoryIDs(),
		access.grantedScopeIDs(),
		label,
		string(search.Kind),
		strings.TrimSpace(search.Query),
		strings.TrimSpace(search.Type),
		strings.TrimSpace(search.Provider),
		strings.TrimSpace(search.Module),
		strings.TrimSpace(search.Repository),
		strings.TrimSpace(search.AfterName),
		strings.TrimSpace(search.AfterID),
		search.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search active IaC inventory: %w", err)
	}
	defer func() { _ = rows.Close() }()

	candidates := make([]iacInventoryCandidate, 0, search.Limit)
	for rows.Next() {
		var candidate iacInventoryCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Name, &candidate.GenerationID); err != nil {
			return nil, fmt.Errorf("scan active IaC candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search active IaC inventory: %w", err)
	}
	return candidates, nil
}

// Summary returns bounded authoritative totals and selector facets.
func (s PostgresIaCInventoryStore) Summary(
	ctx context.Context,
	access repositoryAccessFilter,
	limit int,
) (iacInventorySummary, error) {
	summary := newIaCInventorySummary(limit)
	if s.db == nil {
		return summary, fmt.Errorf("IaC inventory database is required")
	}
	rows, err := s.db.QueryContext(
		ctx,
		iacInventorySummarySQL,
		access.scoped(),
		access.grantedRepositoryIDs(),
		access.grantedScopeIDs(),
		limit,
	)
	if err != nil {
		return summary, fmt.Errorf("summarize active IaC inventory: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var dimension, rawKind, value string
		var count, rank int
		if err := rows.Scan(&dimension, &rawKind, &value, &count, &rank); err != nil {
			return summary, fmt.Errorf("scan active IaC facet: %w", err)
		}
		kind := iacResourceKind(rawKind)
		if dimension == "kind" {
			summary.ByKind[kind] = count
			summary.Total += count
			continue
		}
		if rank > limit {
			summary.Truncated[iacFacetTruncationKey(dimension)] = true
			continue
		}
		facet := iacInventoryFacet{Kind: kind, Value: value, Count: count}
		switch dimension {
		case "type":
			summary.Types = append(summary.Types, facet)
		case "provider":
			summary.Providers = append(summary.Providers, facet)
		case "module":
			summary.Modules = append(summary.Modules, facet)
		case "repository":
			summary.Repositories = append(summary.Repositories, facet)
		}
	}
	if err := rows.Err(); err != nil {
		return summary, fmt.Errorf("summarize active IaC inventory: %w", err)
	}
	return summary, nil
}

func iacFacetTruncationKey(dimension string) string {
	if dimension == "repository" {
		return "repositories"
	}
	return dimension + "s"
}
