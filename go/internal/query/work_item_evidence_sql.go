// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// listWorkItemEvidenceQuery reads one bounded page of active work-item source
// facts. The scoped-token grant set ($9) intersects each fact's durable
// linked_repository_id before ORDER BY/LIMIT: an empty grant array (shared,
// admin, or local callers) keeps the unscoped all-rows branch, while a non-empty
// array bounds the page so a scoped caller observes only work items whose durable
// repository link is granted. A fact with no linked_repository_id (every fact
// kind except a canonicalized external_link) fails the ANY() match and stays
// invisible to scoped tokens — fail-closed by construction, never an
// all-or-nothing provider-scope leak.
const listWorkItemEvidenceQuery = `
SELECT
    fact.fact_id,
    fact.fact_kind,
    fact.scope_id,
    fact.generation_id,
    fact.source_confidence,
    fact.observed_at,
    fact.schema_version,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = ANY($1::text[])
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($2 = '' OR fact.scope_id = $2)
  AND ($3 = '' OR fact.payload->>'work_item_key' = $3)
  AND ($4 = '' OR fact.payload->>'provider_work_item_id' = $4)
  AND ($5 = '' OR fact.payload->>'project_key' = $5)
  AND ($6 = '' OR fact.payload->>'url_fingerprint' = $6 OR fact.payload->>'source_url_fingerprint' = $6)
  AND ($7::timestamptz IS NULL OR fact.observed_at >= $7)
  AND ($8 = '' OR fact.fact_id > $8)
  AND (
    cardinality($9::text[]) = 0
    OR fact.payload->>'linked_repository_id' = ANY($9::text[])
  )
ORDER BY fact.fact_id ASC
LIMIT $10
`
