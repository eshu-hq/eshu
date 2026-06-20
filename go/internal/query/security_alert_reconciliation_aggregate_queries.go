package query

// SQL for the reducer-owned security-alert reconciliation aggregate read model.
// Each query keeps the latest comparison row per provider/alert/repository/
// package/advisory key, applies the requested repository scope ($1) and the
// scoped-token grant set ($8), and only then ranks, filters, groups, and pages
// so aggregate totals and inventory buckets never include reconciliation rows
// outside a scoped caller's granted repositories.

const securityAlertReconciliationAggregateRankingCTE = `
WITH security_alert_current AS (
  SELECT
      fact.payload,
      ROW_NUMBER() OVER (
        PARTITION BY
          COALESCE(NULLIF(fact.payload->>'provider', ''), 'unknown'),
          COALESCE(NULLIF(fact.payload->>'provider_alert_id', ''), NULLIF(fact.payload->>'provider_alert_number', ''), 'unknown'),
          COALESCE(NULLIF(fact.payload->>'provider_repository_id', ''), NULLIF(fact.payload->>'scope_id', ''), NULLIF(fact.payload->>'repository_id', ''), 'unknown'),
          COALESCE(NULLIF(fact.payload->>'package_id', ''), 'unknown'),
          COALESCE(NULLIF(fact.payload->'cve_ids', 'null'::jsonb), '[]'::jsonb),
          COALESCE(NULLIF(fact.payload->'ghsa_ids', 'null'::jsonb), '[]'::jsonb)
        ORDER BY
          fact.observed_at DESC,
          fact.ingested_at DESC,
          CASE fact.payload->>'reconciliation_status'
            WHEN 'stale' THEN 50
            WHEN 'matched' THEN 40
            WHEN 'unmatched' THEN 30
            WHEN 'fixed' THEN 20
            WHEN 'dismissed' THEN 20
            WHEN 'provider_only' THEN 0
            ELSE 10
          END DESC,
          fact.fact_id DESC
      ) AS security_alert_current_rank
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  WHERE fact.fact_kind = 'reducer_security_alert_reconciliation'
    AND fact.is_tombstone = FALSE
    AND generation.status = 'active'
    AND (
      COALESCE(cardinality($1::text[]), 0) = 0
      OR fact.payload->>'repository_id' = ANY($1::text[])
      OR fact.payload->>'provider_repository_id' = ANY($1::text[])
      OR fact.payload->>'scope_id' = ANY($1::text[])
    )
    AND ($2 = '' OR fact.payload->>'provider' = $2)
    AND ($3 = '' OR fact.payload->>'package_id' = $3)
    AND ($4 = '' OR fact.payload->'cve_ids' ? $4)
    AND ($5 = '' OR fact.payload->'ghsa_ids' ? $5)
    AND (
      COALESCE(cardinality($8::text[]), 0) = 0
      OR fact.payload->>'repository_id' = ANY($8::text[])
      OR fact.payload->>'provider_repository_id' = ANY($8::text[])
      OR fact.payload->>'scope_id' = ANY($8::text[])
    )
)
`

const securityAlertReconciliationAggregateTotalQuery = securityAlertReconciliationAggregateRankingCTE + `
SELECT COUNT(*) AS total
FROM security_alert_current AS current_fact
WHERE current_fact.security_alert_current_rank = 1
  AND ($6 = '' OR current_fact.payload->>'provider_state' = $6)
  AND ($7 = '' OR current_fact.payload->>'reconciliation_status' = $7);
`

const securityAlertReconciliationAggregateGroupQueryTemplate = securityAlertReconciliationAggregateRankingCTE + `
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM security_alert_current AS current_fact
WHERE current_fact.security_alert_current_rank = 1
  AND ($6 = '' OR current_fact.payload->>'provider_state' = $6)
  AND ($7 = '' OR current_fact.payload->>'reconciliation_status' = $7)
GROUP BY bucket;
`

const securityAlertReconciliationSourceFreshnessGroupExpr = `
COALESCE(
  NULLIF(current_fact.payload->>'source_freshness', ''),
  CASE
    WHEN current_fact.payload->>'collection_coverage_state' = 'incomplete' THEN 'partial'
    ELSE 'active'
  END
)
`

const securityAlertReconciliationInventoryQueryTemplate = securityAlertReconciliationAggregateRankingCTE + `
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM security_alert_current AS current_fact
WHERE current_fact.security_alert_current_rank = 1
  AND ($6 = '' OR current_fact.payload->>'provider_state' = $6)
  AND ($7 = '' OR current_fact.payload->>'reconciliation_status' = $7)
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $9 OFFSET $10;
`
