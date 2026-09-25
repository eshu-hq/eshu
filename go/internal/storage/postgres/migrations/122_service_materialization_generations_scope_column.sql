-- 122_service_materialization_generations_scope_column.sql
--
-- #6475: service materialization lineage was keyed by service_id alone, so two
-- ingestion scopes that correlate the same service id shared one lineage: the
-- second scope's commit superseded the first scope's active generation and a
-- changed-since reader could not tell whose evidence it was diffing. This adds
-- the writing intent's ingestion scope as scope_id. The reducer writer sets it
-- on every new generation from here on (ServiceMaterializationWrite.ScopeID).
--
-- Nullable and without a default, so ADD COLUMN is a catalog-only change with
-- no table rewrite. A row stays NULL ("unattributed") when no witness below
-- names its scope; the writer never supersedes an unattributed row, and NULLs
-- are distinct under the (scope_id, service_id) unique index migration 124
-- builds, so a legacy active row never conflicts with a scoped one.
--
-- Backfill witness. A generation's source_intent_id is the reducer intent that
-- wrote it, and a reducer intent's IntentID is the fact_work_items.work_item_id
-- the reducer queue claimed (scanReducerIntent, reducer_queue_helpers.go) --
-- not a shared_projection_intents id, which this domain never uses. That work
-- item row carries the ingestion scope_id (NOT NULL, FK to ingestion_scopes),
-- so it is the one row-specific witness. It is a primary-key lookup, so two
-- witnesses cannot conflict. The domain predicate refuses a work item of any
-- other domain that happens to share the id. When the witness is gone --
-- generation retention deletes a scope generation and its work items cascade
-- -- the row stays NULL rather than being attributed by service-name
-- coincidence or by the current reducer_service_catalog_correlation facts,
-- which describe current ownership, not who wrote this historical generation.
--
-- Idempotent: the UPDATE only touches rows still NULL, so a replay of this
-- file (an untracked database's one full replay, or a retried interrupted
-- apply) re-attributes nothing that is already attributed. Both statements
-- run in one implicit transaction because the runner Execs this file as a
-- single simple-query string; the table holds one row per generation, so the
-- ACCESS EXCLUSIVE lock ADD COLUMN takes is held for a short scan.
ALTER TABLE service_materialization_generations
    ADD COLUMN IF NOT EXISTS scope_id TEXT NULL;

UPDATE service_materialization_generations AS generation
SET scope_id = work.scope_id
FROM fact_work_items AS work
WHERE generation.scope_id IS NULL
  AND generation.source_intent_id IS NOT NULL
  AND work.work_item_id = generation.source_intent_id
  AND work.domain = 'service_catalog_correlation'
  AND work.scope_id <> '';
