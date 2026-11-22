-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #11221: WAL and tuple-version cost of one re-save of fixture rows, per arm, in one
-- session (per-backend WAL via pg_stat_get_backend_wal). Variables (psql -v): arm.
--   shipped_new_ts   the reducer batch upsert as shipped: new observed_at and ingested_at,
--                    only the fencing guard (batch_insert.go BatchInsertConflict)
--   shipped_same_ts  the same statement with the stored timestamps
--   guarded_new_ts   the same plus the collector path's NULL-safe content guard, timestamps
--                    left out of the comparison (facts_streaming.go upsertFactBatchSuffix)
--   changed_guarded  guarded statement, payload really changed
--   fresh_insert     new fact ids, for the insert cost
\set ON_ERROR_STOP on
SELECT pg_stat_force_next_flush();
SELECT n_tup_upd AS u0, n_tup_hot_upd AS h0, n_tup_ins AS i0 FROM pg_stat_user_tables WHERE relname = 'fact_records' \gset
SELECT wal_records AS r0, wal_fpi AS f0, wal_bytes AS w0 FROM pg_stat_get_backend_wal(pg_backend_pid()) \gset
\if :{?changed}
\else
\set changed 0
\endif
CREATE TEMP TABLE incoming AS
SELECT CASE WHEN :'arm' = 'fresh_insert' THEN 'new-' || fact_id ELSE fact_id END AS fact_id, scope_id, generation_id, fact_kind,
  stable_fact_key, schema_version, collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
  source_uri, source_record_id,
  CASE WHEN :'arm' IN ('shipped_same_ts') THEN observed_at ELSE now() END AS observed_at,
  CASE WHEN :'arm' IN ('shipped_same_ts') THEN ingested_at ELSE now() END AS ingested_at,
  is_tombstone,
  CASE WHEN :'arm' = 'changed_guarded' THEN payload || '{"changed": true}'::jsonb ELSE payload END AS payload
FROM fact_records WHERE fact_id LIKE 'fx-%';
SELECT wal_bytes AS w1 FROM pg_stat_get_backend_wal(pg_backend_pid()) \gset
\if :{?x}
\endif
SELECT :'arm' IN ('guarded_new_ts', 'changed_guarded') AS guard \gset
\if :guard
INSERT INTO fact_records AS t (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, source_uri, source_record_id, observed_at, ingested_at,
  is_tombstone, payload)
SELECT * FROM incoming
ON CONFLICT (fact_id) DO UPDATE SET fact_kind = EXCLUDED.fact_kind, stable_fact_key = EXCLUDED.stable_fact_key,
  schema_version = EXCLUDED.schema_version, collector_kind = EXCLUDED.collector_kind, source_confidence = EXCLUDED.source_confidence,
  source_system = EXCLUDED.source_system, source_fact_key = EXCLUDED.source_fact_key, source_uri = EXCLUDED.source_uri,
  source_record_id = EXCLUDED.source_record_id, observed_at = EXCLUDED.observed_at, ingested_at = EXCLUDED.ingested_at,
  is_tombstone = EXCLUDED.is_tombstone, payload = EXCLUDED.payload, fencing_token = EXCLUDED.fencing_token
WHERE t.fencing_token <= EXCLUDED.fencing_token
  AND (t.fact_kind IS DISTINCT FROM EXCLUDED.fact_kind OR t.stable_fact_key IS DISTINCT FROM EXCLUDED.stable_fact_key
    OR t.schema_version IS DISTINCT FROM EXCLUDED.schema_version OR t.collector_kind IS DISTINCT FROM EXCLUDED.collector_kind
    OR t.fencing_token IS DISTINCT FROM EXCLUDED.fencing_token OR t.source_confidence IS DISTINCT FROM EXCLUDED.source_confidence
    OR t.source_system IS DISTINCT FROM EXCLUDED.source_system OR t.source_fact_key IS DISTINCT FROM EXCLUDED.source_fact_key
    OR t.source_uri IS DISTINCT FROM EXCLUDED.source_uri OR t.source_record_id IS DISTINCT FROM EXCLUDED.source_record_id
    OR t.is_tombstone IS DISTINCT FROM EXCLUDED.is_tombstone OR t.payload IS DISTINCT FROM EXCLUDED.payload);
\else
INSERT INTO fact_records AS t (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, source_uri, source_record_id, observed_at, ingested_at,
  is_tombstone, payload)
SELECT * FROM incoming
ON CONFLICT (fact_id) DO UPDATE SET fact_kind = EXCLUDED.fact_kind, stable_fact_key = EXCLUDED.stable_fact_key,
  schema_version = EXCLUDED.schema_version, collector_kind = EXCLUDED.collector_kind, source_confidence = EXCLUDED.source_confidence,
  source_system = EXCLUDED.source_system, source_fact_key = EXCLUDED.source_fact_key, source_uri = EXCLUDED.source_uri,
  source_record_id = EXCLUDED.source_record_id, observed_at = EXCLUDED.observed_at, ingested_at = EXCLUDED.ingested_at,
  is_tombstone = EXCLUDED.is_tombstone, payload = EXCLUDED.payload, fencing_token = EXCLUDED.fencing_token
WHERE t.fencing_token <= EXCLUDED.fencing_token;
\endif
SELECT pg_stat_force_next_flush();
SELECT (SELECT wal_bytes FROM pg_stat_get_backend_wal(pg_backend_pid())) - :w1 AS wal_bytes,
       (SELECT wal_records FROM pg_stat_get_backend_wal(pg_backend_pid())) - :r0 AS wal_records,
       (SELECT wal_fpi FROM pg_stat_get_backend_wal(pg_backend_pid())) - :f0 AS wal_fpi,
       (SELECT n_tup_upd FROM pg_stat_user_tables WHERE relname = 'fact_records') - :u0 AS tup_upd,
       (SELECT n_tup_hot_upd FROM pg_stat_user_tables WHERE relname = 'fact_records') - :h0 AS tup_hot_upd,
       (SELECT n_tup_ins FROM pg_stat_user_tables WHERE relname = 'fact_records') - :i0 AS tup_ins,
       (SELECT count(*) FROM incoming) AS incoming_rows \gset
\echo arm=:arm incoming_rows=:incoming_rows wal_bytes=:wal_bytes wal_records=:wal_records wal_fpi=:wal_fpi tup_upd=:tup_upd tup_hot_upd=:tup_hot_upd tup_ins=:tup_ins
