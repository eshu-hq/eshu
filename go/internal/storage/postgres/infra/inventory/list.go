// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// IaCSearch narrows one bounded IaC resource list page over
// infra_resource_entities. Label is the single Terraform graph label to read
// (TerraformResource, TerraformModule, or TerraformDataSource); every other
// field mirrors InventorySearch in the query layer's iac package, whose
// Postgres store translates its closed kind enum to Label before calling
// here. An empty Query, ItemType, Provider, Module, or Repository disables
// that predicate. AfterName/AfterID resume the (entity_name, entity_id)
// keyset; both empty starts from the first row.
type IaCSearch struct {
	Label      string
	Kind       string
	Query      string
	ItemType   string
	Provider   string
	Module     string
	Repository string
	AfterName  string
	AfterID    string
	Limit      int
}

// IaCCandidate is one current identity on the page. GenerationID is the
// row's derive provenance, which the backfill records as "": callers must
// not treat it as the active generation, only as an opaque stamp that live
// derives overwrite with the projecting generation.
type IaCCandidate struct {
	ID           string
	Name         string
	GenerationID string
}

// IaCSummaryRow is one ranked facet row of the IaC inventory summary. Rank is
// 1-based within (Dimension, Kind); rows with Rank above the requested limit
// mark their facet truncated rather than surfacing.
type IaCSummaryRow struct {
	Dimension string
	Kind      string
	Value     string
	Count     int
	Rank      int
}

// iacModuleNameSQL derives the Terraform module instance name from an
// entity-name expression, mirroring the currentInventoryCTE module_name CASE
// in the query layer's iac package: the entity_name of a TerraformModule row
// is the module itself, otherwise a "module."-prefixed address yields its
// instance name (quoted or bare, without a for_each index), else "".
const iacModuleNameSQL = `CASE
      WHEN label = 'TerraformModule'
        THEN entity_name
      WHEN entity_name LIKE 'module."%'
        THEN split_part(entity_name, '"', 2)
      WHEN entity_name LIKE 'module.%'
        THEN split_part(split_part(substr(entity_name, 8), '.', 1), '[', 1)
      ELSE ''
    END`

// iacSearchSQL selects the bounded keyset page of current identities. Every
// predicate mirrors the active-inventory CTE clause for clause: the free-text
// query matches name, path, type, provider, module, repository, label, and
// the kind selector; the item type matches either type column (the #6817
// table precedent -- the CTE's COALESCE over the payload cannot distinguish
// a missing key from an empty one once both normalize to empty); the
// module and repository match exactly; pagination resumes after
// (AfterName, AfterID) in (entity_name, entity_id) order.
const iacSearchSQL = `
SELECT entity_id, entity_name, generation_id
FROM infra_resource_entities
WHERE label = $1
  AND (
    $3::text = ''
    OR strpos(lower(entity_name), lower($3)) > 0
    OR strpos(lower(relative_path), lower($3)) > 0
    OR strpos(lower(resource_type), lower($3)) > 0
    OR strpos(lower(data_type), lower($3)) > 0
    OR strpos(lower(provider), lower($3)) > 0
    OR strpos(lower(` + iacModuleNameSQL + `), lower($3)) > 0
    OR strpos(lower(repo_id), lower($3)) > 0
    OR strpos(lower(label), lower($3)) > 0
    OR strpos(lower($2), lower($3)) > 0
  )
  AND ($4::text = '' OR resource_type = $4 OR data_type = $4)
  AND ($5::text = '' OR provider = $5)
  AND ($6::text = '' OR (` + iacModuleNameSQL + `) = $6)
  AND ($7::text = '' OR repo_id = $7)
  AND (
    ($8::text = '' AND $9::text = '')
    OR entity_name > $8
    OR (entity_name = $8 AND entity_id > $9)
  )
ORDER BY entity_name, entity_id
LIMIT $10`

// iacSummarySQL counts the bounded selector facets over the three Terraform
// labels, mirroring the active-inventory CTE summary: totals by kind, then
// type, provider, module, and repository facets ranked by count, with one
// row past the limit so callers can mark truncation.
const iacSummarySQL = `
WITH current_iac AS MATERIALIZED (
  SELECT entity_id, entity_name, label, repo_id, resource_type, data_type,
    provider, ` + iacModuleNameSQL + ` AS module_name
  FROM infra_resource_entities
  WHERE label = ANY($1::text[])
),
facet_counts AS (
  SELECT 'kind' AS dimension,
         CASE label
           WHEN 'TerraformResource' THEN 'resource'
           WHEN 'TerraformModule' THEN 'module'
           ELSE 'data-source'
         END AS kind,
         CASE label
           WHEN 'TerraformResource' THEN 'resource'
           WHEN 'TerraformModule' THEN 'module'
           ELSE 'data-source'
         END AS value,
         count(*)::bigint AS item_count
  FROM current_iac
  GROUP BY label
  UNION ALL
  SELECT 'type',
         CASE label WHEN 'TerraformResource' THEN 'resource' WHEN 'TerraformModule' THEN 'module' ELSE 'data-source' END,
         resource_type,
         count(*)::bigint
  FROM current_iac
  WHERE resource_type <> ''
  GROUP BY label, resource_type
  UNION ALL
  SELECT 'type',
         CASE label WHEN 'TerraformResource' THEN 'resource' WHEN 'TerraformModule' THEN 'module' ELSE 'data-source' END,
         data_type,
         count(*)::bigint
  FROM current_iac
  WHERE data_type <> '' AND resource_type = ''
  GROUP BY label, data_type
  UNION ALL
  SELECT 'provider',
         CASE label WHEN 'TerraformResource' THEN 'resource' WHEN 'TerraformModule' THEN 'module' ELSE 'data-source' END,
         provider,
         count(*)::bigint
  FROM current_iac
  WHERE provider <> ''
  GROUP BY label, provider
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
  AND facet_rank <= $2 + 1
ORDER BY dimension, facet_rank`

// SearchIaCEntities returns the bounded keyset page of current identities
// for search. Callers must serve it only when ReadModelReady reports true;
// otherwise the table may cover only part of the corpus.
func SearchIaCEntities(ctx context.Context, queryer db.Queryer, search IaCSearch) ([]IaCCandidate, error) {
	if strings.TrimSpace(search.Label) == "" {
		return nil, fmt.Errorf("infra inventory IaC search label is required")
	}
	rows, err := queryer.QueryContext(
		ctx,
		iacSearchSQL,
		search.Label,
		search.Kind,
		strings.TrimSpace(search.Query),
		strings.TrimSpace(search.ItemType),
		strings.TrimSpace(search.Provider),
		strings.TrimSpace(search.Module),
		strings.TrimSpace(search.Repository),
		strings.TrimSpace(search.AfterName),
		strings.TrimSpace(search.AfterID),
		search.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search infra inventory IaC entities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	candidates := make([]IaCCandidate, 0, search.Limit)
	for rows.Next() {
		var candidate IaCCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Name, &candidate.GenerationID); err != nil {
			return nil, fmt.Errorf("scan infra inventory IaC candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search infra inventory IaC entities: %w", err)
	}
	return candidates, nil
}

// SummarizeIaCEntities returns the ranked selector facets over the labels.
// Callers must serve them only when ReadModelReady reports true.
func SummarizeIaCEntities(ctx context.Context, queryer db.Queryer, labels []string, limit int) ([]IaCSummaryRow, error) {
	if len(labels) == 0 {
		return nil, nil
	}
	rows, err := queryer.QueryContext(ctx, iacSummarySQL, labels, limit)
	if err != nil {
		return nil, fmt.Errorf("summarize infra inventory IaC entities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []IaCSummaryRow
	for rows.Next() {
		var row IaCSummaryRow
		var count int64
		var rank int64
		if err := rows.Scan(&row.Dimension, &row.Kind, &row.Value, &count, &rank); err != nil {
			return nil, fmt.Errorf("scan infra inventory IaC facet: %w", err)
		}
		row.Count = int(count)
		row.Rank = int(rank)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("summarize infra inventory IaC entities: %w", err)
	}
	return out, nil
}
