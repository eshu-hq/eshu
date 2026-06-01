package query

const listWorkItemEvidenceQuery = `
SELECT
    fact.fact_id,
    fact.fact_kind,
    fact.scope_id,
    fact.generation_id,
    fact.source_confidence,
    fact.observed_at,
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
ORDER BY fact.fact_id ASC
LIMIT $9
`
