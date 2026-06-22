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
