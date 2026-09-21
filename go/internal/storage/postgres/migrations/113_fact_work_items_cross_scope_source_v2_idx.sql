-- #6785: the completion fanout's consumer-intent lookup index, widened to the
-- value-flow refresh consumer.
--
-- Migration 094 created this index with only the two pre-existing consumers.
-- The fanout reopens the current-generation consumer intents in place, and a
-- partial index only covers the fanout query when its predicate is a subset
-- match of the query's consumer filter, so the refresh consumer
-- (code_value_flow_refresh) needs its own arm here.
--
-- A NEW name (not an in-place predicate change): this directory has no
-- applied-migration ledger, so a same-name drop+create pair would rebuild
-- this index concurrently over fact_work_items on EVERY bootstrap. Migration
-- 114 drops the legacy name once; fresh installs never build it.
--
-- This file holds exactly ONE build statement: the migration runner Execs
-- each file as a single simple-query string and Postgres treats a
-- multi-statement string as an implicit transaction block -- which a
-- concurrent build cannot run inside.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_work_items_cross_scope_source_v2_idx
    ON fact_work_items (
        domain, scope_id, generation_id, status, work_item_id
    )
    WHERE stage = 'reducer'
      AND status IN ('claimed', 'running', 'succeeded')
      AND domain IN ('ci_cd_run_correlation', 'code_value_flow_refresh', 'supply_chain_impact');
