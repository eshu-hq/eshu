// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Reducer-owned materialized read model for the collector-readiness surface
// (#3466). See docs/internal/design/collector-readiness-evidence-summary-materialization-design.md.
//
// The collector-readiness query previously aggregated active fact_records inside
// a per-scope LATERAL on every request, which is O(total active facts): the live
// stack scanned ~6.6M active non-tombstone rows (982 scopes x ~6,740 facts) to
// produce ~24 output rows, costing 5.4s warm / 7.7s cold. This table precomputes
// that aggregate per active scope; the API read joins it instead of scanning
// fact_records. The reducer's CollectorEvidenceSummaryMaintainer keeps it current
// with a lease-guarded atomic full resweep (RebuildAllCollectorEvidence).
//
// Grain (scope_id, generation_id, evidence_source, source_system):
//   - evidence_source is 'reducer_facts' when fact_kind LIKE 'reducer_%', else
//     'source_facts'.
//   - source_system stores '' (not NULL) for the original
//     NULLIF(BTRIM(source_system),'') NULL group so it can sit in the primary
//     key; the read reconstructs "no source system" with source_system <> ''.
//   - observation_count / last_observed_at / last_ingested_at are the exact
//     COUNT(*) / MAX(observed_at) / MAX(ingested_at) over the scope's active
//     non-tombstone facts; observation_count stays the exact wire-contract value.
//   - materialized_at is the resweep watermark for freshness/observability.
//
// Backfill is the maintainer's startup resweep (the single authoritative
// aggregate), not embedded in the migration, so the schema cannot drift from the
// Go resweep statement.

// collectorEvidenceSummarySchemaSQL mirrors
// schema/data-plane/postgres/036_collector_evidence_summary.sql.
const collectorEvidenceSummarySchemaSQL = `
CREATE TABLE IF NOT EXISTS collector_evidence_summary (
    scope_id          TEXT NOT NULL,
    generation_id     TEXT NOT NULL,
    collector_kind    TEXT NOT NULL,
    evidence_source   TEXT NOT NULL,
    source_system     TEXT NOT NULL DEFAULT '',
    observation_count BIGINT NOT NULL,
    last_observed_at  TIMESTAMPTZ NOT NULL,
    last_ingested_at  TIMESTAMPTZ NOT NULL,
    materialized_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, evidence_source, source_system)
);

CREATE INDEX IF NOT EXISTS collector_evidence_summary_scope_gen_idx
    ON collector_evidence_summary (scope_id, generation_id);
`

// collectorEvidenceSummaryBootstrapDefinition registers the collector-readiness
// evidence summary table so it exists in fresh and bootstrapped data planes.
func collectorEvidenceSummaryBootstrapDefinition() Definition {
	return Definition{
		Name: "collector_evidence_summary",
		Path: "schema/data-plane/postgres/036_collector_evidence_summary.sql",
		SQL:  collectorEvidenceSummarySchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(
		bootstrapDefinitions,
		collectorEvidenceSummaryBootstrapDefinition(),
	)
}
