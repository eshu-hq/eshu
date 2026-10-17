-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
-- #7420 PR2 re-measurement of site A (whole-generation prune) and site B
-- (100k fact chunk pages) in the database's current foreign key state, with
-- the shipped statements: the explicit key deletes, then the generation or
-- page fact delete. Each arm rolls back. Run it once before migrations
-- 188-190 (foreign keys present) and once after (dropped for real):
--   psql -v state=fk-present -f 7420-drop-key-sidecar-fks-sites.sql
-- Fixture: 7420-key-sidecar-fixture.sql, 150,000-fact gen-target in scope-1
-- plus gen-2..20-live (3M facts, 5% share).
\set ON_ERROR_STOP on
SET jit = off;
SET pg_stat_statements.track = 'all';
SELECT count(*) AS fact_records_foreign_keys FROM pg_constraint
WHERE contype = 'f' AND confrelid = 'fact_records'::regclass;
\echo ### site A :state
SELECT pg_stat_statements_reset() \g /dev/null
BEGIN;
SELECT clock_timestamp() AS t0 \gset
DELETE FROM relationship_reference_candidate_keys WHERE scope_id = 'scope-1' AND generation_id = 'gen-target';
DELETE FROM package_manifest_consumption_keys WHERE scope_id = 'scope-1' AND generation_id = 'gen-target';
DELETE FROM package_registry_identity_keys WHERE scope_id = 'scope-1' AND generation_id = 'gen-target';
DELETE FROM scope_generations WHERE generation_id = ANY('{gen-target}'::text[]);
SELECT round(extract(epoch FROM clock_timestamp() - :'t0'::timestamptz)::numeric, 3) AS site_a_seconds;
ROLLBACK;
SELECT sum(shared_blks_hit) AS hit, sum(shared_blks_read) AS rd, pg_size_pretty(sum(wal_bytes)) AS wal
FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
  AND toplevel AND query ~* '^DELETE';
SELECT left(regexp_replace(query, '\s+', ' ', 'g'), 60) AS stmt, toplevel, calls, rows, shared_blks_hit hit, shared_blks_read rd,
       wal_bytes, round(total_exec_time::numeric, 1) ms
FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
  AND query ~* 'DELETE' ORDER BY total_exec_time DESC LIMIT 8;
\echo ### site B :state
SELECT pg_stat_statements_reset() \g /dev/null
BEGIN;
DO $$ DECLARE ids text[]; t0 timestamptz; started timestamptz := clock_timestamp(); p int := 0; BEGIN
 LOOP
  SELECT array_agg(fact_id) INTO ids FROM (SELECT fact.fact_id FROM fact_records AS fact
   WHERE fact.scope_id = 'scope-1' AND fact.generation_id = 'gen-target'
   ORDER BY fact.observed_at, fact.fact_id LIMIT 100000) s;
  EXIT WHEN ids IS NULL; p := p + 1; t0 := clock_timestamp();
  DELETE FROM relationship_reference_candidate_keys WHERE scope_id = 'scope-1' AND generation_id = 'gen-target' AND fact_id = ANY(ids);
  DELETE FROM package_manifest_consumption_keys WHERE scope_id = 'scope-1' AND generation_id = 'gen-target' AND fact_id = ANY(ids);
  DELETE FROM package_registry_identity_keys WHERE scope_id = 'scope-1' AND generation_id = 'gen-target' AND fact_id = ANY(ids);
  DELETE FROM fact_records WHERE fact_id = ANY(ids);
  RAISE NOTICE 'site B page % ms=%', p, round(extract(epoch FROM clock_timestamp() - t0) * 1000);
 END LOOP;
 RAISE NOTICE 'site B total seconds=%', round(extract(epoch FROM clock_timestamp() - started)::numeric, 3);
END $$;
ROLLBACK;
SELECT sum(shared_blks_hit) AS hit, sum(shared_blks_read) AS rd, pg_size_pretty(sum(wal_bytes)) AS wal
FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
  AND toplevel AND query ~* '^DO';
SELECT left(regexp_replace(query, '\s+', ' ', 'g'), 60) AS stmt, toplevel, calls, rows, shared_blks_hit hit, shared_blks_read rd,
       wal_bytes, round(total_exec_time::numeric, 1) ms
FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
  AND query ~* 'DELETE' ORDER BY total_exec_time DESC LIMIT 8;
