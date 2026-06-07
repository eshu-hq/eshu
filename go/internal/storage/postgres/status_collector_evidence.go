package postgres

import (
	"context"
	"fmt"
	"strings"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

const collectorFactEvidenceQuery = `
WITH fact_evidence AS (
SELECT
    scope.collector_kind,
    COALESCE(NULLIF(BTRIM(item.collector_instance_id), ''), '') AS collector_instance_id,
    CASE
      WHEN fact.fact_kind LIKE 'reducer_%' THEN 'reducer_facts'
      ELSE 'source_facts'
    END AS evidence_source,
    fact.observed_at,
    fact.ingested_at
FROM ingestion_scopes AS scope
JOIN fact_records AS fact
  ON fact.scope_id = scope.scope_id
 AND fact.generation_id = scope.active_generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = scope.scope_id
 AND generation.generation_id = fact.generation_id
LEFT JOIN LATERAL (
    SELECT workflow_item.collector_instance_id
    FROM workflow_work_items AS workflow_item
    WHERE workflow_item.collector_kind = scope.collector_kind
      AND workflow_item.scope_id = scope.scope_id
      AND workflow_item.generation_id = fact.generation_id
    ORDER BY workflow_item.updated_at DESC, workflow_item.work_item_id ASC
    LIMIT 1
) AS item ON TRUE
WHERE scope.collector_kind IN (
    'aws',
    'ci_cd_run',
    'documentation',
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
  AND fact.is_tombstone = FALSE
)
SELECT
    collector_kind,
    collector_instance_id,
    evidence_source,
    COUNT(*) AS observation_count,
    MAX(observed_at) AS last_observed_at,
    MAX(ingested_at) AS updated_at
FROM fact_evidence
GROUP BY collector_kind, collector_instance_id, evidence_source
ORDER BY collector_kind ASC, collector_instance_id ASC, evidence_source ASC
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
		if err := rows.Scan(
			&row.CollectorKind,
			&row.InstanceID,
			&row.EvidenceSource,
			&observationCount,
			&row.LastObservedAt,
			&row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("read collector fact evidence: %w", err)
		}
		row.CollectorKind = strings.TrimSpace(row.CollectorKind)
		row.InstanceID = strings.TrimSpace(row.InstanceID)
		row.EvidenceSource = strings.TrimSpace(row.EvidenceSource)
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
