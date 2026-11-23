-- #10880 PR-B1 theory proof: candidate supports table DDL (scratch; not a migration).
-- One row per active finding fact ("support"), keyed by the fact id. Operator
-- suppression overlays (scope operator:vulnerability_suppressions) share the
-- table and are told apart by scope_id, exactly as the legacy CTE does.
CREATE TABLE IF NOT EXISTS supply_chain_impact_finding_supports (
    fact_id                TEXT PRIMARY KEY,
    canonical_key          TEXT NOT NULL,
    scope_id               TEXT NOT NULL,
    generation_id          TEXT NOT NULL,
    finding_id             TEXT NOT NULL,
    has_payload_finding_id SMALLINT NOT NULL,
    priority_score         INTEGER NOT NULL,
    source_confidence      TEXT NOT NULL,
    suppression_state      TEXT NOT NULL,
    impact_status          TEXT NOT NULL DEFAULT '',
    ecosystem              TEXT NOT NULL DEFAULT '',   -- LOWER(x COLLATE "C"), as the legacy compare
    severity_bucket        TEXT NOT NULL DEFAULT '',
    repository_id          TEXT NOT NULL DEFAULT '',
    cve_id                 TEXT NOT NULL DEFAULT '',
    advisory_id            TEXT NOT NULL DEFAULT '',
    package_id             TEXT NOT NULL DEFAULT '',
    subject_digest         TEXT NOT NULL DEFAULT '',
    image_ref              TEXT NOT NULL DEFAULT '',
    priority_bucket        TEXT NOT NULL DEFAULT '',
    detection_profile      TEXT NOT NULL DEFAULT '',
    observed_version       TEXT NOT NULL DEFAULT '',
    match_reason           TEXT NOT NULL DEFAULT '',
    payload                JSONB NOT NULL
);
