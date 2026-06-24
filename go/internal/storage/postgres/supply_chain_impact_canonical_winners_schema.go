// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Maintained cross-scope canonical winner per supply-chain impact finding
// (#3389). See docs/internal/supply-chain-impact-canonical-dedup-materialization-design.md.
//
// The impact-findings list endpoint deduplicates at read time with
// ROW_NUMBER() OVER (PARTITION BY canonical_key ...), which sorts the full
// filtered set and spills (~98MB at a broad page). This table moves that dedup
// off the read path: it holds exactly one row per currently-active canonical_key
// — the winner the read-time tiebreak would pick — denormalized with the
// filterable columns so the list read runs filter + keyset + LIMIT on this table
// alone (measured O(page), sub-ms) and joins fact_records by winner_fact_id only
// for the page payloads. fact_records stays the single home for payload truth.

// supplyChainImpactCanonicalWinnersSchemaSQL mirrors
// schema/data-plane/postgres/033_supply_chain_impact_canonical_winners.sql.
const supplyChainImpactCanonicalWinnersSchemaSQL = `
CREATE TABLE IF NOT EXISTS supply_chain_impact_canonical_winners (
    canonical_key TEXT PRIMARY KEY,
    winner_fact_id TEXT NOT NULL,
    winner_scope_id TEXT NOT NULL DEFAULT '',
    finding_id TEXT NOT NULL,
    priority_score INTEGER NOT NULL DEFAULT 0,
    source_count INTEGER NOT NULL DEFAULT 1,
    impact_status TEXT NOT NULL DEFAULT '',
    ecosystem TEXT NOT NULL DEFAULT '',
    severity_bucket TEXT NOT NULL DEFAULT '',
    repository_id TEXT NOT NULL DEFAULT '',
    cve_id TEXT NOT NULL DEFAULT '',
    advisory_id TEXT NOT NULL DEFAULT '',
    package_id TEXT NOT NULL DEFAULT '',
    subject_digest TEXT NOT NULL DEFAULT '',
    image_ref TEXT NOT NULL DEFAULT '',
    priority_bucket TEXT NOT NULL DEFAULT '',
    detection_profile TEXT NOT NULL DEFAULT '',
    observed_version TEXT NOT NULL DEFAULT '',
    match_reason TEXT NOT NULL DEFAULT '',
    suppression_state TEXT NOT NULL DEFAULT 'active',
    service_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    workload_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    environments JSONB NOT NULL DEFAULT '[]'::jsonb,
    materialized_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS supply_chain_impact_canonical_winners_finding_idx
    ON supply_chain_impact_canonical_winners (finding_id);
CREATE INDEX IF NOT EXISTS supply_chain_impact_canonical_winners_priority_idx
    ON supply_chain_impact_canonical_winners (priority_score DESC, finding_id);

CREATE INDEX IF NOT EXISTS supply_chain_impact_canonical_winners_status_idx
    ON supply_chain_impact_canonical_winners (impact_status, priority_score DESC, finding_id);
CREATE INDEX IF NOT EXISTS supply_chain_impact_canonical_winners_severity_idx
    ON supply_chain_impact_canonical_winners (severity_bucket, priority_score DESC, finding_id);
CREATE INDEX IF NOT EXISTS supply_chain_impact_canonical_winners_repository_idx
    ON supply_chain_impact_canonical_winners (repository_id, priority_score DESC, finding_id);

CREATE INDEX IF NOT EXISTS supply_chain_impact_canonical_winners_service_gin
    ON supply_chain_impact_canonical_winners USING GIN (service_ids);
CREATE INDEX IF NOT EXISTS supply_chain_impact_canonical_winners_workload_gin
    ON supply_chain_impact_canonical_winners USING GIN (workload_ids);
CREATE INDEX IF NOT EXISTS supply_chain_impact_canonical_winners_environment_gin
    ON supply_chain_impact_canonical_winners USING GIN (environments);
`

// supplyChainImpactCanonicalWinnersBootstrapDefinition registers the winners
// table so it exists in fresh and bootstrapped data planes.
func supplyChainImpactCanonicalWinnersBootstrapDefinition() Definition {
	return Definition{
		Name: "supply_chain_impact_canonical_winners",
		Path: "schema/data-plane/postgres/033_supply_chain_impact_canonical_winners.sql",
		SQL:  supplyChainImpactCanonicalWinnersSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(
		bootstrapDefinitions,
		supplyChainImpactCanonicalWinnersBootstrapDefinition(),
	)
}
