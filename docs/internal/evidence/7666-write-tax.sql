-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #7666 index write-tax probe. Run against a migrated database twice, once with
-- fact_records_terraform_state_resource_arn_idx and once with it dropped:
--   psql -d <db> -v arm=with_index -f 7666-write-tax.sql
-- Each run CHECKPOINTs, inserts 50,000 terraform_state_resource rows (25% with
-- an ARN) in a rolled-back transaction, and prints the per-statement WAL
-- counters from pg_stat_statements. Compare wal_records; wal_bytes is dominated
-- by full-page images (wal_fpi) and is not comparable across runs.
-- Requires pg_stat_statements in shared_preload_libraries and a superuser.
\set ON_ERROR_STOP on
\pset format unaligned
\pset tuples_only on
SET pg_stat_statements.track = 'top';
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
VALUES ('tax-scope','state_snapshot','terraform_state','tax','terraform_state','tax',now(),now(),'active') ON CONFLICT DO NOTHING;
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('tax-gen','tax-scope','snapshot',now(),now(),'pending') ON CONFLICT DO NOTHING;
\echo arm=:arm
SELECT 'index_present=' || count(*) FROM pg_indexes WHERE indexname = 'fact_records_terraform_state_resource_arn_idx';
\set i 1
CHECKPOINT;
SELECT pg_stat_statements_reset((SELECT oid FROM pg_roles WHERE rolname=current_user), (SELECT oid FROM pg_database WHERE datname=current_database()), 0::bigint) IS NOT NULL AS reset;
BEGIN;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'tax-' || gen_random_uuid(), 'tax-scope', 'tax-gen', 'terraform_state_resource', 'k-' || i || '-' || gen_random_uuid(), 'terraform_state', 'sf-' || i, now(), now(),
       jsonb_build_object('address', 'aws_x.r' || i, 'attributes', CASE WHEN i % 4 = 0 THEN jsonb_build_object('arn', 'arn:aws:s3:::bucket-' || i || '-' || gen_random_uuid()) ELSE '{}'::jsonb END)
FROM generate_series(1, 50000) i;
SELECT 'run wal_bytes=' || wal_bytes || ' wal_records=' || wal_records || ' wal_fpi=' || wal_fpi || ' exec_ms=' || round(total_exec_time::numeric,0) || ' hit=' || shared_blks_hit
FROM pg_stat_statements WHERE dbid=(SELECT oid FROM pg_database WHERE datname=current_database()) AND query LIKE 'INSERT INTO fact_records%' ORDER BY total_exec_time DESC LIMIT 1;
ROLLBACK;
CHECKPOINT;
SELECT pg_stat_statements_reset((SELECT oid FROM pg_roles WHERE rolname=current_user), (SELECT oid FROM pg_database WHERE datname=current_database()), 0::bigint) IS NOT NULL AS reset;
BEGIN;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'tax-' || gen_random_uuid(), 'tax-scope', 'tax-gen', 'terraform_state_resource', 'k-' || i || '-' || gen_random_uuid(), 'terraform_state', 'sf-' || i, now(), now(),
       jsonb_build_object('address', 'aws_x.r' || i, 'attributes', CASE WHEN i % 4 = 0 THEN jsonb_build_object('arn', 'arn:aws:s3:::bucket-' || i || '-' || gen_random_uuid()) ELSE '{}'::jsonb END)
FROM generate_series(1, 50000) i;
SELECT 'run wal_bytes=' || wal_bytes || ' wal_records=' || wal_records || ' wal_fpi=' || wal_fpi || ' exec_ms=' || round(total_exec_time::numeric,0) || ' hit=' || shared_blks_hit
FROM pg_stat_statements WHERE dbid=(SELECT oid FROM pg_database WHERE datname=current_database()) AND query LIKE 'INSERT INTO fact_records%' ORDER BY total_exec_time DESC LIMIT 1;
ROLLBACK;
CHECKPOINT;
SELECT pg_stat_statements_reset((SELECT oid FROM pg_roles WHERE rolname=current_user), (SELECT oid FROM pg_database WHERE datname=current_database()), 0::bigint) IS NOT NULL AS reset;
BEGIN;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'tax-' || gen_random_uuid(), 'tax-scope', 'tax-gen', 'terraform_state_resource', 'k-' || i || '-' || gen_random_uuid(), 'terraform_state', 'sf-' || i, now(), now(),
       jsonb_build_object('address', 'aws_x.r' || i, 'attributes', CASE WHEN i % 4 = 0 THEN jsonb_build_object('arn', 'arn:aws:s3:::bucket-' || i || '-' || gen_random_uuid()) ELSE '{}'::jsonb END)
FROM generate_series(1, 50000) i;
SELECT 'run wal_bytes=' || wal_bytes || ' wal_records=' || wal_records || ' wal_fpi=' || wal_fpi || ' exec_ms=' || round(total_exec_time::numeric,0) || ' hit=' || shared_blks_hit
FROM pg_stat_statements WHERE dbid=(SELECT oid FROM pg_database WHERE datname=current_database()) AND query LIKE 'INSERT INTO fact_records%' ORDER BY total_exec_time DESC LIMIT 1;
ROLLBACK;
