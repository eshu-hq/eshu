-- 123_service_materialization_generations_active_service_idx_rescope.sql
--
-- #6475: drops the single-active-per-service_id definition of
-- service_materialization_generations_active_service_idx so migration 124 can
-- rebuild the SAME NAME on (scope_id, service_id). Two ingestion scopes may
-- now each hold one active generation for one service id.
--
-- Why the name is reused rather than replaced. Migration 025 is shipped and
-- immutable (migration_checksum_manifest_test.go), and it creates this name
-- with the old (service_id) definition under IF NOT EXISTS, which matches on
-- NAME only. Any database that replays 025 -- a fresh database, or an existing
-- database without eshu_schema_migrations receipts replaying the tree once --
-- would recreate the old definition if the name were free, and once two scopes
-- hold active rows for one service id that recreate FAILS the bootstrap. With
-- the name reused, 025 is a no-op wherever the rescoped index exists.
--
-- Guarded, so it converges once: the drop fires only while the index under
-- this name is the pre-#6475 definition (its indexdef lacks scope_id). A fresh
-- database drops 025's just-built index on an empty table; an upgrading
-- database drops the old definition once; every later replay sees scope_id in
-- the definition and does nothing, so no replay drops or rebuilds the index.
-- TestServiceMaterializationActiveIndexReplayConvergesLive proves that on a
-- populated store.
--
-- Plain DROP INDEX, not CONCURRENTLY: a DO block runs in a transaction, and
-- CONCURRENTLY cannot. The table holds one row per service generation, so the
-- ACCESS EXCLUSIVE lock is brief, and the bootstrap runner bounds its wait
-- with lock_timeout and retries (schema.go). Between this drop and 124's build
-- nothing enforces one active per (scope, service); the writer's supersede and
-- activate run in one transaction, and 124 fails loudly (and is retried) if a
-- duplicate somehow appeared.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_indexes
        WHERE schemaname = current_schema()
          AND tablename = 'service_materialization_generations'
          AND indexname = 'service_materialization_generations_active_service_idx'
          AND indexdef NOT LIKE '%scope_id%'
    ) THEN
        DROP INDEX service_materialization_generations_active_service_idx;
    END IF;
END
$$;
