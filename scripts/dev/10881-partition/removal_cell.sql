-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #10881 removal cell body, run by removal_bench.sh in ONE session so the
-- per-backend WAL counters cover exactly the delete and the VACUUM.
-- Variables (psql -v): arr, page, scoped, db, arm, size_sql.
\set ON_ERROR_STOP on
SELECT (SELECT sum(pg_total_relation_size(relid)) FROM pg_partition_tree('fact_records') WHERE isleaf) AS size_before \gset
SELECT wal_bytes AS w0 FROM pg_stat_get_backend_wal(pg_backend_pid()) \gset
SELECT clock_timestamp() AS t0 \gset
CALL drain_generations(:arr, :page, :scoped);
SELECT round(extract(epoch FROM clock_timestamp() - :'t0'::timestamptz)::numeric, 2) AS delete_seconds,
       (SELECT wal_bytes FROM pg_stat_get_backend_wal(pg_backend_pid())) - :w0 AS delete_wal_bytes,
       (SELECT coalesce(sum(rows_deleted), 0) FROM bench_drain_log) AS rows_deleted \gset
SELECT wal_bytes AS w1 FROM pg_stat_get_backend_wal(pg_backend_pid()) \gset
SELECT clock_timestamp() AS t1 \gset
VACUUM (VERBOSE, INDEX_CLEANUP on, PARALLEL 0) fact_records;
SELECT round(extract(epoch FROM clock_timestamp() - :'t1'::timestamptz)::numeric, 2) AS vacuum_seconds,
       (SELECT wal_bytes FROM pg_stat_get_backend_wal(pg_backend_pid())) - :w1 AS vacuum_wal_bytes,
       (SELECT sum(pg_total_relation_size(relid)) FROM pg_partition_tree('fact_records') WHERE isleaf) AS size_after \gset
\echo cell db=:db arm=:arm rows_deleted=:rows_deleted delete_wal_bytes=:delete_wal_bytes vacuum_wal_bytes=:vacuum_wal_bytes size_before=:size_before size_after=:size_after delete_seconds=:delete_seconds vacuum_seconds=:vacuum_seconds
