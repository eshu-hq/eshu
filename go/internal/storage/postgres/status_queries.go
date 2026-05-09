package postgres

const (
	scopeCountsQuery = `
SELECT status, COUNT(*) AS count
FROM ingestion_scopes
GROUP BY status
ORDER BY status
`
	generationCountsQuery = `
SELECT status, COUNT(*) AS count
FROM scope_generations
GROUP BY status
ORDER BY status
`
	generationTransitionsQuery = `
SELECT generation.scope_id,
       generation.generation_id,
       generation.status,
       generation.trigger_kind,
       COALESCE(generation.freshness_hint, '') AS freshness_hint,
       generation.observed_at,
       generation.activated_at,
       generation.superseded_at,
       COALESCE(scope.active_generation_id, '') AS current_active_generation_id
FROM scope_generations AS generation
JOIN ingestion_scopes AS scope
  ON scope.scope_id = generation.scope_id
WHERE generation.status IN ('active', 'superseded')
   OR generation.activated_at IS NOT NULL
   OR generation.superseded_at IS NOT NULL
ORDER BY COALESCE(generation.superseded_at, generation.activated_at, generation.ingested_at, generation.observed_at) DESC,
         generation.scope_id ASC,
         generation.generation_id ASC
LIMIT 5
`
	stageCountsQuery = `
SELECT stage, status, COUNT(*) AS count
FROM fact_work_items
GROUP BY stage, status
ORDER BY stage, status
`
	domainBacklogQuery = `
WITH fact_domain_backlogs AS (
  SELECT domain,
         COUNT(*) FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying')) AS outstanding_count,
         COUNT(*) FILTER (WHERE status IN ('claimed', 'running')) AS in_flight_count,
         COUNT(*) FILTER (WHERE status = 'retrying') AS retrying_count,
         COUNT(*) FILTER (WHERE status = 'dead_letter') AS dead_letter_count,
         COUNT(*) FILTER (WHERE status = 'failed') AS failed_count,
         COALESCE(
           EXTRACT(
             EPOCH FROM (
               $1 - (
                 MIN(created_at)
                   FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying'))
               )
             )
           ),
           0
         ) AS oldest_outstanding_age_seconds
  FROM fact_work_items
  GROUP BY domain
  HAVING COUNT(*) FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying', 'dead_letter', 'failed')) > 0
),
shared_projection_active_leases AS (
  SELECT projection_domain AS domain,
         COUNT(*) AS in_flight_count
  FROM shared_projection_partition_leases
  WHERE lease_owner IS NOT NULL
    AND lease_expires_at IS NOT NULL
    AND lease_expires_at > $1
  GROUP BY projection_domain
),
shared_projection_domains AS (
  SELECT projection_domain AS domain
  FROM shared_projection_intents
  UNION
  SELECT domain
  FROM shared_projection_active_leases
),
shared_projection_domain_backlogs AS (
  SELECT domains.domain AS domain,
         COUNT(*) FILTER (WHERE intents.completed_at IS NULL) AS outstanding_count,
         COALESCE(MAX(active.in_flight_count), 0) AS in_flight_count,
         0::BIGINT AS retrying_count,
         0::BIGINT AS dead_letter_count,
         0::BIGINT AS failed_count,
         COALESCE(
           EXTRACT(
             EPOCH FROM (
               $1 - (
                 MIN(intents.created_at)
                   FILTER (WHERE intents.completed_at IS NULL)
               )
             )
         ),
         0
       ) AS oldest_outstanding_age_seconds
  FROM shared_projection_domains AS domains
  LEFT JOIN shared_projection_intents AS intents
    ON intents.projection_domain = domains.domain
  LEFT JOIN shared_projection_active_leases AS active
    ON active.domain = domains.domain
  GROUP BY domains.domain
  HAVING COUNT(*) FILTER (WHERE intents.completed_at IS NULL) > 0
      OR COALESCE(MAX(active.in_flight_count), 0) > 0
)
SELECT domain,
       SUM(outstanding_count) AS outstanding_count,
       SUM(in_flight_count) AS in_flight_count,
       SUM(retrying_count) AS retrying_count,
       SUM(dead_letter_count) AS dead_letter_count,
       SUM(failed_count) AS failed_count,
       MAX(oldest_outstanding_age_seconds) AS oldest_outstanding_age_seconds
FROM (
  SELECT * FROM fact_domain_backlogs
  UNION ALL
  SELECT * FROM shared_projection_domain_backlogs
) AS domain_backlogs
GROUP BY domain
HAVING SUM(outstanding_count) + SUM(in_flight_count) + SUM(retrying_count) + SUM(dead_letter_count) + SUM(failed_count) > 0
ORDER BY outstanding_count DESC, oldest_outstanding_age_seconds DESC, domain ASC
`
	queueSnapshotQuery = `
SELECT COUNT(*) AS total_count,
       COUNT(*) FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying')) AS outstanding_count,
       COUNT(*) FILTER (WHERE status = 'pending') AS pending_count,
       COUNT(*) FILTER (WHERE status IN ('claimed', 'running')) AS in_flight_count,
       COUNT(*) FILTER (WHERE status = 'retrying') AS retrying_count,
       COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded_count,
       COUNT(*) FILTER (WHERE status = 'dead_letter') AS dead_letter_count,
       COUNT(*) FILTER (WHERE status = 'failed') AS failed_count,
       COALESCE(
         EXTRACT(
           EPOCH FROM (
             $1 - (
               MIN(created_at)
                 FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying'))
             )
           )
         ),
         0
       ) AS oldest_outstanding_age_seconds,
       COUNT(*) FILTER (
         WHERE status IN ('claimed', 'running')
           AND claim_until IS NOT NULL
           AND claim_until < $1
       ) AS overdue_claim_count
FROM fact_work_items
`
)
