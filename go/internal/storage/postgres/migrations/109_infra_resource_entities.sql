-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Entity-grain read model for the graph-backed infra aggregate routes (#6793).
--
-- /api/v0/infra/resources/count and /inventory aggregate every infra node. On
-- NornicDB every aggregate is a full label scan, so on a large corpus the
-- routes exceed the graph-read budget and return 504.
-- This table holds one narrow row per entity-derived infra node, copied from
-- content_entities by the content writer after it commits a path
-- (storage/postgres/infra/inventory). The aggregate is then a scan of a small,
-- cache-resident heap.
--
-- Rows are keyed by entity_id and cleaned by (repo_id, relative_path), exactly
-- like content_entities, so full and delta projections, retries, and duplicate
-- delivery are idempotent by construction. There are no counters.
--
-- This migration creates the empty table only. The backfill runs per repository
-- under the same advisory lock the live derive takes, and records
-- infra_resource_entity_backfill_markers when every repository is covered.
-- Readers use the graph until that marker exists.
CREATE TABLE IF NOT EXISTS infra_resource_entities (
    entity_id         TEXT PRIMARY KEY,
    repo_id           TEXT NOT NULL,
    scope_id          TEXT NOT NULL DEFAULT '',
    generation_id     TEXT NOT NULL DEFAULT '',
    relative_path     TEXT NOT NULL,
    label             TEXT NOT NULL,
    entity_name       TEXT NOT NULL,
    kind              TEXT NOT NULL DEFAULT '',
    resource_type     TEXT NOT NULL DEFAULT '',
    data_type         TEXT NOT NULL DEFAULT '',
    provider          TEXT NOT NULL DEFAULT '',
    environment       TEXT NOT NULL DEFAULT '',
    resource_service  TEXT NOT NULL DEFAULT '',
    resource_category TEXT NOT NULL DEFAULT '',
    service_kind      TEXT NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ NOT NULL
);

-- The derive step deletes and re-inserts by (repo_id, relative_path).
CREATE INDEX IF NOT EXISTS infra_resource_entities_repo_path_idx
    ON infra_resource_entities (repo_id, relative_path);

CREATE TABLE IF NOT EXISTS infra_resource_entity_backfill_markers (
    marker_name  TEXT PRIMARY KEY,
    completed_at TIMESTAMPTZ NOT NULL
);

-- The reducer's drift reconcile walk stores where its last cycle stopped, so
-- a restarted process resumes from one primary-key lookup instead of
-- enumerating every repository to choose a start, and the walk keeps
-- advancing however often processes restart.
CREATE TABLE IF NOT EXISTS infra_resource_entity_reconcile_cursor (
    walk_name  TEXT PRIMARY KEY,
    cursor     TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

-- Rolling-upgrade fence. Derive-aware binaries mark every connection with
-- SET eshu.infra_inventory_writer = 'derive' (inventory.WriterSessionSQL);
-- their content_entities writes keep this table in step. A write of an
-- infra-typed row from any other connection (an older ingester, projector,
-- or bootstrap-index binary, or manual SQL) marks its repository here in the
-- same statement. Readers trust the table only while this table is empty,
-- and the reducer's reconcile re-derives and clears each marked repository.
-- The upsert takes the mark's row lock until the writer commits, so a repair
-- that clears the mark waits for that write and re-derives its rows.
CREATE TABLE IF NOT EXISTS infra_resource_entity_dirty_repos (
    repo_id   TEXT PRIMARY KEY,
    marked_at TIMESTAMPTZ NOT NULL
);

CREATE OR REPLACE FUNCTION mark_infra_resource_entity_dirty_repo()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        INSERT INTO infra_resource_entity_dirty_repos (repo_id, marked_at)
        VALUES (NEW.repo_id, clock_timestamp())
        ON CONFLICT (repo_id) DO UPDATE SET marked_at = EXCLUDED.marked_at;
    END IF;
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        INSERT INTO infra_resource_entity_dirty_repos (repo_id, marked_at)
        VALUES (OLD.repo_id, clock_timestamp())
        ON CONFLICT (repo_id) DO UPDATE SET marked_at = EXCLUDED.marked_at;
    END IF;
    RETURN NULL;
END;
$$;

-- The WHEN clauses list inventory.Labels; TestInfraInventoryFenceTriggerLabels
-- pins the three lists to it. A derive-aware writer's rows fail the first
-- predicate, so they never call the function.
DROP TRIGGER IF EXISTS content_entities_infra_fence_insert ON content_entities;
CREATE TRIGGER content_entities_infra_fence_insert
AFTER INSERT ON content_entities
FOR EACH ROW
WHEN (
    current_setting('eshu.infra_inventory_writer', true) IS DISTINCT FROM 'derive'
    AND NEW.entity_type IN (
        'K8sResource', 'KustomizeOverlay', 'TerraformResource', 'TerraformModule',
        'TerraformVariable', 'TerraformOutput', 'TerraformDataSource', 'TerraformProvider',
        'TerraformLocal', 'TerraformBackend', 'TerraformImport', 'TerraformMovedBlock',
        'TerraformRemovedBlock', 'TerraformCheck', 'TerraformLockProvider', 'TerraformBlock',
        'TerragruntConfig', 'TerragruntDependency', 'CloudFormationResource',
        'ArgoCDApplication', 'ArgoCDApplicationSet', 'CrossplaneXRD', 'CrossplaneComposition',
        'HelmChart', 'HelmValues')
)
EXECUTE FUNCTION mark_infra_resource_entity_dirty_repo();

DROP TRIGGER IF EXISTS content_entities_infra_fence_update ON content_entities;
CREATE TRIGGER content_entities_infra_fence_update
AFTER UPDATE ON content_entities
FOR EACH ROW
WHEN (
    current_setting('eshu.infra_inventory_writer', true) IS DISTINCT FROM 'derive'
    AND (NEW.entity_type IN (
        'K8sResource', 'KustomizeOverlay', 'TerraformResource', 'TerraformModule',
        'TerraformVariable', 'TerraformOutput', 'TerraformDataSource', 'TerraformProvider',
        'TerraformLocal', 'TerraformBackend', 'TerraformImport', 'TerraformMovedBlock',
        'TerraformRemovedBlock', 'TerraformCheck', 'TerraformLockProvider', 'TerraformBlock',
        'TerragruntConfig', 'TerragruntDependency', 'CloudFormationResource',
        'ArgoCDApplication', 'ArgoCDApplicationSet', 'CrossplaneXRD', 'CrossplaneComposition',
        'HelmChart', 'HelmValues')
    OR OLD.entity_type IN (
        'K8sResource', 'KustomizeOverlay', 'TerraformResource', 'TerraformModule',
        'TerraformVariable', 'TerraformOutput', 'TerraformDataSource', 'TerraformProvider',
        'TerraformLocal', 'TerraformBackend', 'TerraformImport', 'TerraformMovedBlock',
        'TerraformRemovedBlock', 'TerraformCheck', 'TerraformLockProvider', 'TerraformBlock',
        'TerragruntConfig', 'TerragruntDependency', 'CloudFormationResource',
        'ArgoCDApplication', 'ArgoCDApplicationSet', 'CrossplaneXRD', 'CrossplaneComposition',
        'HelmChart', 'HelmValues'))
)
EXECUTE FUNCTION mark_infra_resource_entity_dirty_repo();

DROP TRIGGER IF EXISTS content_entities_infra_fence_delete ON content_entities;
CREATE TRIGGER content_entities_infra_fence_delete
AFTER DELETE ON content_entities
FOR EACH ROW
WHEN (
    current_setting('eshu.infra_inventory_writer', true) IS DISTINCT FROM 'derive'
    AND OLD.entity_type IN (
        'K8sResource', 'KustomizeOverlay', 'TerraformResource', 'TerraformModule',
        'TerraformVariable', 'TerraformOutput', 'TerraformDataSource', 'TerraformProvider',
        'TerraformLocal', 'TerraformBackend', 'TerraformImport', 'TerraformMovedBlock',
        'TerraformRemovedBlock', 'TerraformCheck', 'TerraformLockProvider', 'TerraformBlock',
        'TerragruntConfig', 'TerragruntDependency', 'CloudFormationResource',
        'ArgoCDApplication', 'ArgoCDApplicationSet', 'CrossplaneXRD', 'CrossplaneComposition',
        'HelmChart', 'HelmValues')
)
EXECUTE FUNCTION mark_infra_resource_entity_dirty_repo();
