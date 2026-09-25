-- 124_service_materialization_generations_active_service_idx_v2.sql
--
-- #6475: rebuilds service_materialization_generations_active_service_idx on
-- (scope_id, service_id) for active rows. It permits one active generation per
-- service id PER INGESTION SCOPE, which is the writer's conflict key since
-- #6475 (PostgresServiceMaterializationWriter). NULL scope_id rows -- legacy
-- generations no witness could attribute (migration 122) -- are distinct under
-- a unique index, so they never block a scoped active row.
--
-- The name is the one migration 025 creates; migration 123 drops 025's
-- definition first, and IF NOT EXISTS makes this a no-op on every replay once
-- the rescoped definition exists (see 123 for why the name is reused).
--
-- CONCURRENTLY, and the lone statement in this file, because the runner Execs
-- each file as one simple-query string and a multi-statement string is an
-- implicit transaction, which CONCURRENTLY cannot run inside. A failed build
-- leaves an INVALID index that the schema apply path drops by name before
-- re-executing (SQLDB.dropInvalidConcurrentIndexes, adapters.go).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS service_materialization_generations_active_service_idx
    ON service_materialization_generations (scope_id, service_id)
    WHERE status = 'active';
