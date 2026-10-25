-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8961: sbt dependency rows now join a Maven registry identity, so the
-- package_manifest_consumption_keys sidecar writes keys for them. Rows stored
-- before that change have no keys, and an unchanged repository is never
-- re-collected (the freshness hint folds the unchanged content version), so
-- the sidecar would stay keyless for it.
--
-- This marks exactly the scopes that hold an sbt dependency row dirty. The
-- reducer's existing backfill (storage/postgres/package_manifest_consumption_
-- backfill.go) rebuilds a dirty scope's sidecars, one short transaction per
-- fact page, and clears the fence; readers treat any dirty row as not ready
-- until it does. No marker changes and no other scope is touched.
--
-- Concurrency. ON CONFLICT DO UPDATE writes a new row version when the scope
-- is already dirty, as the dirty trigger does, so a rebuild that fenced the
-- scope before this ran compares xmin on clear and keeps the row. Rows are
-- inserted in scope_id order, the order a statement-level writer takes the
-- row locks in. Re-running the statement is safe: it re-marks the same scopes
-- and the repair converges to the same keys.
--
-- Cost. The scan reads the package_manager = 'sbt' prefix of
-- fact_records_active_package_dependency_entity_idx, so it follows the number
-- of sbt dependency rows, not the size of fact_records. DML only, no table
-- lock beyond ROW EXCLUSIVE on the dirty-scopes table.

INSERT INTO package_manifest_consumption_key_dirty_scopes (scope_id, marked_at)
SELECT d.scope_id, now()
FROM (
    SELECT DISTINCT fact.scope_id
    FROM fact_records AS fact
    WHERE fact.fact_kind = 'content_entity'
      AND fact.source_system = 'git'
      AND fact.payload->>'entity_type' = 'Variable'
      AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
      AND fact.payload->'entity_metadata'->>'package_manager' = 'sbt'
) AS d
ORDER BY d.scope_id
ON CONFLICT (scope_id) DO UPDATE SET marked_at = EXCLUDED.marked_at;
