-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #11221 fixture: one scope, one generation, N reducer-shaped fact rows with a
-- payload of about PAYLOAD_BYTES random hex (roughly the replica's measured
-- average for reducer_eshu_search_document, 1.3 KB). Variables (psql -v): rows, payload_chunks.
\set ON_ERROR_STOP on
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
VALUES ('fx-scope', 'repository', 'git', 'fx', 'git', 'p', now(), now(), 'active') ON CONFLICT DO NOTHING;
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('fx-gen', 'fx-scope', 't', now(), now(), 'active') ON CONFLICT DO NOTHING;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'fx-' || i, 'fx-scope', 'fx-gen', 'reducer_eshu_search_document', 'k' || i, '1.0.0', 'reducer', 5,
  'inferred', 'reducer', 'sk' || i, timestamptz '2026-11-01 00:00:00+00', timestamptz '2026-11-01 00:00:00+00', false,
  jsonb_build_object('document_id', i, 'text', (SELECT string_agg(md5(i::text || k::text), '') FROM generate_series(1, :payload_chunks) k))
FROM generate_series(1, :rows) i;
ANALYZE fact_records;
