// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Service materialization lineage schema (#1943, parent #1797). This is the
// additive foundation for service-scope changed-since deltas: a durable,
// versioned snapshot the reducer commits per service re-materialization, keyed by
// service_id, mirroring the repository-scope ingestion_scopes/scope_generations
// lineage that #1799 diffs. It does not change reducer_service_catalog_correlation
// facts.

// serviceMaterializationGenerationsSchemaSQL is the per-service generation
// lineage. One row per re-materialization, with a single-active-per-service
// partial unique index that mirrors scope_generations_active_scope_idx so a
// reader and the delta surface always resolve exactly one active generation per
// service_id.
const serviceMaterializationGenerationsSchemaSQL = `
CREATE TABLE IF NOT EXISTS service_materialization_generations (
    generation_id TEXT PRIMARY KEY,
    service_id TEXT NOT NULL,
    trigger_kind TEXT NOT NULL,
    source_intent_id TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    superseded_at TIMESTAMPTZ NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS service_materialization_generations_service_idx
    ON service_materialization_generations (service_id, status, ingested_at DESC);

CREATE INDEX IF NOT EXISTS service_materialization_generations_observed_idx
    ON service_materialization_generations (service_id, observed_at DESC, generation_id);

CREATE UNIQUE INDEX IF NOT EXISTS service_materialization_generations_active_service_idx
    ON service_materialization_generations (service_id)
    WHERE status = 'active';
`

// serviceEvidenceSnapshotsSchemaSQL is the generation-stable per-evidence
// snapshot. The identity is (generation_id, service_evidence_key); the generation
// lives in a column, never in the key, so the same logical evidence row keeps its
// key across generations and the FULL OUTER JOIN delta can classify
// added/updated/unchanged/retired/superseded. The diff index orders rows by
// (generation_id, evidence_family, service_evidence_key) so the bounded sample
// reads are deterministic and indexed.
const serviceEvidenceSnapshotsSchemaSQL = `
CREATE TABLE IF NOT EXISTS service_evidence_snapshots (
    generation_id TEXT NOT NULL REFERENCES service_materialization_generations(generation_id) ON DELETE CASCADE,
    service_id TEXT NOT NULL,
    evidence_family TEXT NOT NULL,
    service_evidence_key TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    observed_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (generation_id, service_evidence_key)
);

CREATE INDEX IF NOT EXISTS service_evidence_snapshots_service_family_idx
    ON service_evidence_snapshots (service_id, evidence_family, generation_id);

CREATE INDEX IF NOT EXISTS service_evidence_snapshots_diff_idx
    ON service_evidence_snapshots (generation_id, evidence_family, service_evidence_key);
`

func serviceMaterializationGenerationsBootstrapDefinition() Definition {
	return Definition{
		Name: "service_materialization_generations",
		Path: "schema/data-plane/postgres/025_service_materialization_generations.sql",
		SQL:  serviceMaterializationGenerationsSchemaSQL,
	}
}

func serviceEvidenceSnapshotsBootstrapDefinition() Definition {
	return Definition{
		Name: "service_evidence_snapshots",
		Path: "schema/data-plane/postgres/026_service_evidence_snapshots.sql",
		SQL:  serviceEvidenceSnapshotsSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(
		bootstrapDefinitions,
		serviceMaterializationGenerationsBootstrapDefinition(),
		serviceEvidenceSnapshotsBootstrapDefinition(),
	)
}
