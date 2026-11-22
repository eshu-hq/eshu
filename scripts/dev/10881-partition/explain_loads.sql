-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #10881 AC3: EXPLAIN (ANALYZE, BUFFERS) of the five busiest fact loads, run
-- identically against the undivided and the divided rehearsal databases.
-- Each load runs three times; the third plan is printed (warm). Parameters
-- come from the data (largest scope, a mid scope, a 500-row guard batch), so
-- the same script is valid for any sample size.
--
-- The five loads, and why they are the busiest:
--   L1 projector fact page   listFactsQuery (facts.go): every projection cycle
--   L2 reducer active-facts  scope+generation+kind scan: the replica's top
--                            fact_records statement by total_exec_time
--   L3 unchanged guard       selectUnchangedFactIDs (unchanged_guard.go): every
--                            ingestion batch; joins by fact_id ONLY
--   L4 streaming upsert      upsertFactBatch (facts_streaming.go): every batch
--   L5 cross-scope by kind   keyset page of active facts across scopes ordered
--                            by fact_id (the ListFactsByKind / catalog family)
-- L3 and L4 also run in their divided-table form (scope-aware / target change).
-- Usage: psql -d DB -v divided=0|1 -v giant_scope=ID -v mid_scope=ID -f explain_loads.sql

\set ON_ERROR_STOP on
\timing off
SET max_parallel_workers_per_gather = 0;
SET jit = off;

-- Parameters. giant_scope and mid_scope come from the caller (psql -v), chosen
-- from the census (census_replica.sh output) so discovery does not scan the table:
-- giant = the sample's largest scope, mid = a scope of a few thousand rows.
SELECT active_generation_id AS giant_gen FROM ingestion_scopes WHERE scope_id = :'giant_scope' \gset
SELECT active_generation_id AS mid_gen FROM ingestion_scopes WHERE scope_id = :'mid_scope' \gset
SELECT fact_kind AS mid_kind FROM fact_records
WHERE scope_id = :'mid_scope' AND generation_id = :'mid_gen' AND NOT is_tombstone
GROUP BY 1 ORDER BY count(*) DESC, 1 LIMIT 1 \gset

\echo '== parameters'
SELECT :'giant_scope' giant_scope, :'giant_gen' giant_gen, :'mid_scope' mid_scope, :'mid_kind' mid_kind;

-- A 500-row guard batch: 400 existing identical rows plus 100 unseen ids.
CREATE TEMP TABLE guard_batch AS
SELECT fact_id, fact_kind, stable_fact_key, schema_version, collector_kind, fencing_token,
       source_confidence, source_system, source_fact_key, source_uri, source_record_id,
       is_tombstone, payload
FROM fact_records WHERE scope_id = :'giant_scope' AND generation_id = :'giant_gen'
ORDER BY observed_at, fact_id LIMIT 400;
INSERT INTO guard_batch
SELECT 'unseen-' || g, 'k', 's', '1', 'c', 0, 'x', 'y', 'z', NULL, NULL, false, '{}'::jsonb
FROM generate_series(1, 100) g;
ANALYZE guard_batch;

CREATE TEMP TABLE upsert_batch AS
SELECT fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
       fencing_token, source_confidence, source_system, source_fact_key, source_uri, source_record_id,
       observed_at, ingested_at, is_tombstone,
       payload || '{"bench":"changed"}'::jsonb AS payload
FROM fact_records WHERE scope_id = :'giant_scope' AND generation_id = :'giant_gen'
ORDER BY observed_at, fact_id LIMIT 250;
INSERT INTO upsert_batch
SELECT 'newfact-' || g, :'giant_scope', :'giant_gen', 'k', 's', '1', 'c', 0, 'x', 'y', 'z', NULL, NULL,
       now(), now(), false, '{}'::jsonb
FROM generate_series(1, 250) g;
ANALYZE upsert_batch;

\echo '== L1 projector fact page (giant scope, active gen, first 5000 rows by keyset order)'
\o /dev/null
EXPLAIN (ANALYZE, BUFFERS) SELECT * FROM (SELECT fact_id, scope_id, generation_id, fact_kind, payload FROM fact_records WHERE scope_id = :'giant_scope' AND generation_id = :'giant_gen' ORDER BY observed_at ASC, fact_id ASC) q LIMIT 5000;
EXPLAIN (ANALYZE, BUFFERS) SELECT * FROM (SELECT fact_id, scope_id, generation_id, fact_kind, payload FROM fact_records WHERE scope_id = :'giant_scope' AND generation_id = :'giant_gen' ORDER BY observed_at ASC, fact_id ASC) q LIMIT 5000;
\o
EXPLAIN (ANALYZE, BUFFERS) SELECT * FROM (SELECT fact_id, scope_id, generation_id, fact_kind, payload FROM fact_records WHERE scope_id = :'giant_scope' AND generation_id = :'giant_gen' ORDER BY observed_at ASC, fact_id ASC) q LIMIT 5000;

\echo '== L2 reducer active-facts scan (mid scope, active gen, one kind)'
\o /dev/null
EXPLAIN (ANALYZE, BUFFERS) WITH active_facts AS (SELECT fact.fact_id, fact.scope_id, fact.payload FROM fact_records AS fact WHERE fact.scope_id = :'mid_scope' AND fact.generation_id = :'mid_gen' AND fact.fact_kind = :'mid_kind' AND fact.is_tombstone = false) SELECT fact_id, payload->>'id' FROM active_facts ORDER BY fact_id;
EXPLAIN (ANALYZE, BUFFERS) WITH active_facts AS (SELECT fact.fact_id, fact.scope_id, fact.payload FROM fact_records AS fact WHERE fact.scope_id = :'mid_scope' AND fact.generation_id = :'mid_gen' AND fact.fact_kind = :'mid_kind' AND fact.is_tombstone = false) SELECT fact_id, payload->>'id' FROM active_facts ORDER BY fact_id;
\o
EXPLAIN (ANALYZE, BUFFERS) WITH active_facts AS (SELECT fact.fact_id, fact.scope_id, fact.payload FROM fact_records AS fact WHERE fact.scope_id = :'mid_scope' AND fact.generation_id = :'mid_gen' AND fact.fact_kind = :'mid_kind' AND fact.is_tombstone = false) SELECT fact_id, payload->>'id' FROM active_facts ORDER BY fact_id;

\echo '== L3a unchanged guard, shipped SQL (fact_id only join)'
\o /dev/null
EXPLAIN (ANALYZE, BUFFERS) SELECT v.fact_id FROM guard_batch v JOIN fact_records AS f ON f.fact_id = v.fact_id WHERE f.fact_kind IS NOT DISTINCT FROM v.fact_kind AND f.stable_fact_key IS NOT DISTINCT FROM v.stable_fact_key AND f.schema_version IS NOT DISTINCT FROM v.schema_version AND f.collector_kind IS NOT DISTINCT FROM v.collector_kind AND f.fencing_token IS NOT DISTINCT FROM v.fencing_token AND f.source_confidence IS NOT DISTINCT FROM v.source_confidence AND f.source_system IS NOT DISTINCT FROM v.source_system AND f.source_fact_key IS NOT DISTINCT FROM v.source_fact_key AND f.source_uri IS NOT DISTINCT FROM v.source_uri AND f.source_record_id IS NOT DISTINCT FROM v.source_record_id AND f.is_tombstone IS NOT DISTINCT FROM v.is_tombstone AND f.payload IS NOT DISTINCT FROM v.payload;
\o
EXPLAIN (ANALYZE, BUFFERS) SELECT v.fact_id FROM guard_batch v JOIN fact_records AS f ON f.fact_id = v.fact_id WHERE f.fact_kind IS NOT DISTINCT FROM v.fact_kind AND f.stable_fact_key IS NOT DISTINCT FROM v.stable_fact_key AND f.schema_version IS NOT DISTINCT FROM v.schema_version AND f.collector_kind IS NOT DISTINCT FROM v.collector_kind AND f.fencing_token IS NOT DISTINCT FROM v.fencing_token AND f.source_confidence IS NOT DISTINCT FROM v.source_confidence AND f.source_system IS NOT DISTINCT FROM v.source_system AND f.source_fact_key IS NOT DISTINCT FROM v.source_fact_key AND f.source_uri IS NOT DISTINCT FROM v.source_uri AND f.source_record_id IS NOT DISTINCT FROM v.source_record_id AND f.is_tombstone IS NOT DISTINCT FROM v.is_tombstone AND f.payload IS NOT DISTINCT FROM v.payload;

\echo '== L3b unchanged guard, scope-aware form (divided PK is (scope_id, fact_id)); batch scope known at the call'
\o /dev/null
EXPLAIN (ANALYZE, BUFFERS) SELECT v.fact_id FROM guard_batch v JOIN fact_records AS f ON f.scope_id = :'giant_scope' AND f.fact_id = v.fact_id WHERE f.fact_kind IS NOT DISTINCT FROM v.fact_kind AND f.stable_fact_key IS NOT DISTINCT FROM v.stable_fact_key AND f.payload IS NOT DISTINCT FROM v.payload;
\o
EXPLAIN (ANALYZE, BUFFERS) SELECT v.fact_id FROM guard_batch v JOIN fact_records AS f ON f.scope_id = :'giant_scope' AND f.fact_id = v.fact_id WHERE f.fact_kind IS NOT DISTINCT FROM v.fact_kind AND f.stable_fact_key IS NOT DISTINCT FROM v.stable_fact_key AND f.payload IS NOT DISTINCT FROM v.payload;

\echo '== L4 streaming upsert (250 changed existing + 250 new), rolled back'
BEGIN;
\if :divided
EXPLAIN (ANALYZE, BUFFERS) INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind, fencing_token, source_confidence, source_system, source_fact_key, source_uri, source_record_id, observed_at, ingested_at, is_tombstone, payload) SELECT * FROM upsert_batch ON CONFLICT (scope_id, fact_id) DO UPDATE SET payload = EXCLUDED.payload, ingested_at = EXCLUDED.ingested_at WHERE fact_records.fencing_token <= EXCLUDED.fencing_token AND fact_records.payload IS DISTINCT FROM EXCLUDED.payload;
\else
EXPLAIN (ANALYZE, BUFFERS) INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind, fencing_token, source_confidence, source_system, source_fact_key, source_uri, source_record_id, observed_at, ingested_at, is_tombstone, payload) SELECT * FROM upsert_batch ON CONFLICT (fact_id) DO UPDATE SET payload = EXCLUDED.payload, ingested_at = EXCLUDED.ingested_at WHERE fact_records.fencing_token <= EXCLUDED.fencing_token AND fact_records.payload IS DISTINCT FROM EXCLUDED.payload;
\endif
ROLLBACK;

\echo '== L5 cross-scope active facts by kind, keyset page of 500 ordered by fact_id (catalog / ListFactsByKind family)'
\o /dev/null
EXPLAIN (ANALYZE, BUFFERS) SELECT fact.fact_id, fact.payload FROM fact_records AS fact JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id AND scope.active_generation_id = fact.generation_id JOIN scope_generations AS generation ON generation.scope_id = fact.scope_id AND generation.generation_id = fact.generation_id WHERE fact.fact_kind = 'repository' AND fact.is_tombstone = false AND generation.status = 'active' ORDER BY fact.fact_id ASC LIMIT 500;
\o
EXPLAIN (ANALYZE, BUFFERS) SELECT fact.fact_id, fact.payload FROM fact_records AS fact JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id AND scope.active_generation_id = fact.generation_id JOIN scope_generations AS generation ON generation.scope_id = fact.scope_id AND generation.generation_id = fact.generation_id WHERE fact.fact_kind = 'repository' AND fact.is_tombstone = false AND generation.status = 'active' ORDER BY fact.fact_id ASC LIMIT 500;
