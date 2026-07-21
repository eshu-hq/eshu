-- 068_drop_relationship_family_candidate_index_legacy.sql
--
-- Drops the original relationship-family candidate partial index
-- (fact_records_relationship_family_scope_generation_idx). Issue #5483 C2
-- replaced it with fact_records_relationship_family_scope_generation_idx_v2,
-- whose WHERE predicate additionally covers Flux GitRepository file facts, in
-- 059_relationship_family_candidate_index.sql. The v2 index is created (idempotent
-- CREATE ... IF NOT EXISTS) before this migration runs, so there is never a
-- window without a covering index.
--
-- On existing deployments this drops the stale old-predicate index exactly once
-- and on every subsequent boot (and on fresh databases that never created the
-- old name) DROP INDEX CONCURRENTLY IF EXISTS is a no-op, so this adds no
-- per-boot churn. CONCURRENTLY avoids blocking writers, and it is the only
-- statement in this file because CONCURRENTLY DDL cannot run inside the implicit
-- transaction a multi-statement string forms.

DROP INDEX CONCURRENTLY IF EXISTS fact_records_relationship_family_scope_generation_idx;
