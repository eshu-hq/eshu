CREATE TABLE ingestion_scopes (
  scope_id text PRIMARY KEY,
  scope_kind text,
  active_generation_id text,
  payload jsonb
);
CREATE TABLE scope_generations (
  generation_id text PRIMARY KEY,
  status text
);
CREATE TABLE fact_records (
  fact_id text PRIMARY KEY,
  scope_id text,
  generation_id text,
  fact_kind text,
  stable_fact_key text,
  schema_version text,
  collector_kind text,
  fencing_token bigint,
  source_confidence text,
  source_system text,
  source_fact_key text,
  observed_at timestamptz,
  ingested_at timestamptz,
  is_tombstone boolean,
  payload jsonb
);
INSERT INTO scope_generations VALUES ('generation-one', 'active');
INSERT INTO ingestion_scopes VALUES (
  'scope-one', 'repository', 'generation-one', '{"name":"orders-api"}'
);
