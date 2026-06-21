package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/lib/pq"
)

// collectorFactEvidenceQuery summarizes active source and reducer fact evidence
// per collector instance for the collector-readiness surface.
//
// The fact_summary CTE aggregates each active scope's facts inside a per-scope
// LATERAL subquery rather than one global GROUP BY over every active
// fact_records row. A single global GROUP BY had to sort all active facts at
// once, which spilled to an on-disk external merge sort that scaled with total
// active facts (issue #3375: ~20s on a 906-repo / 4.5M fact_records stack). The
// per-scope LATERAL keeps each aggregate bounded to one scope's facts so it
// stays in memory and never spills. The emitted rows are byte-identical to the
// previous shape, so collector readiness evidence is preserved exactly.
const collectorFactEvidenceQuery = `
WITH active_scopes AS (
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
),
fact_summary AS (
SELECT
    scope.collector_kind,
    scope.scope_id,
    scope.generation_id,
    per_scope.evidence_source,
    per_scope.source_system,
    per_scope.observation_count,
    per_scope.last_observed_at,
    per_scope.updated_at
FROM active_scopes AS scope
JOIN LATERAL (
  SELECT
    CASE
      WHEN fact.fact_kind LIKE 'reducer_%' THEN 'reducer_facts'
      ELSE 'source_facts'
    END AS evidence_source,
    NULLIF(BTRIM(fact.source_system), '') AS source_system,
    COUNT(*) AS observation_count,
    MAX(fact.observed_at) AS last_observed_at,
    MAX(fact.ingested_at) AS updated_at
  FROM fact_records AS fact
    WHERE fact.scope_id = scope.scope_id
      AND fact.generation_id = scope.generation_id
      AND fact.is_tombstone = FALSE
  GROUP BY evidence_source, source_system
) AS per_scope ON TRUE
),
workflow_instances AS (
SELECT DISTINCT ON (
    workflow_item.collector_kind,
    workflow_item.scope_id,
    workflow_item.generation_id
)
    workflow_item.collector_kind,
    workflow_item.scope_id,
    workflow_item.generation_id,
    workflow_item.collector_instance_id
FROM workflow_work_items AS workflow_item
JOIN active_scopes AS scope
  ON scope.collector_kind = workflow_item.collector_kind
 AND scope.scope_id = workflow_item.scope_id
 AND scope.generation_id = workflow_item.generation_id
WHERE NULLIF(BTRIM(workflow_item.collector_instance_id), '') IS NOT NULL
ORDER BY
    workflow_item.collector_kind,
    workflow_item.scope_id,
    workflow_item.generation_id,
    workflow_item.updated_at DESC,
    workflow_item.work_item_id ASC
)
SELECT
    fact.collector_kind,
    COALESCE(NULLIF(BTRIM(item.collector_instance_id), ''), '') AS collector_instance_id,
    fact.evidence_source,
    COALESCE(
      ARRAY_AGG(DISTINCT fact.source_system ORDER BY fact.source_system)
        FILTER (WHERE fact.source_system IS NOT NULL),
      ARRAY[]::text[]
    ) AS source_systems,
    SUM(fact.observation_count) AS observation_count,
    MAX(fact.last_observed_at) AS last_observed_at,
    MAX(fact.updated_at) AS updated_at
FROM fact_summary AS fact
LEFT JOIN workflow_instances AS item
  ON item.collector_kind = fact.collector_kind
 AND item.scope_id = fact.scope_id
 AND item.generation_id = fact.generation_id
GROUP BY fact.collector_kind, collector_instance_id, fact.evidence_source
ORDER BY fact.collector_kind ASC, collector_instance_id ASC, fact.evidence_source ASC
LIMIT 200
`

func readCollectorFactEvidence(
	ctx context.Context,
	queryer Queryer,
) ([]statuspkg.CollectorFactEvidence, error) {
	rows, err := queryer.QueryContext(ctx, collectorFactEvidenceQuery)
	if err != nil {
		return nil, fmt.Errorf("read collector fact evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	output := []statuspkg.CollectorFactEvidence{}
	for rows.Next() {
		var row statuspkg.CollectorFactEvidence
		var observationCount int64
		var sourceSystems pq.StringArray
		if err := rows.Scan(
			&row.CollectorKind,
			&row.InstanceID,
			&row.EvidenceSource,
			&sourceSystems,
			&observationCount,
			&row.LastObservedAt,
			&row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("read collector fact evidence: %w", err)
		}
		row.CollectorKind = strings.TrimSpace(row.CollectorKind)
		row.InstanceID = strings.TrimSpace(row.InstanceID)
		row.EvidenceSource = strings.TrimSpace(row.EvidenceSource)
		row.SourceSystems = cleanCollectorSourceSystems(sourceSystems)
		row.ObservationCount = int(observationCount)
		row.LastObservedAt = row.LastObservedAt.UTC()
		row.UpdatedAt = row.UpdatedAt.UTC()
		if row.CollectorKind == "" || row.EvidenceSource == "" || row.ObservationCount <= 0 {
			continue
		}
		output = append(output, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read collector fact evidence: %w", err)
	}
	return output, nil
}

func cleanCollectorSourceSystems(values []string) []string {
	seen := map[string]struct{}{}
	output := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		output = append(output, value)
	}
	sort.Strings(output)
	return output
}
