-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
-- #7420 arms: site A F1 (scope, generation) child delete and site B page
-- Run: psql -v gen=gen-target -f 7420-key-sidecar-arms.sql (or gen=gen-stale for stale statistics).
-- candidates, each in BEGIN/ROLLBACK, custom and forced-generic plans.
SET jit = off;
\echo ### A-F1 custom
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) DELETE FROM relationship_reference_candidate_keys WHERE scope_id = 'scope-1' AND generation_id = :'gen'; ROLLBACK;
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) DELETE FROM package_manifest_consumption_keys WHERE scope_id = 'scope-1' AND generation_id = :'gen'; ROLLBACK;
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) DELETE FROM package_registry_identity_keys WHERE scope_id = 'scope-1' AND generation_id = :'gen'; ROLLBACK;
\echo ### A-F1 generic
SET plan_cache_mode = force_generic_plan;
PREPARE a1(text, text) AS DELETE FROM relationship_reference_candidate_keys WHERE scope_id = $1 AND generation_id = $2;
PREPARE a2(text, text) AS DELETE FROM package_manifest_consumption_keys WHERE scope_id = $1 AND generation_id = $2;
PREPARE a3(text, text) AS DELETE FROM package_registry_identity_keys WHERE scope_id = $1 AND generation_id = $2;
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) EXECUTE a1('scope-1', :'gen'); ROLLBACK;
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) EXECUTE a2('scope-1', :'gen'); ROLLBACK;
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) EXECUTE a3('scope-1', :'gen'); ROLLBACK;
RESET plan_cache_mode;
SELECT array_agg(fact_id) AS ids FROM (SELECT fact.fact_id FROM fact_records AS fact WHERE fact.scope_id = 'scope-1' AND fact.generation_id = :'gen' ORDER BY fact.observed_at, fact.fact_id LIMIT 100000) s \gset
\echo ### B1 fact_id = ANY custom
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) DELETE FROM relationship_reference_candidate_keys WHERE fact_id = ANY(:'ids'::text[]); ROLLBACK;
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) DELETE FROM package_manifest_consumption_keys WHERE fact_id = ANY(:'ids'::text[]); ROLLBACK;
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) DELETE FROM package_registry_identity_keys WHERE fact_id = ANY(:'ids'::text[]); ROLLBACK;
\echo ### B2 scoped ANY custom
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) DELETE FROM relationship_reference_candidate_keys WHERE scope_id = 'scope-1' AND generation_id = :'gen' AND fact_id = ANY(:'ids'::text[]); ROLLBACK;
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) DELETE FROM package_manifest_consumption_keys WHERE scope_id = 'scope-1' AND generation_id = :'gen' AND fact_id = ANY(:'ids'::text[]); ROLLBACK;
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) DELETE FROM package_registry_identity_keys WHERE scope_id = 'scope-1' AND generation_id = :'gen' AND fact_id = ANY(:'ids'::text[]); ROLLBACK;
\echo ### B2 scoped ANY generic
SET plan_cache_mode = force_generic_plan;
PREPARE b1(text, text, text[]) AS DELETE FROM relationship_reference_candidate_keys WHERE scope_id = $1 AND generation_id = $2 AND fact_id = ANY($3);
PREPARE b2(text, text, text[]) AS DELETE FROM package_manifest_consumption_keys WHERE scope_id = $1 AND generation_id = $2 AND fact_id = ANY($3);
PREPARE b3(text, text, text[]) AS DELETE FROM package_registry_identity_keys WHERE scope_id = $1 AND generation_id = $2 AND fact_id = ANY($3);
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) EXECUTE b1('scope-1', :'gen', :'ids'); ROLLBACK;
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) EXECUTE b2('scope-1', :'gen', :'ids'); ROLLBACK;
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) EXECUTE b3('scope-1', :'gen', :'ids'); ROLLBACK;
\echo ### B1 fact_id = ANY generic
PREPARE c1(text[]) AS DELETE FROM relationship_reference_candidate_keys WHERE fact_id = ANY($1);
PREPARE c2(text[]) AS DELETE FROM package_manifest_consumption_keys WHERE fact_id = ANY($1);
PREPARE c3(text[]) AS DELETE FROM package_registry_identity_keys WHERE fact_id = ANY($1);
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) EXECUTE c1(:'ids'); ROLLBACK;
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) EXECUTE c2(:'ids'); ROLLBACK;
BEGIN; EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) EXECUTE c3(:'ids'); ROLLBACK;
