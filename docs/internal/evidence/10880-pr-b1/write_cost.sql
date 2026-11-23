-- #10880 PR-B1: WAL and row cost of the supports writes per generation save, measured
-- in one rolled-back transaction on the scaled corpus. 1,400 findings (40 scopes x 35,
-- the corpus average) are written as a new generation, then the previous generation's
-- facts are retracted (tombstone) and its supports deleted.
\set ON_ERROR_STOP on
\pset format unaligned
\pset tuples_only on
BEGIN;
SELECT pg_current_wal_insert_lsn() AS lsn0 \gset
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT f.fact_id || ':wc', f.scope_id, f.generation_id, f.fact_kind, f.stable_fact_key || ':wc', f.schema_version, f.collector_kind,
       2, f.source_confidence, f.source_system, f.source_fact_key || ':wc', now(), now(), f.payload
FROM fact_records f
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND f.fact_id LIKE 'fact:%'
  AND f.scope_id IN (SELECT scope_id FROM ingestion_scopes WHERE scope_id LIKE 'git-repository-scope:repository:r1%' ORDER BY scope_id LIMIT 40)
  AND f.generation_id = 'gen:' || f.scope_id || ':2';
SELECT pg_current_wal_insert_lsn() AS lsn1 \gset
SELECT count(*) AS facts_written FROM fact_records WHERE fact_id LIKE '%:wc' \gset
\i supports_wc_insert.sql
SELECT pg_current_wal_insert_lsn() AS lsn2 \gset
UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id LIKE '%:wc';
SELECT pg_current_wal_insert_lsn() AS lsn3 \gset
DELETE FROM supply_chain_impact_finding_supports WHERE fact_id LIKE '%:wc';
SELECT pg_current_wal_insert_lsn() AS lsn4 \gset
SELECT :'facts_written' AS findings_written,
       pg_wal_lsn_diff(:'lsn1', :'lsn0') AS wal_bytes_fact_insert,
       pg_wal_lsn_diff(:'lsn2', :'lsn1') AS wal_bytes_supports_insert,
       pg_wal_lsn_diff(:'lsn3', :'lsn2') AS wal_bytes_fact_tombstone,
       pg_wal_lsn_diff(:'lsn4', :'lsn3') AS wal_bytes_supports_delete;
ROLLBACK;
