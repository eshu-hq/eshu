-- 048_drop_graph_projection_phase_state_lookup_idx.sql
--
-- Drops the redundant lookup index on
-- graph_projection_phase_state(scope_id, acceptance_unit_id, source_run_id,
-- generation_id, keyspace, phase). The primary key on the same six columns in
-- the same order already provides the same lookup for readiness-gate queries
-- (lookupGraphProjectionPhaseStateSQL) and reducer claim readiness, while the
-- standalone lookup index adds one more B-tree write for every phase upsert.
--
-- CONCURRENTLY keeps existing reducer writers from blocking while the index is
-- removed on populated deployments. New databases no longer create this index
-- in the embedded Go migration set's 012_graph_projection_phase_state.sql, so
-- this migration is a no-op there.

DROP INDEX CONCURRENTLY IF EXISTS graph_projection_phase_state_lookup_idx;
