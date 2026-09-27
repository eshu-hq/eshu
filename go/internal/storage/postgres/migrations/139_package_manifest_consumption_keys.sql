-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

CREATE TABLE IF NOT EXISTS package_manifest_consumption_keys (
    fact_id TEXT NOT NULL REFERENCES fact_records(fact_id) ON DELETE CASCADE,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    ecosystem TEXT NOT NULL,
    package_name TEXT NOT NULL,
    PRIMARY KEY (fact_id, ecosystem, package_name)
);

CREATE INDEX IF NOT EXISTS package_manifest_consumption_keys_lookup_idx
    ON package_manifest_consumption_keys (
        ecosystem, package_name, scope_id, generation_id, repository_id, fact_id
    );

CREATE TABLE IF NOT EXISTS package_manifest_consumption_key_backfill_markers (
    marker_name TEXT PRIMARY KEY,
    completed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS package_manifest_consumption_key_dirty_scopes (
    scope_id TEXT PRIMARY KEY,
    marked_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION package_manifest_consumption_keys_mark_dirty() RETURNS trigger AS $$
BEGIN
    IF current_setting('eshu.package_manifest_consumption_keys_writer', true) = 'sidecar' THEN
        RETURN NULL;
    END IF;

    IF TG_OP = 'UPDATE' THEN
        INSERT INTO package_manifest_consumption_key_dirty_scopes (scope_id)
        SELECT DISTINCT scope_id
        FROM (VALUES (OLD.scope_id), (NEW.scope_id)) AS changed(scope_id)
        ON CONFLICT (scope_id) DO UPDATE SET marked_at = EXCLUDED.marked_at;
    ELSIF TG_OP = 'DELETE' THEN
        INSERT INTO package_manifest_consumption_key_dirty_scopes (scope_id)
        VALUES (OLD.scope_id)
        ON CONFLICT (scope_id) DO UPDATE SET marked_at = EXCLUDED.marked_at;
    ELSE
        INSERT INTO package_manifest_consumption_key_dirty_scopes (scope_id)
        VALUES (NEW.scope_id)
        ON CONFLICT (scope_id) DO UPDATE SET marked_at = EXCLUDED.marked_at;
    END IF;
    RETURN NULL;
END $$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS package_manifest_consumption_keys_dirty_insert ON fact_records;
CREATE TRIGGER package_manifest_consumption_keys_dirty_insert
AFTER INSERT ON fact_records
FOR EACH ROW
WHEN (
    NEW.fact_kind = 'content_entity'
    AND NEW.source_system = 'git'
    AND NEW.payload->>'entity_type' = 'Variable'
    AND COALESCE(NULLIF(NEW.payload->>'config_kind', ''), NEW.payload->'entity_metadata'->>'config_kind') = 'dependency'
)
EXECUTE FUNCTION package_manifest_consumption_keys_mark_dirty();

DROP TRIGGER IF EXISTS package_manifest_consumption_keys_dirty_update ON fact_records;
CREATE TRIGGER package_manifest_consumption_keys_dirty_update
AFTER UPDATE ON fact_records
FOR EACH ROW
WHEN (
    (NEW.fact_kind = 'content_entity' AND NEW.source_system = 'git' AND NEW.payload->>'entity_type' = 'Variable'
        AND COALESCE(NULLIF(NEW.payload->>'config_kind', ''), NEW.payload->'entity_metadata'->>'config_kind') = 'dependency')
    OR
    (OLD.fact_kind = 'content_entity' AND OLD.source_system = 'git' AND OLD.payload->>'entity_type' = 'Variable'
        AND COALESCE(NULLIF(OLD.payload->>'config_kind', ''), OLD.payload->'entity_metadata'->>'config_kind') = 'dependency')
)
EXECUTE FUNCTION package_manifest_consumption_keys_mark_dirty();

DROP TRIGGER IF EXISTS package_manifest_consumption_keys_dirty_delete ON fact_records;
CREATE TRIGGER package_manifest_consumption_keys_dirty_delete
AFTER DELETE ON fact_records
FOR EACH ROW
WHEN (
    OLD.fact_kind = 'content_entity'
    AND OLD.source_system = 'git'
    AND OLD.payload->>'entity_type' = 'Variable'
    AND COALESCE(NULLIF(OLD.payload->>'config_kind', ''), OLD.payload->'entity_metadata'->>'config_kind') = 'dependency'
)
EXECUTE FUNCTION package_manifest_consumption_keys_mark_dirty();
