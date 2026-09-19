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

-- Rolling-upgrade fence (#6793). Derive-aware binaries mark every connection
-- with SET eshu.infra_inventory_writer = 'derive' (inventory.WriterSessionSQL,
-- run by runtime.OpenPostgres); their content_entities writes keep this table
-- in step. A write of an infra-typed row from any other session (an older
-- ingester, projector, bootstrap-index, or reducer, or manual SQL) marks its
-- repository here in the same transaction. Readers trust the table only while
-- this set is empty; the reducer's reconcile re-derives each marked repository
-- and clears the mark under the repository lock.
CREATE TABLE IF NOT EXISTS infra_resource_entity_dirty_repos (
    repo_id   TEXT PRIMARY KEY,
    marked_at TIMESTAMPTZ NOT NULL
);

-- Every mark upsert is lock-only: ON CONFLICT DO UPDATE ... WHERE false takes
-- an existing mark's row lock until the writer commits, so a repair's DELETE
-- of that mark waits for the write and its re-derive reads the rows, but the
-- row is never rewritten, so a large unaware write leaves no dead tuples.
-- DO NOTHING takes no lock and would let a repair clear a mark under an open
-- unaware write. marked_at therefore means "first marked".
--
-- The label arrays below must equal inventory.Labels;
-- TestInfraInventoryFenceTriggerLabels pins all four.
CREATE OR REPLACE FUNCTION infra_resource_entities_mark_dirty_inserted()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    -- Deduplicate before stamping the time: clock_timestamp() differs per
    -- row, so DISTINCT over (repo_id, clock_timestamp()) would keep every row
    -- and the upsert would hit one repository twice (SQLSTATE 21000).
    INSERT INTO infra_resource_entity_dirty_repos AS dirty (repo_id, marked_at)
    SELECT touched.repo_id, clock_timestamp()
    FROM (
        SELECT DISTINCT repo_id FROM new_rows
        WHERE entity_type = ANY (ARRAY[
        'K8sResource', 'KustomizeOverlay', 'TerraformResource', 'TerraformModule',
        'TerraformVariable', 'TerraformOutput', 'TerraformDataSource', 'TerraformProvider',
        'TerraformLocal', 'TerraformBackend', 'TerraformImport', 'TerraformMovedBlock',
        'TerraformRemovedBlock', 'TerraformCheck', 'TerraformLockProvider', 'TerraformBlock',
        'TerragruntConfig', 'TerragruntDependency', 'CloudFormationResource',
        'ArgoCDApplication', 'ArgoCDApplicationSet', 'CrossplaneXRD', 'CrossplaneComposition',
        'HelmChart', 'HelmValues']::text[])
    ) AS touched
    ON CONFLICT (repo_id) DO UPDATE SET marked_at = dirty.marked_at WHERE false;
    RETURN NULL;
END;
$$;

-- UPDATE marks the old and the new repository of every row that is infra
-- typed before or after the statement, so a row moving into or out of a
-- label, or between repositories, dirties both sides.
CREATE OR REPLACE FUNCTION infra_resource_entities_mark_dirty_updated()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO infra_resource_entity_dirty_repos AS dirty (repo_id, marked_at)
    SELECT touched.repo_id, clock_timestamp()
    FROM (
        SELECT repo_id FROM new_rows WHERE entity_type = ANY (ARRAY[
        'K8sResource', 'KustomizeOverlay', 'TerraformResource', 'TerraformModule',
        'TerraformVariable', 'TerraformOutput', 'TerraformDataSource', 'TerraformProvider',
        'TerraformLocal', 'TerraformBackend', 'TerraformImport', 'TerraformMovedBlock',
        'TerraformRemovedBlock', 'TerraformCheck', 'TerraformLockProvider', 'TerraformBlock',
        'TerragruntConfig', 'TerragruntDependency', 'CloudFormationResource',
        'ArgoCDApplication', 'ArgoCDApplicationSet', 'CrossplaneXRD', 'CrossplaneComposition',
        'HelmChart', 'HelmValues']::text[])
        UNION
        SELECT repo_id FROM old_rows WHERE entity_type = ANY (ARRAY[
        'K8sResource', 'KustomizeOverlay', 'TerraformResource', 'TerraformModule',
        'TerraformVariable', 'TerraformOutput', 'TerraformDataSource', 'TerraformProvider',
        'TerraformLocal', 'TerraformBackend', 'TerraformImport', 'TerraformMovedBlock',
        'TerraformRemovedBlock', 'TerraformCheck', 'TerraformLockProvider', 'TerraformBlock',
        'TerragruntConfig', 'TerragruntDependency', 'CloudFormationResource',
        'ArgoCDApplication', 'ArgoCDApplicationSet', 'CrossplaneXRD', 'CrossplaneComposition',
        'HelmChart', 'HelmValues']::text[])
    ) AS touched
    ON CONFLICT (repo_id) DO UPDATE SET marked_at = dirty.marked_at WHERE false;
    RETURN NULL;
END;
$$;

-- DELETE is row-level; the trigger's WHEN already filtered the label.
CREATE OR REPLACE FUNCTION infra_resource_entities_mark_dirty_deleted()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO infra_resource_entity_dirty_repos AS dirty (repo_id, marked_at)
    VALUES (OLD.repo_id, clock_timestamp())
    ON CONFLICT (repo_id) DO UPDATE SET marked_at = dirty.marked_at WHERE false;
    RETURN NULL;
END;
$$;

-- TRUNCATE has no row events. After a truncate by anyone every mirrored
-- repository is wrong, so it marks every repository the table holds and is
-- not session-gated. No production path truncates content_entities.
CREATE OR REPLACE FUNCTION infra_resource_entities_mark_all_dirty()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO infra_resource_entity_dirty_repos (repo_id, marked_at)
    SELECT repos.repo_id, clock_timestamp()
    FROM (SELECT DISTINCT repo_id FROM infra_resource_entities) AS repos
    ON CONFLICT (repo_id) DO NOTHING;
    RETURN NULL;
END;
$$;

-- INSERT and UPDATE are statement-level with transition tables: the WHEN is
-- evaluated once per statement and never calls the function in a
-- derive-aware session, and an unaware statement takes each mark lock once
-- instead of once per row. PostgreSQL still captures a transition tuple for
-- every affected row before the WHEN runs, so a derive-aware session pays
-- that copy: at most 0.36 us per row in the fence evidence note
-- (docs/internal/evidence/6793-infra-read-model-fence.md). An INSERT ... ON CONFLICT DO UPDATE fires both: inserted rows land in
-- the INSERT trigger's new_rows, updated rows in the UPDATE trigger's.
-- Transition tables forbid an UPDATE OF column list, so the UPDATE trigger
-- fires on every unaware UPDATE statement and its body filters by label.
DROP TRIGGER IF EXISTS content_entities_infra_dirty_insert ON content_entities;
CREATE TRIGGER content_entities_infra_dirty_insert
AFTER INSERT ON content_entities
REFERENCING NEW TABLE AS new_rows
FOR EACH STATEMENT
WHEN (current_setting('eshu.infra_inventory_writer', true) IS DISTINCT FROM 'derive')
EXECUTE FUNCTION infra_resource_entities_mark_dirty_inserted();

DROP TRIGGER IF EXISTS content_entities_infra_dirty_update ON content_entities;
CREATE TRIGGER content_entities_infra_dirty_update
AFTER UPDATE ON content_entities
REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows
FOR EACH STATEMENT
WHEN (current_setting('eshu.infra_inventory_writer', true) IS DISTINCT FROM 'derive')
EXECUTE FUNCTION infra_resource_entities_mark_dirty_updated();

-- DELETE is row-level: a retention prune deletes tens of thousands of wide
-- rows in one statement, and a transition table would copy every one of them
-- in every session, whereas a false row-level WHEN queues no event. The
-- session test comes first so a derive-aware session never evaluates the
-- label array.
DROP TRIGGER IF EXISTS content_entities_infra_dirty_delete ON content_entities;
CREATE TRIGGER content_entities_infra_dirty_delete
AFTER DELETE ON content_entities
FOR EACH ROW
WHEN (
    current_setting('eshu.infra_inventory_writer', true) IS DISTINCT FROM 'derive'
    AND OLD.entity_type = ANY (ARRAY[
        'K8sResource', 'KustomizeOverlay', 'TerraformResource', 'TerraformModule',
        'TerraformVariable', 'TerraformOutput', 'TerraformDataSource', 'TerraformProvider',
        'TerraformLocal', 'TerraformBackend', 'TerraformImport', 'TerraformMovedBlock',
        'TerraformRemovedBlock', 'TerraformCheck', 'TerraformLockProvider', 'TerraformBlock',
        'TerragruntConfig', 'TerragruntDependency', 'CloudFormationResource',
        'ArgoCDApplication', 'ArgoCDApplicationSet', 'CrossplaneXRD', 'CrossplaneComposition',
        'HelmChart', 'HelmValues']::text[])
)
EXECUTE FUNCTION infra_resource_entities_mark_dirty_deleted();

DROP TRIGGER IF EXISTS content_entities_infra_dirty_truncate ON content_entities;
CREATE TRIGGER content_entities_infra_dirty_truncate
AFTER TRUNCATE ON content_entities
FOR EACH STATEMENT
EXECUTE FUNCTION infra_resource_entities_mark_all_dirty();
