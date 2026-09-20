// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

type inventoryQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresIaCInventoryStore reads active-generation IaC identities and facets.
type PostgresIaCInventoryStore struct {
	db inventoryQueryer
}

// NewPostgresIaCInventoryStore constructs an active IaC inventory reader.
func NewPostgresIaCInventoryStore(db inventoryQueryer) PostgresIaCInventoryStore {
	return PostgresIaCInventoryStore{db: db}
}

// iacReadModelLabels is the closed label set the infra read model mirrors
// for the IaC list route: every resourceKindLabels value. Scoped callers
// always stay on the active-inventory CTE: the table's scope_id is derive
// provenance the backfill records as "", so scope-granted callers cannot be
// bounded from the table the way the CTE bounds them through fact scope_id.
var iacReadModelLabels = []string{
	resourceKindLabels[resourceKindResource],
	resourceKindLabels[resourceKindModule],
	resourceKindLabels[resourceKindDataSource],
}

// inventoryDBAdapter lets the read-model inventory package query through the
// store's inventoryQueryer (*sql.DB, *sql.Conn, *sql.Tx, or a test double:
// *sql.Rows already satisfies db.Rows, so only the signature adapts).
type inventoryDBAdapter struct {
	query inventoryQueryer
}

func (a inventoryDBAdapter) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return a.query.QueryContext(ctx, query, args...)
}

// readModelServes reports whether the unscoped read-model path serves this
// caller. Scoped callers always stay on the graph-backed CTE; otherwise the
// table serves once its backfill marker exists and no repository waits for a
// fence repair. A failed readiness check fails the read rather than silently
// switching stores, matching the infra aggregate policy.
func (s PostgresIaCInventoryStore) readModelServes(
	ctx context.Context,
	access querycontract.RepositoryAccessFilter,
) (bool, error) {
	if s.db == nil || access.Scoped() {
		return false, nil
	}
	// Fail fast on a dead caller context before touching the connection: a
	// readiness probe issued under cancellation kills a dedicated pgx
	// connection ("driver: bad connection"), breaking every later read on
	// it. The CTE path never probes, so guard here to keep cancelled reads
	// behaving exactly as they did before the table path existed.
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("IaC inventory read model readiness: %w", err)
	}
	ready, err := inventory.ReadModelReady(ctx, inventoryDBAdapter{query: s.db})
	if err != nil {
		return false, fmt.Errorf("check IaC inventory read model readiness: %w", err)
	}
	return ready, nil
}

const inventoryAuthorizationSQL = `
  AND (
    NOT $1::boolean
    OR fact.payload->>'repo_id' = ANY($2::text[])
    OR fact.scope_id = ANY($3::text[])
  )`

const currentInventoryCTE = `
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
    )` + inventoryAuthorizationSQL + `
  ORDER BY
    fact.payload->>'entity_id',
    generation.ingested_at DESC,
    fact.fact_id DESC
)`

const inventorySearchSQL = currentInventoryCTE + `
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

const inventorySummarySQL = currentInventoryCTE + `,
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
	search InventorySearch,
	access querycontract.RepositoryAccessFilter,
) ([]InventoryCandidate, error) {
	if s.db == nil {
		return nil, fmt.Errorf("IaC inventory database is required")
	}
	label, ok := resourceKindLabels[search.Kind]
	if !ok {
		return nil, fmt.Errorf("unknown IaC inventory kind %q", search.Kind)
	}
	if useTable, err := s.readModelServes(ctx, access); err != nil {
		return nil, err
	} else if useTable {
		return s.searchTable(ctx, search, label)
	}
	rows, err := s.db.QueryContext(
		ctx,
		inventorySearchSQL,
		access.Scoped(),
		access.GrantedRepositoryIDs(),
		access.GrantedScopeIDs(),
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

	candidates := make([]InventoryCandidate, 0, search.Limit)
	for rows.Next() {
		var candidate InventoryCandidate
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

// searchTable serves one unscoped page from infra_resource_entities. The
// kind label is already resolved by SearchActive; the remaining search fields
// pass through unchanged, and the keyset cursor keeps its (name, id) order.
func (s PostgresIaCInventoryStore) searchTable(
	ctx context.Context,
	search InventorySearch,
	label string,
) ([]InventoryCandidate, error) {
	tableCandidates, err := inventory.SearchIaCEntities(ctx, inventoryDBAdapter{query: s.db}, inventory.IaCSearch{
		Label:      label,
		Kind:       string(search.Kind),
		Query:      search.Query,
		ItemType:   search.Type,
		Provider:   search.Provider,
		Module:     search.Module,
		Repository: search.Repository,
		AfterName:  search.AfterName,
		AfterID:    search.AfterID,
		Limit:      search.Limit,
	})
	if err != nil {
		return nil, err
	}
	candidates := make([]InventoryCandidate, 0, len(tableCandidates))
	for _, candidate := range tableCandidates {
		candidates = append(candidates, InventoryCandidate{
			ID:           candidate.ID,
			Name:         candidate.Name,
			GenerationID: candidate.GenerationID,
		})
	}
	return candidates, nil
}

// Summary returns bounded authoritative totals and selector facets.
func (s PostgresIaCInventoryStore) Summary(
	ctx context.Context,
	access querycontract.RepositoryAccessFilter,
	limit int,
) (InventorySummary, error) {
	summary := newIaCInventorySummary(limit)
	if s.db == nil {
		return summary, fmt.Errorf("IaC inventory database is required")
	}
	if useTable, err := s.readModelServes(ctx, access); err != nil {
		return summary, err
	} else if useTable {
		return s.summarizeTable(ctx, summary, limit)
	}
	rows, err := s.db.QueryContext(
		ctx,
		inventorySummarySQL,
		access.Scoped(),
		access.GrantedRepositoryIDs(),
		access.GrantedScopeIDs(),
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
		applyInventoryFacetRow(&summary, dimension, rawKind, value, count, rank, limit)
	}
	if err := rows.Err(); err != nil {
		return summary, fmt.Errorf("summarize active IaC inventory: %w", err)
	}
	return summary, nil
}

// summarizeTable serves the unscoped summary from infra_resource_entities.
// Row for row it applies the same facet mapping as the CTE path, so totals
// and facets agree whenever the table mirrors current content.
func (s PostgresIaCInventoryStore) summarizeTable(
	ctx context.Context,
	summary InventorySummary,
	limit int,
) (InventorySummary, error) {
	tableRows, err := inventory.SummarizeIaCEntities(ctx, inventoryDBAdapter{query: s.db}, iacReadModelLabels, limit)
	if err != nil {
		return summary, err
	}
	for _, row := range tableRows {
		applyInventoryFacetRow(&summary, row.Dimension, row.Kind, row.Value, row.Count, row.Rank, limit)
	}
	return summary, nil
}

// applyInventoryFacetRow folds one ranked facet row into the summary: kind
// rows set totals, rows past the limit mark their facet truncated, and the
// rest append to their dimension bucket in rank order.
func applyInventoryFacetRow(summary *InventorySummary, dimension, rawKind, value string, count, rank, limit int) {
	kind := resourceKind(rawKind)
	if dimension == "kind" {
		summary.ByKind[kind] = count
		summary.Total += count
		return
	}
	if rank > limit {
		summary.Truncated[facetTruncationKey(dimension)] = true
		return
	}
	facet := InventoryFacet{Kind: kind, Value: value, Count: count}
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

func facetTruncationKey(dimension string) string {
	if dimension == "repository" {
		return "repositories"
	}
	return dimension + "s"
}
