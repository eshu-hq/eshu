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
