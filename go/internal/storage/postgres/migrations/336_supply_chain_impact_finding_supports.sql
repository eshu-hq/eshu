-- 336_supply_chain_impact_finding_supports.sql: one row per active finding
-- fact (a "support") so grant-scoped and existence-filtered impact reads can
-- leave the fact CTE (#10880 PR-B1, arbiter option B). The row identity is the
-- support's fact_id: the SQL canonical key collapses the CVE and advisory ids
-- that the writer's fact id keeps apart, so (canonical_key, scope_id) is not
-- unique. The table carries the generation id (a cascading foreign key, so
-- retention and repository retirement remove supports with their generation,
-- as they remove the facts) and readers keep the active-generation join. Operator suppression overlay facts (scope
-- operator:vulnerability_suppressions) share the table and are told apart by
-- scope_id, as the legacy CTE does. The writer fills it in the transaction that
-- writes the finding facts; a marker-gated reducer backfill fills existing
-- rows. No reader uses it in this migration's release.
--
-- Indexes are those a measured plan needs (docs/internal/design/
-- 10880-pr-b1-supports-table-proof.md): the primary key (page and override
-- joins), (scope_id, generation_id) for the scope grant leg and the stale
-- generation skip, (repository_id) for the repository grant leg (14x slower
-- without it), (finding_id) for explain by public finding id, and a partial
-- canonical_key index for the operator override probe.

CREATE TABLE IF NOT EXISTS supply_chain_impact_finding_supports (
    fact_id                TEXT PRIMARY KEY,
    canonical_key          TEXT NOT NULL,
    scope_id               TEXT NOT NULL,
    generation_id          TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    finding_id             TEXT NOT NULL,
    has_payload_finding_id SMALLINT NOT NULL,
    priority_score         INTEGER NOT NULL,
    source_confidence      TEXT NOT NULL,
    suppression_state      TEXT NOT NULL,
    impact_status          TEXT NOT NULL DEFAULT '',
    ecosystem              TEXT NOT NULL DEFAULT '',
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

CREATE INDEX IF NOT EXISTS supply_chain_impact_finding_supports_scope_generation_idx
    ON supply_chain_impact_finding_supports (scope_id, generation_id);

CREATE INDEX IF NOT EXISTS supply_chain_impact_finding_supports_repository_idx
    ON supply_chain_impact_finding_supports (repository_id);

CREATE INDEX IF NOT EXISTS supply_chain_impact_finding_supports_finding_idx
    ON supply_chain_impact_finding_supports (finding_id);

CREATE INDEX IF NOT EXISTS supply_chain_impact_finding_supports_operator_idx
    ON supply_chain_impact_finding_supports (canonical_key)
    WHERE scope_id = 'operator:vulnerability_suppressions';

-- Marker and cursor of the reducer-owned backfill that fills supports for the
-- finding facts written before the maintainer shipped. One row per marker name.
-- completed_at is set only after a full sweep followed by a verification that
-- found no live finding fact without a support, so readers (a later PR) refuse a
-- grant-scoped call while it is NULL or its contract_version is behind. The
-- cursor is the last (scope, generation, fact) the sweep finished, so the sweep
-- resumes at the next live finding fact in that order; sweeps counts restarts
-- after a verification found a gap.
CREATE TABLE IF NOT EXISTS supply_chain_impact_finding_supports_backfill (
    marker_name          TEXT PRIMARY KEY,
    contract_version     INTEGER NOT NULL,
    cursor_scope_id      TEXT NOT NULL DEFAULT '',
    cursor_generation_id TEXT NOT NULL DEFAULT '',
    cursor_fact_id       TEXT NOT NULL DEFAULT '',
    sweeps               INTEGER NOT NULL DEFAULT 0,
    completed_at         TIMESTAMPTZ,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Old-writer fence. The same-transaction maintainer keeps supports current only
-- for writers that run it, and a migration runs in PreSync while reducers of the
-- previous release still write finding facts. The trigger marks the scope of
-- every finding fact (insert or update, tombstones included) that a writer
-- without the maintainer writes, and the backfill drains the marks. The
-- maintainer's writer sets eshu.supply_chain_impact_supports_writer to
-- 'supports_v1' for its transaction, so its rows skip the trigger entirely (the
-- WHEN clause is the only cost on every other insert); the same setting pattern
-- as migration 139. Readers treat a non-empty table as not ready.
CREATE TABLE IF NOT EXISTS supply_chain_impact_finding_supports_dirty (
    scope_id      TEXT PRIMARY KEY,
    dirty_version BIGINT NOT NULL DEFAULT 1,
    marked_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION supply_chain_impact_finding_supports_mark_dirty() RETURNS trigger AS $$
BEGIN
    INSERT INTO supply_chain_impact_finding_supports_dirty AS dirty (scope_id)
    VALUES (NEW.scope_id)
    ON CONFLICT (scope_id) DO UPDATE
    SET dirty_version = dirty.dirty_version + 1, marked_at = now();
    RETURN NULL;
END $$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS supply_chain_impact_finding_supports_dirty_insert ON fact_records;
CREATE TRIGGER supply_chain_impact_finding_supports_dirty_insert
AFTER INSERT ON fact_records
FOR EACH ROW
WHEN (
    NEW.fact_kind = 'reducer_supply_chain_impact_finding'
    AND current_setting('eshu.supply_chain_impact_supports_writer', true) IS DISTINCT FROM 'supports_v1'
)
EXECUTE FUNCTION supply_chain_impact_finding_supports_mark_dirty();

DROP TRIGGER IF EXISTS supply_chain_impact_finding_supports_dirty_update ON fact_records;
CREATE TRIGGER supply_chain_impact_finding_supports_dirty_update
AFTER UPDATE ON fact_records
FOR EACH ROW
WHEN (
    NEW.fact_kind = 'reducer_supply_chain_impact_finding'
    AND current_setting('eshu.supply_chain_impact_supports_writer', true) IS DISTINCT FROM 'supports_v1'
)
EXECUTE FUNCTION supply_chain_impact_finding_supports_mark_dirty();
