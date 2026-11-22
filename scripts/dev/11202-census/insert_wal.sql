-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #11202 box 1: per-row WAL of inserting fact rows, to set beside the removal
-- WAL per row. One session, triggers on, the production INSERT ... ON CONFLICT
-- shape, 100,000 new fact ids copied from one scope's active rows (so the
-- payload sizes are real). Per-backend WAL: pg_stat_get_backend_wal.
-- Variables (psql -v): scope (the source scope id).
\set ON_ERROR_STOP on
SELECT active_generation_id AS gen FROM ingestion_scopes WHERE scope_id = :'scope' \gset
SELECT wal_bytes AS w0 FROM pg_stat_get_backend_wal(pg_backend_pid()) \gset
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, source_uri, source_record_id, observed_at,
  ingested_at, is_tombstone, payload)
SELECT 'insbench-' || fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, source_uri, source_record_id, observed_at,
  now(), is_tombstone, payload
FROM fact_records WHERE scope_id = :'scope' AND generation_id = :'gen' LIMIT 100000
ON CONFLICT (fact_id) DO NOTHING;
SELECT count(*) AS inserted FROM fact_records WHERE fact_id LIKE 'insbench-%' \gset
SELECT (SELECT wal_bytes FROM pg_stat_get_backend_wal(pg_backend_pid())) - :w0 AS insert_wal_bytes \gset
\echo inserted=:inserted insert_wal_bytes=:insert_wal_bytes
