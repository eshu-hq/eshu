-- Reproduce the #5275 findings and free-text index decisions on PostgreSQL 18.
-- Run only in a newly created disposable database whose name starts with
-- eshu_5275_. The script refuses every other database name.
\pset pager off
\timing on
\set ON_ERROR_STOP on

SELECT current_database() LIKE 'eshu_5275_%' AS eshu_5275_disposable \gset
\if :eshu_5275_disposable
\else
  \echo 'refusing non-disposable database; expected an eshu_5275_ prefix'
  SELECT 1 / 0 AS refusing_non_disposable_database;
\endif

SHOW server_version;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS btree_gin;

CREATE TABLE ingestion_scopes (
  scope_id TEXT PRIMARY KEY,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE TABLE fact_records (
  fact_id TEXT PRIMARY KEY,
  scope_id TEXT NOT NULL,
  generation_id TEXT NOT NULL,
  fact_kind TEXT NOT NULL,
  stable_fact_key TEXT NOT NULL,
  schema_version TEXT NOT NULL DEFAULT '0.0.0',
  collector_kind TEXT NOT NULL DEFAULT 'unknown',
  fencing_token BIGINT NOT NULL DEFAULT 0,
  source_confidence TEXT NOT NULL DEFAULT 'unknown',
  source_system TEXT NOT NULL,
  source_fact_key TEXT NOT NULL,
  source_uri TEXT NULL,
  source_record_id TEXT NULL,
  observed_at TIMESTAMPTZ NOT NULL,
  ingested_at TIMESTAMPTZ NOT NULL,
  is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX fact_records_scope_generation_idx
  ON fact_records (scope_id, generation_id, fact_kind, observed_at DESC);
CREATE INDEX fact_records_documentation_sources_observed_idx
  ON fact_records (observed_at DESC, fact_id DESC)
  WHERE fact_kind = 'documentation_source' AND is_tombstone = FALSE;
INSERT INTO ingestion_scopes(scope_id, payload)
VALUES ('scope:findings-proof', '{"repo":"proof"}'),
       ('scope:largest-search-proof', '{"repo":"proof"}');

\echo FINDINGS_SEED_200000
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  'finding:' || lpad(n::text, 12, '0'),
  'scope:findings-proof', 'generation:findings-proof',
  'documentation_finding', 'finding:' || n,
  'proof', 'proof', 'finding:' || n,
  timestamptz '2026-01-01 00:00:00+00' + n * interval '1 second',
  timestamptz '2026-01-01 00:00:00+00' + n * interval '1 second',
  jsonb_build_object(
    'finding_type', CASE WHEN n % 3000 = 0 THEN 'target_type' ELSE 'other_type_' || n % 17 END,
    'source_id', CASE WHEN n % 3000 = 0 THEN 'target_source' ELSE 'other_source_' || n % 23 END,
    'document_id', CASE WHEN n % 3000 = 0 THEN 'target_document' ELSE 'other_document_' || n % 29 END,
    'status', CASE WHEN n % 3000 = 0 THEN 'open' ELSE 'closed' END,
    'truth_level', CASE WHEN n % 3000 = 0 THEN 'observed' ELSE 'inferred' END,
    'freshness_state', CASE WHEN n % 3000 = 0 THEN 'fresh' ELSE 'stale' END,
    'permissions', jsonb_build_object(
      'viewer_can_read_source', n % 20 = 0,
      'source_acl_evaluated', n % 20 = 0
    ),
    'states', jsonb_build_object(
      'permission_decision', CASE WHEN n % 20 = 0 THEN 'allowed' ELSE 'denied' END
    )
  )
FROM generate_series(1, 200000) AS n;

CREATE INDEX fact_records_documentation_findings_visible_idx
ON fact_records (
  (payload->>'finding_type'), (payload->>'source_id'),
  (payload->>'document_id'), (payload->>'status'),
  (payload->>'truth_level'), (payload->>'freshness_state'),
  observed_at DESC, fact_id DESC
)
WHERE fact_kind = 'documentation_finding'
  AND is_tombstone = FALSE
  AND (payload->'permissions'->>'viewer_can_read_source') = 'true'
  AND LOWER(COALESCE(payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'
  AND LOWER(COALESCE(payload->'states'->>'permission_decision', '')) <> 'denied';
ANALYZE fact_records;

PREPARE findings_unfiltered_read AS
SELECT fact_records.payload
  || jsonb_build_object('scope_id', fact_records.scope_id, 'generation_id', fact_records.generation_id)
  || CASE WHEN ingestion_scopes.payload ? 'repo'
          THEN jsonb_build_object('repo', ingestion_scopes.payload->>'repo')
          ELSE '{}'::jsonb END AS payload
FROM fact_records
LEFT JOIN ingestion_scopes ON ingestion_scopes.scope_id = fact_records.scope_id
WHERE fact_records.fact_kind = 'documentation_finding'
  AND fact_records.is_tombstone = FALSE
ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC
LIMIT 51 OFFSET 0;

\echo FINDINGS_UNFILTERED_BASELINE_RESULT
EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT) EXECUTE findings_unfiltered_read;
SELECT count(*) AS rows,
       md5(string_agg(fact_id, '|' ORDER BY observed_at DESC, fact_id DESC)) AS digest
FROM (
  SELECT fact_id, observed_at
  FROM fact_records
  WHERE fact_kind = 'documentation_finding' AND is_tombstone = FALSE
  ORDER BY observed_at DESC, fact_id DESC
  LIMIT 51 OFFSET 0
) AS selected;

PREPARE findings_read AS
SELECT fact_records.payload
  || jsonb_build_object('scope_id', fact_records.scope_id, 'generation_id', fact_records.generation_id)
  || CASE WHEN ingestion_scopes.payload ? 'repo'
          THEN jsonb_build_object('repo', ingestion_scopes.payload->>'repo')
          ELSE '{}'::jsonb END AS payload
FROM fact_records
LEFT JOIN ingestion_scopes ON ingestion_scopes.scope_id = fact_records.scope_id
WHERE fact_records.fact_kind = 'documentation_finding'
  AND fact_records.is_tombstone = FALSE
  AND fact_records.payload->>'finding_type' = 'target_type'
  AND fact_records.payload->>'source_id' = 'target_source'
  AND fact_records.payload->>'document_id' = 'target_document'
  AND fact_records.payload->>'status' = 'open'
  AND fact_records.payload->>'truth_level' = 'observed'
  AND fact_records.payload->>'freshness_state' = 'fresh'
ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC
LIMIT 67 OFFSET 0;

\echo FINDINGS_BASELINE_RESULT
SELECT count(*) AS rows,
       md5(string_agg(fact_id, '|' ORDER BY observed_at DESC, fact_id DESC)) AS digest
FROM fact_records
WHERE fact_kind = 'documentation_finding' AND is_tombstone = FALSE
  AND payload->>'finding_type' = 'target_type'
  AND payload->>'source_id' = 'target_source'
  AND payload->>'document_id' = 'target_document'
  AND payload->>'status' = 'open'
  AND payload->>'truth_level' = 'observed'
  AND payload->>'freshness_state' = 'fresh';
EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT) EXECUTE findings_read;

CREATE INDEX fact_records_documentation_findings_read_idx
ON fact_records (observed_at DESC, fact_id DESC)
WHERE fact_kind = 'documentation_finding' AND is_tombstone = FALSE;
ANALYZE fact_records;

\echo FINDINGS_UNFILTERED_CORRECTED_RESULT
EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT) EXECUTE findings_unfiltered_read;
SELECT count(*) AS rows,
       md5(string_agg(fact_id, '|' ORDER BY observed_at DESC, fact_id DESC)) AS digest
FROM (
  SELECT fact_id, observed_at
  FROM fact_records
  WHERE fact_kind = 'documentation_finding' AND is_tombstone = FALSE
  ORDER BY observed_at DESC, fact_id DESC
  LIMIT 51 OFFSET 0
) AS selected;
DEALLOCATE findings_unfiltered_read;

\echo FINDINGS_FILTERED_ORDER_ONLY_RESULT
EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT) EXECUTE findings_read;

CREATE INDEX fact_records_documentation_findings_filter_idx
ON fact_records (
  (payload->>'finding_type'), (payload->>'source_id'),
  (payload->>'document_id'), (payload->>'status'),
  (payload->>'truth_level'), (payload->>'freshness_state'),
  observed_at DESC, fact_id DESC
)
WHERE fact_kind = 'documentation_finding' AND is_tombstone = FALSE;
ANALYZE fact_records;

\echo FINDINGS_CORRECTED_RESULT
EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT) EXECUTE findings_read;
SELECT count(*) AS rows,
       md5(string_agg(fact_id, '|' ORDER BY observed_at DESC, fact_id DESC)) AS digest
FROM fact_records
WHERE fact_kind = 'documentation_finding' AND is_tombstone = FALSE
  AND payload->>'finding_type' = 'target_type'
  AND payload->>'source_id' = 'target_source'
  AND payload->>'document_id' = 'target_document'
  AND payload->>'status' = 'open'
  AND payload->>'truth_level' = 'observed'
  AND payload->>'freshness_state' = 'fresh';
DEALLOCATE findings_read;

\echo AGGREGATE_FINAL_INDEX_PLAN_AND_RESULT
VACUUM (ANALYZE) fact_records;
PREPARE aggregate_total AS
SELECT COUNT(*) AS total
FROM fact_records
WHERE fact_kind = 'documentation_finding'
  AND is_tombstone = FALSE
  AND (payload->'permissions'->>'viewer_can_read_source') = 'true'
  AND LOWER(COALESCE(payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'
  AND LOWER(COALESCE(payload->'states'->>'permission_decision', '')) <> 'denied';
PREPARE aggregate_status AS
SELECT COALESCE(NULLIF(payload->>'status', ''), 'unknown') AS bucket,
       COUNT(*) AS bucket_count
FROM fact_records
WHERE fact_kind = 'documentation_finding'
  AND is_tombstone = FALSE
  AND (payload->'permissions'->>'viewer_can_read_source') = 'true'
  AND LOWER(COALESCE(payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'
  AND LOWER(COALESCE(payload->'states'->>'permission_decision', '')) <> 'denied'
GROUP BY bucket;
CREATE TEMP TABLE aggregate_final_result AS
SELECT 'total'::text AS dimension, 'all'::text AS bucket,
       COUNT(*) AS bucket_count
FROM fact_records
WHERE fact_kind = 'documentation_finding'
  AND is_tombstone = FALSE
  AND (payload->'permissions'->>'viewer_can_read_source') = 'true'
  AND LOWER(COALESCE(payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'
  AND LOWER(COALESCE(payload->'states'->>'permission_decision', '')) <> 'denied'
UNION ALL
SELECT 'status', COALESCE(NULLIF(payload->>'status', ''), 'unknown'), COUNT(*)
FROM fact_records
WHERE fact_kind = 'documentation_finding'
  AND is_tombstone = FALSE
  AND (payload->'permissions'->>'viewer_can_read_source') = 'true'
  AND LOWER(COALESCE(payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'
  AND LOWER(COALESCE(payload->'states'->>'permission_decision', '')) <> 'denied'
GROUP BY 2;
EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT) EXECUTE aggregate_total;
EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT) EXECUTE aggregate_status;

DROP INDEX fact_records_documentation_findings_visible_idx;
VACUUM (ANALYZE) fact_records;

\echo AGGREGATE_BROAD_ONLY_PLAN_AND_RESULT
CREATE TEMP TABLE aggregate_broad_only_result AS
SELECT 'total'::text AS dimension, 'all'::text AS bucket,
       COUNT(*) AS bucket_count
FROM fact_records
WHERE fact_kind = 'documentation_finding'
  AND is_tombstone = FALSE
  AND (payload->'permissions'->>'viewer_can_read_source') = 'true'
  AND LOWER(COALESCE(payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'
  AND LOWER(COALESCE(payload->'states'->>'permission_decision', '')) <> 'denied'
UNION ALL
SELECT 'status', COALESCE(NULLIF(payload->>'status', ''), 'unknown'), COUNT(*)
FROM fact_records
WHERE fact_kind = 'documentation_finding'
  AND is_tombstone = FALSE
  AND (payload->'permissions'->>'viewer_can_read_source') = 'true'
  AND LOWER(COALESCE(payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'
  AND LOWER(COALESCE(payload->'states'->>'permission_decision', '')) <> 'denied'
GROUP BY 2;
EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT) EXECUTE aggregate_total;
EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT) EXECUTE aggregate_status;

\echo AGGREGATE_EXACT_EQUIVALENCE
SELECT
  (SELECT COUNT(*) FROM (
    (SELECT * FROM aggregate_final_result
     EXCEPT SELECT * FROM aggregate_broad_only_result)
    UNION ALL
    (SELECT * FROM aggregate_broad_only_result
     EXCEPT SELECT * FROM aggregate_final_result)
  ) AS diff) AS symmetric_difference_rows,
  (SELECT md5(string_agg(
    dimension || ':' || bucket || ':' || bucket_count::text,
    '|' ORDER BY dimension, bucket
  )) FROM aggregate_final_result) AS final_digest,
  (SELECT md5(string_agg(
    dimension || ':' || bucket || ':' || bucket_count::text,
    '|' ORDER BY dimension, bucket
  )) FROM aggregate_broad_only_result) AS broad_only_digest;
DEALLOCATE aggregate_total;
DEALLOCATE aggregate_status;

DROP INDEX fact_records_documentation_findings_read_idx;
DROP INDEX fact_records_documentation_findings_filter_idx;
TRUNCATE fact_records;

\echo SEARCH_SEED_1600000
WITH oversized AS (
  SELECT string_agg(md5(g::text), '') AS content
  FROM generate_series(1, 400) AS g
)
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  'doc:' || lpad(n::text, 12, '0'),
  'scope:largest-search-proof', 'generation:search-proof',
  (ARRAY[
    'documentation_source', 'documentation_document', 'documentation_section',
    'documentation_link', 'documentation_entity_mention',
    'documentation_claim_candidate', 'semantic.documentation_observation'
  ])[1 + (n % 7)],
  'doc:' || n, 'proof', 'proof', 'doc:' || n,
  timestamptz '2026-02-01 00:00:00+00' + n * interval '1 millisecond',
  timestamptz '2026-02-01 00:00:00+00' + n * interval '1 millisecond',
  jsonb_build_object(
    'display_name', 'Document ' || n,
    'title', CASE WHEN n % 1000 = 0 THEN 'needle title ' || n ELSE 'ordinary title ' || n END,
    'heading_text', 'Heading ' || n % 100,
    'content', CASE WHEN n = 1 THEN oversized.content
                    ELSE 'documentation content ' || n || ' ' || repeat(md5(n::text) || ' ', 8) END,
    'target_uri', 'https://example.invalid/docs/' || n
  )
FROM generate_series(1, 1600000) AS n
CROSS JOIN oversized;
ANALYZE fact_records;

PREPARE scoped_search AS
SELECT jsonb_build_object(
  'fact_id', fact_records.fact_id,
  'fact_kind', fact_records.fact_kind,
  'scope_id', fact_records.scope_id,
  'generation_id', fact_records.generation_id,
  'source_system', fact_records.source_system,
  'source_uri', fact_records.source_uri,
  'source_record_id', fact_records.source_record_id,
  'observed_at', fact_records.observed_at,
  'payload', fact_records.payload
) AS payload
FROM fact_records
WHERE fact_records.is_tombstone = FALSE
  AND fact_records.fact_kind IN (
    'documentation_source', 'documentation_document', 'documentation_section',
    'documentation_link', 'documentation_entity_mention',
    'documentation_claim_candidate', 'semantic.documentation_observation'
  )
  AND fact_records.scope_id = 'scope:largest-search-proof'
  AND LOWER(
    COALESCE(fact_records.payload->>'display_name', '') || ' ' ||
    COALESCE(fact_records.payload->>'title', '') || ' ' ||
    COALESCE(fact_records.payload->>'heading_text', '') || ' ' ||
    COALESCE(fact_records.payload->>'content', '') || ' ' ||
    COALESCE(fact_records.payload->>'target_uri', '')
  ) LIKE '%needle%'
ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC
LIMIT 51 OFFSET 0;

\echo SEARCH_BASELINE_PLAN_AND_RESULT
EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT) EXECUTE scoped_search;
SELECT md5(string_agg(fact_id, '|' ORDER BY observed_at DESC, fact_id DESC)) AS digest
FROM (
  SELECT fact_id, observed_at
  FROM fact_records
  WHERE is_tombstone = FALSE
    AND fact_kind IN (
      'documentation_source', 'documentation_document', 'documentation_section',
      'documentation_link', 'documentation_entity_mention',
      'documentation_claim_candidate', 'semantic.documentation_observation'
    )
    AND scope_id = 'scope:largest-search-proof'
    AND LOWER(
      COALESCE(payload->>'display_name', '') || ' ' ||
      COALESCE(payload->>'title', '') || ' ' ||
      COALESCE(payload->>'heading_text', '') || ' ' ||
      COALESCE(payload->>'content', '') || ' ' ||
      COALESCE(payload->>'target_uri', '')
    ) LIKE '%needle%'
  ORDER BY observed_at DESC, fact_id DESC
  LIMIT 51 OFFSET 0
) AS selected;
\echo BROAD_GIN_CANDIDATE
CREATE INDEX fact_records_documentation_facts_search_trgm_candidate
ON fact_records USING GIN ((
  LOWER(
    COALESCE(payload->>'display_name', '') || ' ' ||
    COALESCE(payload->>'title', '') || ' ' ||
    COALESCE(payload->>'heading_text', '') || ' ' ||
    COALESCE(payload->>'content', '') || ' ' ||
    COALESCE(payload->>'target_uri', '')
  )
) gin_trgm_ops)
WHERE fact_kind IN (
  'documentation_source', 'documentation_document', 'documentation_section',
  'documentation_link', 'documentation_entity_mention',
  'documentation_claim_candidate', 'semantic.documentation_observation'
) AND is_tombstone = FALSE;
ANALYZE fact_records;
SELECT pg_size_pretty(pg_relation_size(
  'fact_records_documentation_facts_search_trgm_candidate'
)) AS broad_gin_size;
EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT) EXECUTE scoped_search;
SELECT md5(string_agg(fact_id, '|' ORDER BY observed_at DESC, fact_id DESC)) AS digest
FROM (
  SELECT fact_id, observed_at
  FROM fact_records
  WHERE is_tombstone = FALSE
    AND fact_kind IN (
      'documentation_source', 'documentation_document', 'documentation_section',
      'documentation_link', 'documentation_entity_mention',
      'documentation_claim_candidate', 'semantic.documentation_observation'
    )
    AND scope_id = 'scope:largest-search-proof'
    AND LOWER(
      COALESCE(payload->>'display_name', '') || ' ' ||
      COALESCE(payload->>'title', '') || ' ' ||
      COALESCE(payload->>'heading_text', '') || ' ' ||
      COALESCE(payload->>'content', '') || ' ' ||
      COALESCE(payload->>'target_uri', '')
    ) LIKE '%needle%'
  ORDER BY observed_at DESC, fact_id DESC
  LIMIT 51 OFFSET 0
) AS selected;
DROP INDEX fact_records_documentation_facts_search_trgm_candidate;

\echo GIST_CANDIDATE_EXPECTED_FAILURE
\set ON_ERROR_STOP off
CREATE INDEX fact_records_documentation_facts_search_gist_candidate
ON fact_records USING GIST ((
  LOWER(
    COALESCE(payload->>'display_name', '') || ' ' ||
    COALESCE(payload->>'title', '') || ' ' ||
    COALESCE(payload->>'heading_text', '') || ' ' ||
    COALESCE(payload->>'content', '') || ' ' ||
    COALESCE(payload->>'target_uri', '')
  )
) gist_trgm_ops(siglen=64))
WHERE fact_kind IN (
  'documentation_source', 'documentation_document', 'documentation_section',
  'documentation_link', 'documentation_entity_mention',
  'documentation_claim_candidate', 'semantic.documentation_observation'
) AND is_tombstone = FALSE;
\set ON_ERROR_STOP on
DROP INDEX IF EXISTS fact_records_documentation_facts_search_gist_candidate;

\echo SCOPED_GIN_CANDIDATE
CREATE INDEX fact_records_documentation_facts_scope_search_gin_candidate
ON fact_records USING GIN (
  scope_id, fact_kind,
  (LOWER(
    COALESCE(payload->>'display_name', '') || ' ' ||
    COALESCE(payload->>'title', '') || ' ' ||
    COALESCE(payload->>'heading_text', '') || ' ' ||
    COALESCE(payload->>'content', '') || ' ' ||
    COALESCE(payload->>'target_uri', '')
  )) gin_trgm_ops
)
WHERE fact_kind IN (
  'documentation_source', 'documentation_document', 'documentation_section',
  'documentation_link', 'documentation_entity_mention',
  'documentation_claim_candidate', 'semantic.documentation_observation'
) AND is_tombstone = FALSE;
ANALYZE fact_records;
SELECT pg_size_pretty(pg_relation_size(
  'fact_records_documentation_facts_scope_search_gin_candidate'
)) AS scoped_gin_size;
EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT) EXECUTE scoped_search;
SELECT md5(string_agg(fact_id, '|' ORDER BY observed_at DESC, fact_id DESC)) AS digest
FROM (
  SELECT fact_id, observed_at
  FROM fact_records
  WHERE is_tombstone = FALSE
    AND fact_kind IN (
      'documentation_source', 'documentation_document', 'documentation_section',
      'documentation_link', 'documentation_entity_mention',
      'documentation_claim_candidate', 'semantic.documentation_observation'
    )
    AND scope_id = 'scope:largest-search-proof'
    AND LOWER(
      COALESCE(payload->>'display_name', '') || ' ' ||
      COALESCE(payload->>'title', '') || ' ' ||
      COALESCE(payload->>'heading_text', '') || ' ' ||
      COALESCE(payload->>'content', '') || ' ' ||
      COALESCE(payload->>'target_uri', '')
    ) LIKE '%needle%'
  ORDER BY observed_at DESC, fact_id DESC
  LIMIT 51 OFFSET 0
) AS selected;
DEALLOCATE scoped_search;
