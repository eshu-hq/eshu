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
