-- 123_service_materialization_generations_scope_backfill.sql
--
-- #6475: attributes legacy service materialization generations to the
-- ingestion scope that wrote them, using the one row-specific witness.
--
-- A generation's source_intent_id is the reducer intent that wrote it, and a
-- reducer intent's IntentID is the fact_work_items.work_item_id the reducer
-- queue claimed (scanReducerIntent, reducer_queue_helpers.go) -- not a
-- shared_projection_intents id, which this domain never uses. That work item
-- row carries the ingestion scope_id (NOT NULL, FK to ingestion_scopes), so it
-- is the witness. It is a primary-key lookup, so two witnesses cannot
-- conflict. The domain predicate refuses a work item of any other domain that
-- happens to share the id. When the witness is gone -- generation retention
-- deletes a scope generation and its work items cascade -- the row stays NULL
-- rather than being attributed by service-name coincidence or by the current
-- reducer_service_catalog_correlation facts, which describe current ownership,
-- not who wrote this historical generation.
--
-- Idempotent: the UPDATE only touches rows still NULL, so a replay of this
-- file (an untracked database's one full replay, or a retried interrupted
-- apply) re-attributes nothing that is already attributed.
--
-- Locking: this is a file of its own, so the runner's single simple-query
-- string is this one statement in its own implicit transaction. It takes ROW
-- EXCLUSIVE on service_materialization_generations -- readers are not blocked
-- -- and a row lock on each NULL-scope row it attributes, for one pass that
-- probes fact_work_items by primary key per candidate row. The new writer
-- never updates NULL-scope rows, so it does not contend with this statement; a
-- previous-release reducer superseding one of those rows during the upgrade
-- waits on that row lock, bounded by the runner's lock_timeout and retry.
UPDATE service_materialization_generations AS generation
SET scope_id = work.scope_id
FROM fact_work_items AS work
WHERE generation.scope_id IS NULL
  AND generation.source_intent_id IS NOT NULL
  AND work.work_item_id = generation.source_intent_id
  AND work.domain = 'service_catalog_correlation'
  AND work.scope_id <> '';
