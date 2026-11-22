-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- Seeded violations for census_not_takeable.sql: one reducer row of each
-- class, all attributed to source 'census-src'. test_census.sh asserts the
-- census puts exactly one row in each class. Run on a disposable database.
\set ON_ERROR_STOP on
DELETE FROM reducer_waiting_items WHERE work_item_id LIKE 'cens-%';
DELETE FROM fact_work_items WHERE work_item_id LIKE 'cens-%';
DELETE FROM scope_generations WHERE scope_id LIKE 'cens-%';
DELETE FROM ingestion_scopes WHERE scope_id LIKE 'cens-%';

INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
  observed_at, ingested_at, status, active_generation_id)
SELECT 'cens-' || s, 'repository', 'census-src', 'k-' || s, 'git', 'p', now() - interval '3 days', now() - interval '3 days', 'active',
  CASE WHEN s = 'stale' THEN 'cens-stale-g2' ELSE 'cens-' || s || '-g1' END
FROM unnest(ARRAY['take','proj','fence','ready','timer','woken','parked','stale']) AS s;

INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'cens-' || s || '-g1', 'cens-' || s, 'snapshot', now() - interval '3 days', now() - interval '3 days', 'active'
FROM unnest(ARRAY['take','proj','fence','ready','timer','woken','parked']) AS s;
-- the stale scope: g1 is older than the active g2
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status) VALUES
  ('cens-stale-g1', 'cens-stale', 'snapshot', now() - interval '3 days', now() - interval '3 days', 'superseded'),
  ('cens-stale-g2', 'cens-stale', 'snapshot', now() - interval '1 day', now() - interval '1 day', 'active');

-- helper to keep the inserts short
CREATE TEMP TABLE cens_rows (id text, scope text, gen text, domain text, status text, visible_at timestamptz,
  reopened_at timestamptz, stage text, lease_owner text, claim_until timestamptz, payload jsonb);
INSERT INTO cens_rows VALUES
  ('take',   'take',   'take-g1',  'eshu_search_document', 'pending',  NULL, NULL, 'reducer', NULL, NULL, '{}'),
  ('proj',   'proj',   'proj-g1',  'eshu_search_document', 'pending',  NULL, NULL, 'reducer', NULL, NULL, '{}'),
  ('projq',  'proj',   'proj-g1',  'source_local',        'pending',  NULL, NULL, 'projector', NULL, NULL, '{}'),
  ('fence',  'fence',  'fence-g1', 'eshu_search_document', 'pending',  NULL, NULL, 'reducer', NULL, NULL, '{}'),
  ('fenceh', 'fence',  'fence-g1', 'deployment_mapping',   'claimed',  NULL, NULL, 'reducer', 'holder', now() + interval '1 hour', '{}'),
  ('ready',  'ready',  'ready-g1', 'aws_relationship_materialization', 'pending', NULL, NULL, 'reducer', NULL, NULL, '{"entity_key":"e1"}'),
  ('timer',  'timer',  'timer-g1', 'eshu_search_document', 'retrying', now() + interval '1 hour', NULL, 'reducer', NULL, NULL, '{}'),
  ('woken',  'woken',  'woken-g1', 'eshu_search_document', 'retrying', now() - interval '1 minute', now() - interval '5 minutes', 'reducer', NULL, NULL, '{}'),
  ('parked', 'parked', 'parked-g1','eshu_search_document', 'retrying', now() + interval '10 minutes', NULL, 'reducer', NULL, NULL, '{}'),
  ('stale',  'stale',  'stale-g1', 'eshu_search_document', 'pending',  NULL, NULL, 'reducer', NULL, NULL, '{}');
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, status,
  attempt_count, lease_owner, claim_until, visible_at, reopened_at, payload, created_at, updated_at, last_attempt_at)
SELECT 'cens-' || id, 'cens-' || scope, 'cens-' || gen, stage, domain, 'scope', status,
  CASE WHEN status = 'claimed' THEN 1 ELSE 0 END, lease_owner, claim_until, visible_at, reopened_at,
  payload || '{"source_system":"census-src"}'::jsonb, now() - interval '2 hours', now() - interval '2 hours',
  CASE WHEN status = 'claimed' THEN now() ELSE NULL END
FROM cens_rows;
INSERT INTO reducer_waiting_items (work_item_id, waits_for_kind, waits_for_key, scope_id, generation_id, domain,
  failure_class, first_deferred_at, updated_at)
SELECT 'cens-' || id, 'phase_committed', '{"k":"v"}'::jsonb, 'cens-' || scope, 'cens-' || gen, domain,
  'reducer_wait', now() - interval '1 hour', now() - interval '1 hour'
FROM cens_rows WHERE id IN ('woken', 'parked');
