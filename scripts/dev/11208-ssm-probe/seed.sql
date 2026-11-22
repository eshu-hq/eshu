-- Measurement shim for #11208, not a gate. Builds a disposable fixture.
-- Run: psql -v nscopes=800 -v nfacts=170000 -v naws=100 -f seed.sql
-- Hand-written DDL: only the tables and indexes the SSM host read can use
-- (fact_records_scope_generation_idx, fact_records_collector_status_active_idx,
-- the scope and generation indexes). It is NOT the production schema.
-- Refuse to drop tables this script did not create: an existing ingestion_scopes
-- with rows must carry the fixture marker set at the end of this file.
DO $guard$
BEGIN
  IF to_regclass('ingestion_scopes') IS NOT NULL THEN
    IF coalesce(obj_description('ingestion_scopes'::regclass, 'pg_class'), '') <> 'perf11208 fixture'
       AND EXISTS (SELECT 1 FROM ingestion_scopes) THEN
      RAISE EXCEPTION 'ingestion_scopes holds rows and is not a perf11208 fixture; refusing to drop it';
    END IF;
  END IF;
END
$guard$;
DROP TABLE IF EXISTS fact_records, scope_generations, ingestion_scopes CASCADE;
CREATE TABLE ingestion_scopes (
 scope_id TEXT PRIMARY KEY, scope_kind TEXT NOT NULL, source_system TEXT NOT NULL, source_key TEXT NOT NULL,
 parent_scope_id TEXT NULL, collector_kind TEXT NOT NULL, partition_key TEXT NOT NULL,
 observed_at TIMESTAMPTZ NOT NULL, ingested_at TIMESTAMPTZ NOT NULL, status TEXT NOT NULL,
 active_generation_id TEXT NULL, payload JSONB NOT NULL DEFAULT '{}'::jsonb);
CREATE INDEX ingestion_scopes_source_idx ON ingestion_scopes (source_system, scope_kind, partition_key, observed_at DESC);
CREATE INDEX ingestion_scopes_parent_idx ON ingestion_scopes (parent_scope_id, observed_at DESC);
CREATE UNIQUE INDEX ingestion_scopes_active_generation_idx ON ingestion_scopes (active_generation_id) WHERE active_generation_id IS NOT NULL;
CREATE TABLE scope_generations (
 generation_id TEXT PRIMARY KEY, scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
 trigger_kind TEXT NOT NULL DEFAULT 'snapshot', freshness_hint TEXT NULL,
 observed_at TIMESTAMPTZ NOT NULL, ingested_at TIMESTAMPTZ NOT NULL, status TEXT NOT NULL,
 activated_at TIMESTAMPTZ NULL, superseded_at TIMESTAMPTZ NULL, payload JSONB NOT NULL DEFAULT '{}'::jsonb);
CREATE INDEX scope_generations_scope_idx ON scope_generations (scope_id, status, ingested_at DESC);
CREATE INDEX scope_generations_scope_latest_lookup_idx ON scope_generations (scope_id, ingested_at DESC, generation_id DESC);
CREATE UNIQUE INDEX scope_generations_active_scope_idx ON scope_generations (scope_id) WHERE status = 'active';
CREATE INDEX scope_generations_scope_generation_idx ON scope_generations (scope_id, generation_id) INCLUDE (ingested_at);
CREATE TABLE fact_records (
 fact_id TEXT PRIMARY KEY, scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
 generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
 fact_kind TEXT NOT NULL, stable_fact_key TEXT NOT NULL, schema_version TEXT NOT NULL DEFAULT '0.0.0',
 collector_kind TEXT NOT NULL DEFAULT 'unknown', fencing_token BIGINT NOT NULL DEFAULT 0,
 source_confidence TEXT NOT NULL DEFAULT 'unknown', source_system TEXT NOT NULL, source_fact_key TEXT NOT NULL,
 source_uri TEXT NULL, source_record_id TEXT NULL, observed_at TIMESTAMPTZ NOT NULL, ingested_at TIMESTAMPTZ NOT NULL,
 is_tombstone BOOLEAN NOT NULL DEFAULT FALSE, payload JSONB NOT NULL DEFAULT '{}'::jsonb);
CREATE INDEX fact_records_scope_generation_idx ON fact_records (scope_id, generation_id, fact_kind, observed_at DESC);
CREATE INDEX fact_records_collector_status_active_idx ON fact_records (scope_id, generation_id, source_system, fact_kind, observed_at DESC, ingested_at DESC) WHERE is_tombstone = FALSE;

-- The first :naws scopes are AWS scopes, the rest are git repository scopes.
INSERT INTO ingestion_scopes
SELECT 'scope-' || i, CASE WHEN i <= :naws THEN 'aws_account_region' ELSE 'repository' END,
  CASE WHEN i <= :naws THEN 'aws' ELSE 'git' END, 'key-' || i, NULL,
  CASE WHEN i <= :naws THEN 'aws' ELSE 'git' END, 'p', now(), now(), 'active', 'gen-' || i || '-a', '{}'
FROM generate_series(1, :nscopes) i;
-- One active generation per scope, plus a superseded one for 40% of scopes.
INSERT INTO scope_generations (generation_id, scope_id, observed_at, ingested_at, status, activated_at)
SELECT 'gen-' || i || '-a', 'scope-' || i, now(), now(), 'active', now() FROM generate_series(1, :nscopes) i;
INSERT INTO scope_generations (generation_id, scope_id, observed_at, ingested_at, status, activated_at, superseded_at)
SELECT 'gen-' || i || '-s', 'scope-' || i, now(), now() - interval '1 day', 'superseded', now(), now()
FROM generate_series(1, :nscopes) i WHERE i % 5 < 2;
-- Non-SSM facts in the active generations, then some in the superseded ones.
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'f-' || n, 'scope-' || s, 'gen-' || s || '-a',
  (ARRAY['repository','file','terraform_resource','supply_chain.impact_finding','iac_artifact','content_entity','aws_resource'])[1 + n % 7],
  'k' || n, 'git', 'sk' || n, now() - (n || ' seconds')::interval, now(), jsonb_build_object('n', n, 'repo_id', 'r' || s)
FROM (SELECT n, 1 + (n % :nscopes) AS s FROM generate_series(1, :nfacts) n) q;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'fs-' || n, 'scope-' || s, 'gen-' || s || '-s', 'repository', 'ks' || n, 'git', 'sks' || n, now(), now(), '{}'
FROM (SELECT n, 1 + (n % :nscopes) AS s FROM generate_series(1, :nfacts / 10) n) q WHERE (s % 5) < 2;
COMMENT ON TABLE ingestion_scopes IS 'perf11208 fixture';
VACUUM (ANALYZE) ingestion_scopes, scope_generations, fact_records;
SELECT 'scopes' AS what, count(*) FROM ingestion_scopes
UNION ALL SELECT 'generations', count(*) FROM scope_generations
UNION ALL SELECT 'facts', count(*) FROM fact_records;
