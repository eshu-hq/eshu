// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// Recompute write side of the #3389 canonical dedup materialization. See
// docs/internal/supply-chain-impact-canonical-dedup-materialization-design.md.
//
// IMPORTANT — read/write parity: the winner selection here MUST stay byte-for-byte
// equivalent to the read-time dedup in
// go/internal/query/supply_chain_impact_findings_queries.go. canonical_key, the
// public finding_id fallback, the has_payload_finding_id tiebreak, the
// severity_bucket CASE, and the suppression_state default are duplicated from
// that query on purpose (cross-package); the integration parity test
// (winners-recompute output filtered == legacy ROW_NUMBER read) is what guards
// against drift.

// supplyChainImpactWinnerSelectSQL projects exactly one winner row per
// canonical_key from the currently-active reducer_supply_chain_impact_finding
// facts, using the same ORDER BY (priority_score DESC, has_payload_finding_id
// DESC, fact_id ASC) the read-time dedup uses to pick canonical_rank = 1.
const supplyChainImpactWinnerSelectSQL = `
SELECT
    canonical_key,
    fact_id          AS winner_fact_id,
    scope_id         AS winner_scope_id,
    finding_id,
    priority_score,
    source_count,
    impact_status,
    ecosystem,
    severity_bucket,
    repository_id,
    cve_id,
    advisory_id,
    package_id,
    subject_digest,
    image_ref,
    priority_bucket,
    detection_profile,
    observed_version,
    match_reason,
    suppression_state,
    service_ids,
    workload_ids,
    environments
FROM (
    SELECT
        fact.fact_id,
        fact.scope_id,
        CONCAT_WS('|',
            COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
            COALESCE(fact.payload->>'advisory_id', ''),
            COALESCE(fact.payload->>'package_id', ''),
            COALESCE(fact.payload->>'purl', ''),
            COALESCE(fact.payload->>'product_criteria', ''),
            COALESCE(fact.payload->>'match_criteria_id', ''),
            COALESCE(fact.payload->>'observed_version', ''),
            COALESCE(fact.payload->>'requested_range', ''),
            COALESCE(fact.payload->>'impact_status', ''),
            COALESCE(fact.payload->>'repository_id', ''),
            COALESCE(fact.payload->>'subject_digest', '')
        ) AS canonical_key,
        COALESCE(
            NULLIF(fact.payload->>'finding_id', ''),
            CONCAT_WS('|',
                COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
                COALESCE(fact.payload->>'advisory_id', ''),
                COALESCE(fact.payload->>'package_id', ''),
                COALESCE(fact.payload->>'purl', ''),
                COALESCE(fact.payload->>'product_criteria', ''),
                COALESCE(fact.payload->>'match_criteria_id', ''),
                COALESCE(fact.payload->>'observed_version', ''),
                COALESCE(fact.payload->>'requested_range', ''),
                COALESCE(fact.payload->>'impact_status', ''),
                COALESCE(fact.payload->>'repository_id', ''),
                COALESCE(fact.payload->>'subject_digest', '')
            )
        ) AS finding_id,
        COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) AS priority_score,
        CASE WHEN NULLIF(fact.payload->>'finding_id', '') IS NULL THEN 0 ELSE 1 END AS has_payload_finding_id,
        COUNT(*) OVER (PARTITION BY
            CONCAT_WS('|',
                COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
                COALESCE(fact.payload->>'advisory_id', ''),
                COALESCE(fact.payload->>'package_id', ''),
                COALESCE(fact.payload->>'purl', ''),
                COALESCE(fact.payload->>'product_criteria', ''),
                COALESCE(fact.payload->>'match_criteria_id', ''),
                COALESCE(fact.payload->>'observed_version', ''),
                COALESCE(fact.payload->>'requested_range', ''),
                COALESCE(fact.payload->>'impact_status', ''),
                COALESCE(fact.payload->>'repository_id', ''),
                COALESCE(fact.payload->>'subject_digest', '')
            )
        ) AS source_count,
        COALESCE(fact.payload->>'impact_status', '') AS impact_status,
        COALESCE(fact.payload->>'ecosystem', '') AS ecosystem,
        CASE
            WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 9.0 THEN 'critical'
            WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 7.0 THEN 'high'
            WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 4.0 THEN 'medium'
            WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) > 0.0  THEN 'low'
            ELSE 'none'
        END AS severity_bucket,
        COALESCE(fact.payload->>'repository_id', '') AS repository_id,
        COALESCE(fact.payload->>'cve_id', '') AS cve_id,
        COALESCE(fact.payload->>'advisory_id', '') AS advisory_id,
        COALESCE(fact.payload->>'package_id', '') AS package_id,
        COALESCE(fact.payload->>'subject_digest', '') AS subject_digest,
        COALESCE(fact.payload->>'image_ref', '') AS image_ref,
        COALESCE(fact.payload->>'priority_bucket', '') AS priority_bucket,
        COALESCE(fact.payload->>'detection_profile', '') AS detection_profile,
        COALESCE(fact.payload->>'observed_version', '') AS observed_version,
        COALESCE(fact.payload->>'match_reason', '') AS match_reason,
        COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') AS suppression_state,
        COALESCE(fact.payload->'service_ids', '[]'::jsonb) AS service_ids,
        COALESCE(fact.payload->'workload_ids', '[]'::jsonb) AS workload_ids,
        COALESCE(fact.payload->'environments', '[]'::jsonb) AS environments,
        ROW_NUMBER() OVER (
            PARTITION BY CONCAT_WS('|',
                COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
                COALESCE(fact.payload->>'advisory_id', ''),
                COALESCE(fact.payload->>'package_id', ''),
                COALESCE(fact.payload->>'purl', ''),
                COALESCE(fact.payload->>'product_criteria', ''),
                COALESCE(fact.payload->>'match_criteria_id', ''),
                COALESCE(fact.payload->>'observed_version', ''),
                COALESCE(fact.payload->>'requested_range', ''),
                COALESCE(fact.payload->>'impact_status', ''),
                COALESCE(fact.payload->>'repository_id', ''),
                COALESCE(fact.payload->>'subject_digest', '')
            )
            ORDER BY
                COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) DESC,
                CASE WHEN NULLIF(fact.payload->>'finding_id', '') IS NULL THEN 0 ELSE 1 END DESC,
                fact.fact_id ASC
        ) AS canonical_rank
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
) ranked
WHERE ranked.canonical_rank = 1
`

// rebuildSupplyChainImpactWinnersSQL atomically reconciles the winners table to
// the current active set and stamps the maintainer watermark: upsert every
// current winner, delete winner rows whose canonical_key is no longer present in
// the active set, and upsert the singleton materialization watermark. All happen
// in one statement (one snapshot), so a concurrent reader never sees a torn
// rebuild. The winners upsert and delete are data-modifying CTEs (Postgres always
// runs them to completion regardless of whether the final statement reads them);
// the final watermark upsert is unconditional, so it stamps even a resweep that
// produced zero active winners — letting the read distinguish "never populated"
// (no watermark row) from "reswept to zero findings" (watermark present, table
// empty).
const rebuildSupplyChainImpactWinnersSQL = `
WITH winners_now AS (` + supplyChainImpactWinnerSelectSQL + `),
upserted AS (
    INSERT INTO supply_chain_impact_canonical_winners (
        canonical_key, winner_fact_id, winner_scope_id, finding_id, priority_score,
        source_count, impact_status, ecosystem, severity_bucket, repository_id,
        cve_id, advisory_id, package_id, subject_digest, image_ref, priority_bucket,
        detection_profile, observed_version, match_reason, suppression_state,
        service_ids, workload_ids, environments, materialized_at
    )
    SELECT
        canonical_key, winner_fact_id, winner_scope_id, finding_id, priority_score,
        source_count, impact_status, ecosystem, severity_bucket, repository_id,
        cve_id, advisory_id, package_id, subject_digest, image_ref, priority_bucket,
        detection_profile, observed_version, match_reason, suppression_state,
        service_ids, workload_ids, environments, $1
    FROM winners_now
    ON CONFLICT (canonical_key) DO UPDATE SET
        winner_fact_id = EXCLUDED.winner_fact_id,
        winner_scope_id = EXCLUDED.winner_scope_id,
        finding_id = EXCLUDED.finding_id,
        priority_score = EXCLUDED.priority_score,
        source_count = EXCLUDED.source_count,
        impact_status = EXCLUDED.impact_status,
        ecosystem = EXCLUDED.ecosystem,
        severity_bucket = EXCLUDED.severity_bucket,
        repository_id = EXCLUDED.repository_id,
        cve_id = EXCLUDED.cve_id,
        advisory_id = EXCLUDED.advisory_id,
        package_id = EXCLUDED.package_id,
        subject_digest = EXCLUDED.subject_digest,
        image_ref = EXCLUDED.image_ref,
        priority_bucket = EXCLUDED.priority_bucket,
        detection_profile = EXCLUDED.detection_profile,
        observed_version = EXCLUDED.observed_version,
        match_reason = EXCLUDED.match_reason,
        suppression_state = EXCLUDED.suppression_state,
        service_ids = EXCLUDED.service_ids,
        workload_ids = EXCLUDED.workload_ids,
        environments = EXCLUDED.environments,
        materialized_at = EXCLUDED.materialized_at
),
deleted AS (
    DELETE FROM supply_chain_impact_canonical_winners w
    WHERE NOT EXISTS (SELECT 1 FROM winners_now n WHERE n.canonical_key = w.canonical_key)
)
INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)
VALUES (1, $1)
ON CONFLICT (singleton) DO UPDATE SET materialized_at = EXCLUDED.materialized_at
`

type supplyChainImpactWinnersExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// SupplyChainImpactWinnersStore maintains the supply_chain_impact_canonical_winners
// read model from the active impact facts.
type SupplyChainImpactWinnersStore struct {
	DB supplyChainImpactWinnersExecutor
}

// NewSupplyChainImpactWinnersStore wraps a DB handle for winner maintenance.
func NewSupplyChainImpactWinnersStore(db supplyChainImpactWinnersExecutor) SupplyChainImpactWinnersStore {
	return SupplyChainImpactWinnersStore{DB: db}
}

// RebuildAllWinners reconciles the entire winners table to the current active
// impact-fact set in one atomic statement. This is the #3389 backfill and the
// safe full-resweep backstop; the per-canonical_key incremental path used by the
// shared-projection worker is a future addition that reuses
// supplyChainImpactWinnerSelectSQL with a canonical_key filter.
func (s SupplyChainImpactWinnersStore) RebuildAllWinners(ctx context.Context, materializedAt any) error {
	if s.DB == nil {
		return fmt.Errorf("supply chain impact winners database is required")
	}
	if _, err := s.DB.ExecContext(ctx, rebuildSupplyChainImpactWinnersSQL, materializedAt); err != nil {
		return fmt.Errorf("rebuild supply chain impact canonical winners: %w", err)
	}
	return nil
}
