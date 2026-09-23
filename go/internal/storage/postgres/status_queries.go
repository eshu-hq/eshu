// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
)

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
	producerActivityQuery = `
SELECT TRUE AS has_active_or_pending_generation,
       GREATEST(
         EXTRACT(
           EPOCH FROM (
             $1 - GREATEST(
               observed_at,
               ingested_at,
               COALESCE(activated_at, observed_at)
             )
           )
         ),
         0
       ) AS latest_generation_age_seconds
FROM scope_generations
WHERE status IN ('pending', 'active')
ORDER BY GREATEST(observed_at, ingested_at, COALESCE(activated_at, observed_at)) DESC
LIMIT 1
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
	// stageCountsSelect counts active work items by stage and status.
	stageCountsSelect = `SELECT stage, status, COUNT(*) AS count
FROM active_fact_work_items
GROUP BY stage, status`
	// stageCountsOrder is stageCountsSelect's result order.
	stageCountsOrder = `stage, status`
	// domainBacklogCTEs builds the per-domain fact backlog and the
	// shared-projection backlog; it needs active_fact_work_items earlier in the
	// same WITH list.
	domainBacklogCTEs = `fact_domain_backlogs AS (
  SELECT domain,
         COUNT(*) FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying')) AS outstanding_count,
         COUNT(*) FILTER (WHERE status IN ('claimed', 'running')) AS in_flight_count,
         COUNT(*) FILTER (WHERE status = 'retrying') AS retrying_count,
         COUNT(*) FILTER (WHERE status = 'dead_letter') AS dead_letter_count,
         COUNT(*) FILTER (WHERE status = 'failed') AS failed_count,
         GREATEST(
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
           ),
           0
         ) AS oldest_outstanding_age_seconds
  FROM active_fact_work_items
  WHERE status IN ('pending', 'claimed', 'running', 'retrying', 'dead_letter', 'failed')
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
-- Only pending intents or a live lease can produce a shared projection
-- backlog row, so read pending intents once instead of joining every intent
-- row (completed rows accumulate into the hundreds of thousands; #6794).
shared_projection_pending AS (
  SELECT projection_domain AS domain,
         COUNT(*) AS outstanding_count,
         MIN(created_at) AS oldest_created_at
  FROM shared_projection_intents
  WHERE completed_at IS NULL
  GROUP BY projection_domain
),
shared_projection_domain_backlogs AS (
  SELECT COALESCE(pending.domain, active.domain) AS domain,
         -- A live-lease domain with no pending intent has zero outstanding
         -- intents. The pre-#6794 LEFT JOIN counted the null-extended row of
         -- a lease-only domain as one phantom pending intent.
         COALESCE(pending.outstanding_count, 0) AS outstanding_count,
         COALESCE(active.in_flight_count, 0) AS in_flight_count,
         0::BIGINT AS retrying_count,
         0::BIGINT AS dead_letter_count,
         0::BIGINT AS failed_count,
         GREATEST(
           COALESCE(EXTRACT(EPOCH FROM ($1 - pending.oldest_created_at)), 0),
           0
         ) AS oldest_outstanding_age_seconds
  FROM shared_projection_pending AS pending
  FULL OUTER JOIN shared_projection_active_leases AS active
    ON active.domain = pending.domain
)`
	// domainBacklogSelect merges the fact and shared-projection backlogs.
	domainBacklogSelect = `SELECT domain,
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
HAVING SUM(outstanding_count) + SUM(in_flight_count) + SUM(retrying_count) + SUM(dead_letter_count) + SUM(failed_count) > 0`
	// domainBacklogOrder is domainBacklogSelect's result order.
	domainBacklogOrder = `outstanding_count DESC, oldest_outstanding_age_seconds DESC, domain ASC`
	// queueSnapshotSelect aggregates the active work-item queue into one row.
	queueSnapshotSelect = `SELECT (SELECT COUNT(*) FROM fact_work_items) AS total_count,
       COUNT(*) FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying')) AS outstanding_count,
       COUNT(*) FILTER (WHERE status = 'pending') AS pending_count,
       COUNT(*) FILTER (WHERE status IN ('claimed', 'running')) AS in_flight_count,
       COUNT(*) FILTER (WHERE status = 'retrying') AS retrying_count,
       COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded_count,
       COUNT(*) FILTER (WHERE status = 'dead_letter') AS dead_letter_count,
       COUNT(*) FILTER (WHERE status = 'failed') AS failed_count,
	   EXISTS (
	     SELECT 1
	     FROM cross_scope_completion_upgrade_markers
	     WHERE marker_name = 'provenance_edge_identity_upgrade_096'
	   ) AS provenance_edge_identity_upgrade_applied,
	   COUNT(*) FILTER (
	     WHERE provenance_edge_identity_upgrade_required
	       AND stage = 'reducer'
	       AND domain IN ('package_source_correlation', 'container_image_identity')
	       AND status IN ('pending', 'claimed', 'running', 'retrying')
	   ) AS provenance_edge_identity_upgrade_required,
       GREATEST(
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
         ),
         0
       ) AS oldest_outstanding_age_seconds,
       COUNT(*) FILTER (
         WHERE status IN ('claimed', 'running')
           AND claim_until IS NOT NULL
           AND claim_until < $1
       ) AS overdue_claim_count
FROM active_fact_work_items`
)

const statusReadinessSchemaQuery = `
WITH required_status_schema AS (
  SELECT scopes.scope_id, work_items.status,
         work_items.provenance_edge_identity_upgrade_required
  FROM ingestion_scopes AS scopes
  CROSS JOIN fact_work_items AS work_items
  LIMIT 0
)
SELECT EXISTS (
  SELECT 1 FROM eshu_schema_migrations
  WHERE path = $1
    AND variant = 'full'
    AND checksum_sha256 = $2
) AND NOT EXISTS (SELECT 1 FROM required_status_schema)
`

type statusReadinessMigration struct {
	path     string
	checksum string
}

var latestStatusReadinessMigration = func() statusReadinessMigration {
	// The ordered bootstrap writes a receipt only after each file succeeds.
	// Its latest full receipt proves that earlier required files completed,
	// including the permitted deferred content-index variants.
	definitions := BootstrapDefinitions()
	latest := definitions[len(definitions)-1]
	return statusReadinessMigration{path: latest.Path, checksum: migrationChecksum(latest.SQL)}
}()

// CheckStatusReadiness verifies the latest Postgres migration receipt and the
// core status schema with one bounded read. It does not aggregate queue or fact
// tables; those remain on the full /admin/status and /metrics surfaces.
func (s StatusStore) CheckStatusReadiness(ctx context.Context) error {
	if s.queryer == nil {
		return errors.New("status queryer is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rows, err := s.queryer.QueryContext(ctx, statusReadinessSchemaQuery,
		latestStatusReadinessMigration.path, latestStatusReadinessMigration.checksum)
	if err != nil {
		return fmt.Errorf("check core status schema: %w", err)
	}
	if rows == nil {
		return errors.New("check core status schema: no result")
	}
	hasRow := rows.Next()
	var applied bool
	var scanErr error
	if hasRow {
		scanErr = rows.Scan(&applied)
	}
	rowErr := rows.Err()
	closeErr := rows.Close()
	if scanErr != nil {
		return fmt.Errorf("scan core status schema check: %w", scanErr)
	}
	if rowErr != nil {
		return fmt.Errorf("read core status schema check: %w", rowErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close core status schema check: %w", closeErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !hasRow {
		return errors.New("check core status schema: no migration receipt result")
	}
	if !applied {
		return fmt.Errorf("current Postgres schema migration %q is not recorded", latestStatusReadinessMigration.path)
	}
	return nil
}
