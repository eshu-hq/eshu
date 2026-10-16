-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
-- #7420 site A (whole-generation prune) and site B (100k fact pages) in
-- three states: FK present (today), FK present plus explicit child deletes
-- (PR1), FK dropped plus explicit child deletes (PR2). Each arm rolls back.
\set ON_ERROR_STOP on
SET jit = off;
SET pg_stat_statements.track = 'all';
\echo ### A0 FK present, cascade only
SELECT pg_stat_statements_reset() \g /dev/null
BEGIN;
\timing on
DELETE FROM scope_generations WHERE generation_id = ANY('{gen-target}'::text[]);
\timing off
ROLLBACK;
SELECT left(regexp_replace(query, '\s+', ' ', 'g'), 70) AS stmt, calls, rows, shared_blks_hit hit, shared_blks_read rd, wal_bytes, round(total_exec_time::numeric,1) ms FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database()) AND query !~* 'pg_stat_statements|^(BEGIN|ROLLBACK|SET|SELECT pg_stat|ALTER)' ORDER BY total_exec_time DESC LIMIT 12;
\echo ### A1 FK present + F1 child deletes first (PR1)
SELECT pg_stat_statements_reset() \g /dev/null
BEGIN;
\timing on
DELETE FROM relationship_reference_candidate_keys WHERE scope_id = 'scope-1' AND generation_id = 'gen-target';
DELETE FROM package_manifest_consumption_keys WHERE scope_id = 'scope-1' AND generation_id = 'gen-target';
DELETE FROM package_registry_identity_keys WHERE scope_id = 'scope-1' AND generation_id = 'gen-target';
DELETE FROM scope_generations WHERE generation_id = ANY('{gen-target}'::text[]);
\timing off
ROLLBACK;
SELECT left(regexp_replace(query, '\s+', ' ', 'g'), 70) AS stmt, calls, rows, shared_blks_hit hit, shared_blks_read rd, wal_bytes, round(total_exec_time::numeric,1) ms FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database()) AND query !~* 'pg_stat_statements|^(BEGIN|ROLLBACK|SET|SELECT pg_stat|ALTER)' ORDER BY total_exec_time DESC LIMIT 12;
\echo ### A2 FK dropped + F1 child deletes first (PR2)
SELECT pg_stat_statements_reset() \g /dev/null
BEGIN;
ALTER TABLE relationship_reference_candidate_keys DROP CONSTRAINT relationship_reference_candidate_keys_fact_id_fkey;
ALTER TABLE package_manifest_consumption_keys DROP CONSTRAINT package_manifest_consumption_keys_fact_id_fkey;
ALTER TABLE package_registry_identity_keys DROP CONSTRAINT package_registry_identity_keys_fact_id_fkey;
\timing on
DELETE FROM relationship_reference_candidate_keys WHERE scope_id = 'scope-1' AND generation_id = 'gen-target';
DELETE FROM package_manifest_consumption_keys WHERE scope_id = 'scope-1' AND generation_id = 'gen-target';
DELETE FROM package_registry_identity_keys WHERE scope_id = 'scope-1' AND generation_id = 'gen-target';
DELETE FROM scope_generations WHERE generation_id = ANY('{gen-target}'::text[]);
\timing off
ROLLBACK;
SELECT left(regexp_replace(query, '\s+', ' ', 'g'), 70) AS stmt, calls, rows, shared_blks_hit hit, shared_blks_read rd, wal_bytes, round(total_exec_time::numeric,1) ms FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database()) AND query !~* 'pg_stat_statements|^(BEGIN|ROLLBACK|SET|SELECT pg_stat|ALTER)' ORDER BY total_exec_time DESC LIMIT 12;
\echo ### B0 FK present, 100k fact pages by fact_id = ANY only
SELECT pg_stat_statements_reset() \g /dev/null
BEGIN;
DO $$ DECLARE ids text[]; t0 timestamptz; p int := 0; BEGIN
 LOOP
  SELECT array_agg(fact_id) INTO ids FROM (SELECT fact.fact_id FROM fact_records AS fact WHERE fact.scope_id='scope-1' AND fact.generation_id='gen-target' ORDER BY fact.observed_at, fact.fact_id LIMIT 100000) s;
  EXIT WHEN ids IS NULL; p := p+1; t0 := clock_timestamp();
  DELETE FROM fact_records WHERE fact_id = ANY(ids);
  RAISE NOTICE 'B0 page % ms=%', p, round(extract(epoch from clock_timestamp()-t0)*1000);
 END LOOP; END $$;
ROLLBACK;
SELECT left(regexp_replace(query, '\s+', ' ', 'g'), 70) AS stmt, calls, rows, shared_blks_hit hit, shared_blks_read rd, wal_bytes, round(total_exec_time::numeric,1) ms FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database()) AND query !~* 'pg_stat_statements|^(BEGIN|ROLLBACK|SET|SELECT pg_stat|ALTER)' ORDER BY total_exec_time DESC LIMIT 12;
\echo ### B1 FK present + scoped child deletes per page (PR1)
SELECT pg_stat_statements_reset() \g /dev/null
BEGIN;
DO $$ DECLARE ids text[]; t0 timestamptz; p int := 0; BEGIN
 LOOP
  SELECT array_agg(fact_id) INTO ids FROM (SELECT fact.fact_id FROM fact_records AS fact WHERE fact.scope_id='scope-1' AND fact.generation_id='gen-target' ORDER BY fact.observed_at, fact.fact_id LIMIT 100000) s;
  EXIT WHEN ids IS NULL; p := p+1; t0 := clock_timestamp();
  DELETE FROM relationship_reference_candidate_keys WHERE scope_id='scope-1' AND generation_id='gen-target' AND fact_id = ANY(ids);
  DELETE FROM package_manifest_consumption_keys WHERE scope_id='scope-1' AND generation_id='gen-target' AND fact_id = ANY(ids);
  DELETE FROM package_registry_identity_keys WHERE scope_id='scope-1' AND generation_id='gen-target' AND fact_id = ANY(ids);
  DELETE FROM fact_records WHERE fact_id = ANY(ids);
  RAISE NOTICE 'B1 page % ms=%', p, round(extract(epoch from clock_timestamp()-t0)*1000);
 END LOOP; END $$;
ROLLBACK;
SELECT left(regexp_replace(query, '\s+', ' ', 'g'), 70) AS stmt, calls, rows, shared_blks_hit hit, shared_blks_read rd, wal_bytes, round(total_exec_time::numeric,1) ms FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database()) AND query !~* 'pg_stat_statements|^(BEGIN|ROLLBACK|SET|SELECT pg_stat|ALTER)' ORDER BY total_exec_time DESC LIMIT 12;
\echo ### B2 FK dropped + scoped child deletes per page (PR2)
SELECT pg_stat_statements_reset() \g /dev/null
BEGIN;
ALTER TABLE relationship_reference_candidate_keys DROP CONSTRAINT relationship_reference_candidate_keys_fact_id_fkey;
ALTER TABLE package_manifest_consumption_keys DROP CONSTRAINT package_manifest_consumption_keys_fact_id_fkey;
ALTER TABLE package_registry_identity_keys DROP CONSTRAINT package_registry_identity_keys_fact_id_fkey;
DO $$ DECLARE ids text[]; t0 timestamptz; p int := 0; BEGIN
 LOOP
  SELECT array_agg(fact_id) INTO ids FROM (SELECT fact.fact_id FROM fact_records AS fact WHERE fact.scope_id='scope-1' AND fact.generation_id='gen-target' ORDER BY fact.observed_at, fact.fact_id LIMIT 100000) s;
  EXIT WHEN ids IS NULL; p := p+1; t0 := clock_timestamp();
  DELETE FROM relationship_reference_candidate_keys WHERE scope_id='scope-1' AND generation_id='gen-target' AND fact_id = ANY(ids);
  DELETE FROM package_manifest_consumption_keys WHERE scope_id='scope-1' AND generation_id='gen-target' AND fact_id = ANY(ids);
  DELETE FROM package_registry_identity_keys WHERE scope_id='scope-1' AND generation_id='gen-target' AND fact_id = ANY(ids);
  DELETE FROM fact_records WHERE fact_id = ANY(ids);
  RAISE NOTICE 'B2 page % ms=%', p, round(extract(epoch from clock_timestamp()-t0)*1000);
 END LOOP; END $$;
ROLLBACK;
SELECT left(regexp_replace(query, '\s+', ' ', 'g'), 70) AS stmt, calls, rows, shared_blks_hit hit, shared_blks_read rd, wal_bytes, round(total_exec_time::numeric,1) ms FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database()) AND query !~* 'pg_stat_statements|^(BEGIN|ROLLBACK|SET|SELECT pg_stat|ALTER)' ORDER BY total_exec_time DESC LIMIT 12;
