package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// activeCollectorScopesCTE selects the active generation of every readiness
// collector family. It is shared by the readiness read (status_collector_evidence.go)
// and the resweep below so the active-scope set cannot drift between writer and
// reader. It is cheap: it scans ingestion_scopes / scope_generations (one row per
// scope), never fact_records.
const activeCollectorScopesCTE = `active_scopes AS (
SELECT
    scope.collector_kind,
    scope.scope_id,
    scope.active_generation_id AS generation_id
FROM ingestion_scopes AS scope
JOIN scope_generations AS generation
  ON generation.scope_id = scope.scope_id
 AND generation.generation_id = scope.active_generation_id
WHERE scope.active_generation_id IS NOT NULL
  AND scope.collector_kind IN (
    'aws',
    'ci_cd_run',
    'documentation',
    'git',
    'grafana',
    'jira',
    'loki',
    'oci_registry',
    'package_registry',
    'pagerduty',
    'prometheus_mimir',
    'sbom_attestation',
    'scanner_worker',
    'security_alert',
    'tempo',
    'terraform_state',
    'vault_live',
    'vulnerability_intelligence'
)
  AND generation.status = 'active'
)`

// rebuildCollectorEvidenceSummarySQL reconciles collector_evidence_summary to the
// current active fact set in one atomic statement (#3466). The per-scope LATERAL
// aggregate is byte-equivalent to the pre-#3466 readiness aggregate, so
// observation_count, last_observed_at, and last_ingested_at stay exact; it just
// writes to the summary table instead of returning to the API.
//
// One statement = one snapshot, so a concurrent reader never sees a torn rebuild.
// The upsert is a data-modifying CTE (Postgres runs it to completion regardless
// of whether the final statement reads it); the final DELETE removes summary rows
// whose (scope_id, generation_id, evidence_source, source_system) is no longer in
// the recomputed active set. That delete-stale step is what makes the resweep
// self-healing for every change class — superseded generations, fully tombstoned
// scopes, and FK-cascade pruned generations all drop out without per-key dirty
// tracking. materialized_at is stamped from the bound parameter ($1), never wall
// clock, so resume/replay is deterministic.
//
// source_system collapses the original NULL group (blank or whitespace-only
// source systems, previously NULLIF(BTRIM(...))) to the empty-string sentinel the
// primary key requires; the read reconstructs the no-source-system case by
// excluding empty-string source systems.
const rebuildCollectorEvidenceSummarySQL = `
WITH ` + activeCollectorScopesCTE + `,
computed AS (
SELECT
    scope.collector_kind,
    scope.scope_id,
    scope.generation_id,
    per_scope.evidence_source,
    per_scope.source_system,
    per_scope.observation_count,
    per_scope.last_observed_at,
    per_scope.last_ingested_at
FROM active_scopes AS scope
JOIN LATERAL (
  SELECT
    CASE
      WHEN fact.fact_kind LIKE 'reducer_%' THEN 'reducer_facts'
      ELSE 'source_facts'
    END AS evidence_source,
    COALESCE(NULLIF(BTRIM(fact.source_system), ''), '') AS source_system,
    COUNT(*) AS observation_count,
    MAX(fact.observed_at) AS last_observed_at,
    MAX(fact.ingested_at) AS last_ingested_at
  FROM fact_records AS fact
    WHERE fact.scope_id = scope.scope_id
      AND fact.generation_id = scope.generation_id
      AND fact.is_tombstone = FALSE
  GROUP BY evidence_source, source_system
) AS per_scope ON TRUE
),
upserted AS (
    INSERT INTO collector_evidence_summary (
        scope_id, generation_id, collector_kind, evidence_source, source_system,
        observation_count, last_observed_at, last_ingested_at, materialized_at
    )
    SELECT
        scope_id, generation_id, collector_kind, evidence_source, source_system,
        observation_count, last_observed_at, last_ingested_at, $1
    FROM computed
    ON CONFLICT (scope_id, generation_id, evidence_source, source_system) DO UPDATE SET
        collector_kind = EXCLUDED.collector_kind,
        observation_count = EXCLUDED.observation_count,
        last_observed_at = EXCLUDED.last_observed_at,
        last_ingested_at = EXCLUDED.last_ingested_at,
        materialized_at = EXCLUDED.materialized_at
)
DELETE FROM collector_evidence_summary AS summary
WHERE NOT EXISTS (
    SELECT 1 FROM computed AS c
    WHERE c.scope_id = summary.scope_id
      AND c.generation_id = summary.generation_id
      AND c.evidence_source = summary.evidence_source
      AND c.source_system = summary.source_system
)
`

// CollectorEvidenceSummaryStore maintains collector_evidence_summary, the
// materialized read model for the collector-readiness surface (#3466). It needs
// both the resweep ExecContext and the watermark QueryContext, so it takes the
// shared ExecQueryer surface.
type CollectorEvidenceSummaryStore struct {
	DB ExecQueryer
}

// NewCollectorEvidenceSummaryStore wraps a DB handle for summary maintenance.
func NewCollectorEvidenceSummaryStore(db ExecQueryer) CollectorEvidenceSummaryStore {
	return CollectorEvidenceSummaryStore{DB: db}
}

// RebuildAllCollectorEvidence reconciles the entire collector_evidence_summary
// table to the current active fact set in one atomic statement. It is the #3466
// startup backfill and the safe full-resweep backstop; an incremental per-scope
// path is a possible future optimization that reuses the same per-scope aggregate
// with a scope filter. materializedAt is stamped as the resweep watermark.
func (s CollectorEvidenceSummaryStore) RebuildAllCollectorEvidence(ctx context.Context, materializedAt any) error {
	if s.DB == nil {
		return fmt.Errorf("collector evidence summary database is required")
	}
	if _, err := s.DB.ExecContext(ctx, rebuildCollectorEvidenceSummarySQL, materializedAt); err != nil {
		return fmt.Errorf("rebuild collector evidence summary: %w", err)
	}
	return nil
}

// lastCollectorEvidenceMaterializedAtSQL reads the newest resweep watermark. It is
// a cheap single-row aggregate over the small summary table (no fact_records),
// used by the maintainer's last-materialized guard.
const lastCollectorEvidenceMaterializedAtSQL = `SELECT MAX(materialized_at) FROM collector_evidence_summary`

// LastCollectorEvidenceMaterializedAt returns the newest resweep watermark in
// collector_evidence_summary. ok is false when the summary has no rows yet (the
// table has never been materialized), in which case the maintainer must resweep.
func (s CollectorEvidenceSummaryStore) LastCollectorEvidenceMaterializedAt(ctx context.Context) (time.Time, bool, error) {
	if s.DB == nil {
		return time.Time{}, false, fmt.Errorf("collector evidence summary database is required")
	}
	rows, err := s.DB.QueryContext(ctx, lastCollectorEvidenceMaterializedAtSQL)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read collector evidence summary watermark: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var watermark sql.NullTime
	if rows.Next() {
		if err := rows.Scan(&watermark); err != nil {
			return time.Time{}, false, fmt.Errorf("read collector evidence summary watermark: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, false, fmt.Errorf("read collector evidence summary watermark: %w", err)
	}
	if !watermark.Valid {
		return time.Time{}, false, nil
	}
	return watermark.Time.UTC(), true, nil
}
