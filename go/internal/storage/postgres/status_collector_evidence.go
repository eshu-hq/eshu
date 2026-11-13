// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// collectorFactEvidenceQuery summarizes active source and reducer fact evidence
// per collector instance for the collector-readiness surface.
//
// #3466: this read joins the precomputed collector_evidence_summary materialized
// table instead of aggregating fact_records on every request. The per-scope
// aggregate (previously a LATERAL over fact_records that was O(total active
// facts): ~6.6M rows / 5.4s warm at 982-scope scale) now lives in the reducer
// resweep (rebuildCollectorEvidenceSummarySQL). The per-scope collector-instance
// identity the read used to probe per request (LEFT JOIN LATERAL over
// workflow_work_items: 4.1s cold / 1.1s warm on QA at 6139 active scopes) also
// moved into the resweep, which maintains summary.collector_instance_id with
// the same DISTINCT ON semantics. This query touches only ingestion_scopes /
// scope_generations (active_scopes) and the small summary table, so it is
// bounded regardless of fact and work-item count.
//
// The active_scopes join keeps the read exact even if the summary lags one resweep
// cadence: rows for superseded generations are filtered out, and a brand-new
// scope not yet materialized is at most one cadence behind. The emitted rows are
// shape- and value-identical to the previous query at resweep freshness;
// instance attribution lags at most one cadence like the aggregates, so collector
// readiness evidence is preserved exactly (see collector_evidence_summary_read_live_test.go).
const collectorFactEvidenceQuery = `
WITH ` + activeCollectorScopesCTE + `
-- #10601 leg1 summary read (reopened): the collector-instance identity comes
-- from the resweep-maintained summary.collector_instance_id column, so the
-- snapshot takes no per-scope work-item probe.
SELECT
    summary.collector_kind,
    summary.collector_instance_id,
    summary.evidence_source,
    COALESCE(
      ARRAY_AGG(DISTINCT summary.source_system ORDER BY summary.source_system)
        FILTER (WHERE summary.source_system <> ''),
      ARRAY[]::text[]
    ) AS source_systems,
    SUM(summary.observation_count) AS observation_count,
    MAX(summary.last_observed_at) AS last_observed_at,
    MAX(summary.last_ingested_at) AS updated_at
FROM active_scopes AS scope
JOIN collector_evidence_summary AS summary
  ON summary.scope_id = scope.scope_id
 AND summary.generation_id = scope.generation_id
GROUP BY summary.collector_kind, summary.collector_instance_id, summary.evidence_source
ORDER BY summary.collector_kind ASC, summary.collector_instance_id ASC, summary.evidence_source ASC
LIMIT 200
`

func readCollectorFactEvidence(
	ctx context.Context,
	queryer db.Queryer,
) ([]collector.FactEvidence, error) {
	rows, err := queryer.QueryContext(ctx, collectorFactEvidenceQuery)
	if err != nil {
		return nil, fmt.Errorf("read collector fact evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	output := []collector.FactEvidence{}
	for rows.Next() {
		var row collector.FactEvidence
		var observationCount int64
		var sourceSystems array.StringArray
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
