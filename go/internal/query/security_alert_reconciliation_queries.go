// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// SQL for the reducer-owned security-alert reconciliation list read model. The
// security_alert_current CTE keeps the latest comparison row per
// provider/alert/repository/package/advisory key, applies the requested
// repository scope ($2) and the scoped-token grant set ($11), and only then
// ranks, filters, and pages so a scoped caller never observes or paginates
// reconciliation rows outside its granted repositories.
const listSecurityAlertReconciliationsQuery = `
WITH security_alert_current AS (
  SELECT
      fact.fact_id,
      fact.source_confidence,
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
  WHERE fact.fact_kind = $1
    AND fact.is_tombstone = FALSE
    AND generation.status = 'active'
    AND (
      cardinality($2::text[]) = 0
      OR fact.payload->>'repository_id' = ANY($2::text[])
      OR fact.payload->>'provider_repository_id' = ANY($2::text[])
      OR fact.payload->>'scope_id' = ANY($2::text[])
    )
    AND ($3 = '' OR fact.payload->>'provider' = $3)
    AND ($4 = '' OR fact.payload->>'package_id' = $4)
    AND ($5 = '' OR fact.payload->'cve_ids' ? $5)
    AND ($6 = '' OR fact.payload->'ghsa_ids' ? $6)
    AND (
      cardinality($11::text[]) = 0
      OR fact.payload->>'repository_id' = ANY($11::text[])
      OR fact.payload->>'provider_repository_id' = ANY($11::text[])
      OR fact.payload->>'scope_id' = ANY($11::text[])
    )
)
SELECT current_fact.fact_id, current_fact.source_confidence, current_fact.payload
FROM security_alert_current AS current_fact
WHERE current_fact.security_alert_current_rank = 1
  AND ($7 = '' OR current_fact.payload->>'provider_state' = $7)
  AND ($8 = '' OR current_fact.payload->>'reconciliation_status' = $8)
  AND ($9 = '' OR current_fact.fact_id > $9)
ORDER BY current_fact.fact_id ASC
LIMIT $10
`
