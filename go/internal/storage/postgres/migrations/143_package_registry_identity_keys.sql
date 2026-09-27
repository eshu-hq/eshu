-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

CREATE TABLE IF NOT EXISTS package_registry_identity_keys (
    fact_id TEXT NOT NULL REFERENCES fact_records(fact_id) ON DELETE CASCADE,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    ecosystem TEXT NOT NULL,
    package_name TEXT NOT NULL,
    package_id TEXT NOT NULL,
    PRIMARY KEY (fact_id, ecosystem, package_name, package_id)
);

CREATE INDEX IF NOT EXISTS package_registry_identity_keys_lookup_idx
    ON package_registry_identity_keys (
        ecosystem, package_name, scope_id, generation_id, package_id, fact_id
    );

-- Registry identity writes from a pre-sidecar binary must fence the combined
-- manifest/registry read model until scope rebuild has derived both tables.
DROP TRIGGER IF EXISTS package_manifest_consumption_keys_dirty_insert ON fact_records;
CREATE TRIGGER package_manifest_consumption_keys_dirty_insert
AFTER INSERT ON fact_records FOR EACH ROW
WHEN (
    (NEW.fact_kind = 'content_entity' AND NEW.source_system = 'git' AND NEW.payload->>'entity_type' = 'Variable'
        AND COALESCE(NULLIF(NEW.payload->>'config_kind', ''), NEW.payload->'entity_metadata'->>'config_kind') = 'dependency')
    OR NEW.fact_kind = 'package_registry.package'
)
EXECUTE FUNCTION package_manifest_consumption_keys_mark_dirty();

DROP TRIGGER IF EXISTS package_manifest_consumption_keys_dirty_update ON fact_records;
CREATE TRIGGER package_manifest_consumption_keys_dirty_update
AFTER UPDATE ON fact_records FOR EACH ROW
WHEN (
    (NEW.fact_kind = 'content_entity' AND NEW.source_system = 'git' AND NEW.payload->>'entity_type' = 'Variable'
        AND COALESCE(NULLIF(NEW.payload->>'config_kind', ''), NEW.payload->'entity_metadata'->>'config_kind') = 'dependency')
    OR (OLD.fact_kind = 'content_entity' AND OLD.source_system = 'git' AND OLD.payload->>'entity_type' = 'Variable'
        AND COALESCE(NULLIF(OLD.payload->>'config_kind', ''), OLD.payload->'entity_metadata'->>'config_kind') = 'dependency')
    OR NEW.fact_kind = 'package_registry.package'
    OR OLD.fact_kind = 'package_registry.package'
)
EXECUTE FUNCTION package_manifest_consumption_keys_mark_dirty();

DROP TRIGGER IF EXISTS package_manifest_consumption_keys_dirty_delete ON fact_records;
CREATE TRIGGER package_manifest_consumption_keys_dirty_delete
AFTER DELETE ON fact_records FOR EACH ROW
WHEN (
    (OLD.fact_kind = 'content_entity' AND OLD.source_system = 'git' AND OLD.payload->>'entity_type' = 'Variable'
        AND COALESCE(NULLIF(OLD.payload->>'config_kind', ''), OLD.payload->'entity_metadata'->>'config_kind') = 'dependency')
    OR OLD.fact_kind = 'package_registry.package'
)
EXECUTE FUNCTION package_manifest_consumption_keys_mark_dirty();

-- A v2 pass stores progress separately from the completed marker. Readers
-- accept only the completed marker, so a crashed or bounded initial pass
-- cannot advertise an incomplete historical sidecar.
CREATE TABLE IF NOT EXISTS package_manifest_consumption_key_backfill_progress (
    marker_name TEXT PRIMARY KEY,
    cursor_scope_id TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL
);
