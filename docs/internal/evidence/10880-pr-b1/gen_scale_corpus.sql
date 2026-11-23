-- #10880 PR-B1 theory proof: synthetic scaled corpus (scratch; no private data).
-- 800 scopes (480 repo, 320 image), 20,000 canonical keys, 28,000 active supports
-- (70% single, 20% two, 10% three), ~4,000 stale-generation supports, 200 operator
-- overrides, 40 derived-class facts, 150,000 unrelated facts as table noise.
\set ON_ERROR_STOP on
BEGIN;
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
  observed_at, ingested_at, status, active_generation_id, payload)
SELECT sc, 'synthetic', 'synthetic', sc, 'synthetic', sc, now(), now(), 'active', 'gen:' || sc || ':2', '{}'::jsonb
FROM (SELECT CASE WHEN i < 480 THEN 'git-repository-scope:repository:r' || i ELSE 'image-scope:i' || i END AS sc
      FROM generate_series(0, 799) i
      UNION ALL SELECT 'operator:vulnerability_suppressions') s(sc);
-- the bootstrap already created eshu:global; this script adds only its own scopes.
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
SELECT 'gen:' || scope_id || ':2', scope_id, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb FROM ingestion_scopes WHERE scope_id <> 'eshu:global';
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
SELECT 'gen:' || scope_id || ':1', scope_id, 'synthetic', now() - interval '1 day', now() - interval '1 day', 'superseded', now() - interval '1 day', '{}'::jsonb
FROM ingestion_scopes WHERE scope_id NOT IN ('operator:vulnerability_suppressions','eshu:global') AND hashtext(scope_id) % 100 < 15;

CREATE TEMP TABLE gen_supports AS
SELECT k, j,
       'fact:' || k || ':' || j AS fact_id,
       (SELECT CASE WHEN x < 480 THEN 'git-repository-scope:repository:r' || x ELSE 'image-scope:i' || x END
        FROM (SELECT ((k * 31 + j * 97 + (k % 13) * 7) % 800) AS x) q) AS scope_id
FROM generate_series(0, 19999) k
JOIN LATERAL generate_series(0, CASE WHEN k % 10 < 7 THEN 0 WHEN k % 10 < 9 THEN 1 ELSE 2 END) j ON TRUE;

INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT g.fact_id, g.scope_id, 'gen:' || g.scope_id || ':2', 'reducer_supply_chain_impact_finding', g.fact_id, '1.0.0',
       'synthetic', 1, 'observed', 'synthetic', g.fact_id, now(), now(),
       jsonb_strip_nulls(jsonb_build_object(
         'finding_id', CASE WHEN (g.k + g.j) % 20 = 0 THEN NULL ELSE 'finding:' || g.k END,
         'cve_id', 'CVE-2026-' || g.k,
         'package_id', 'pkg:npm/p' || (g.k % 4000) || '@1.' || (g.k % 7) || '.0',
         'repository_id', 'repository:r' || (g.k % 1500),
         'subject_digest', CASE WHEN g.k % 3 = 0 THEN 'sha256:' || md5(g.k::text) ELSE NULL END,
         'impact_status', (ARRAY['affected_exact','affected_exact','affected_exact','affected_exact','affected_exact',
                                 'possibly_affected','possibly_affected','not_affected_known_fixed','affected_derived','not_affected_version'])[(g.k % 10) + 1],
         'observed_version', '1.' || (g.k % 7) || '.0',
         'match_reason', CASE WHEN (g.k + g.j * 3) % 5 < 3 THEN 'npm_semver_affected_range' ELSE 'osv_range_match' END,
         'ecosystem', (ARRAY['npm','NPM','pypi','maven','nuget','cargo'])[(g.k % 6) + 1],
         'cvss_score', ((g.k * 7 + g.j * 13) % 100) / 10.0,
         'priority_score', (g.k * 7919 + g.j * 131) % 100,
         'priority_bucket', CASE WHEN (g.k * 7919 + g.j * 131) % 100 >= 70 THEN 'critical'
                                 WHEN (g.k * 7919 + g.j * 131) % 100 >= 40 THEN 'high' ELSE 'low' END,
         'suppression_state', 'active',
         'image_ref', CASE WHEN g.scope_id LIKE 'image-scope:%' THEN 'registry.example/app' || (g.k % 50) || ':v1' ELSE NULL END,
         'service_ids', '[]'::jsonb, 'workload_ids', '[]'::jsonb, 'environments', '[]'::jsonb,
         'intent_id', md5('i' || g.fact_id), 'cause', 'synthetic', 'generation_id', 'gen:' || g.scope_id || ':2',
         'provenance', jsonb_build_object('content_digest', md5(g.fact_id),
             'advisory_group_members', (SELECT jsonb_agg('adv-' || md5(g.k::text || n)) FROM generate_series(1, 6) n),
             'summary', repeat(md5(g.fact_id), 12)),
         'evidence_fact_ids', (SELECT jsonb_agg('fact:' || md5(g.fact_id || n) || md5(n::text)) FROM generate_series(1, 8) n),
         'narrative', repeat(md5(g.fact_id || 'n'), 20)
       ))
FROM gen_supports g;

-- stale-generation supports (superseded generation, must never answer)
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'stale:' || f.fact_id, f.scope_id, 'gen:' || f.scope_id || ':1', f.fact_kind, f.fact_id, '1.0.0', 'synthetic', 0,
       'observed', 'synthetic', 'stale:' || f.fact_id, now() - interval '1 day', now() - interval '1 day',
       f.payload || jsonb_build_object('priority_score', 99, 'priority_bucket', 'critical')
FROM fact_records f
JOIN scope_generations g ON g.scope_id = f.scope_id AND g.generation_id = 'gen:' || f.scope_id || ':1'
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND hashtext(f.fact_id) % 100 < 30;

-- operator overrides: 200 keys, mixed states, some with expiry (expired and future)
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'op:' || f.fact_id, 'operator:vulnerability_suppressions', 'gen:operator:vulnerability_suppressions:2',
       f.fact_kind, 'op:' || f.fact_id, '1.0.0', 'synthetic', 1, 'observed', 'synthetic', 'op:' || f.fact_id, now(), now(),
       (f.payload - 'image_ref') || jsonb_build_object(
         'suppression_state', (ARRAY['accepted_risk','false_positive','not_affected','ignored'])[(substring(f.fact_id from 'fact:(\d+)')::int % 4) + 1],
         'suppression', jsonb_build_object('state', 'accepted_risk',
             'expires_at', CASE (substring(f.fact_id from 'fact:(\d+)')::int % 3)
                             WHEN 0 THEN to_char(now() - interval '2 days', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
                             WHEN 1 THEN to_char(now() + interval '30 days', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
                             ELSE NULL END))
FROM fact_records f
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND f.fact_id LIKE 'fact:%:0'
  AND substring(f.fact_id from 'fact:(\d+)')::int % 100 = 5;

-- derived-class facts: 40 real supports reclassified derived
INSERT INTO scanner_image_result_fact_classifications (finding_fact_id, source_class, lifecycle, scan_scope_id)
SELECT fact_id, 'derived', 'active', 'image://derived/scan-x'
FROM fact_records WHERE fact_kind = 'reducer_supply_chain_impact_finding' AND fact_id LIKE 'fact:%:1'
  AND substring(fact_id from 'fact:(\d+)')::int % 500 = 8;
-- and 200 static classifications (active)
INSERT INTO scanner_image_result_fact_classifications (finding_fact_id, source_class, lifecycle, scan_scope_id)
SELECT fact_id, 'static', 'active', ''
FROM fact_records WHERE fact_kind = 'reducer_supply_chain_impact_finding' AND fact_id LIKE 'fact:%:0'
  AND substring(fact_id from 'fact:(\d+)')::int % 100 = 3
ON CONFLICT DO NOTHING;

-- unrelated table noise
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'noise:' || n, sc.scope_id, 'gen:' || sc.scope_id || ':2', 'content_entity', 'noise:' || n, '1.0.0', 'synthetic', 1,
       'observed', 'git', 'noise:' || n, now(), now(),
       jsonb_build_object('entity_name', 'e' || n, 'entity_type', 'Function', 'blob', repeat(md5(n::text), 30))
FROM generate_series(1, 150000) n
JOIN LATERAL (SELECT scope_id FROM ingestion_scopes WHERE scope_id NOT IN ('operator:vulnerability_suppressions','eshu:global')
              ORDER BY scope_id OFFSET (n % 800) LIMIT 1) sc ON TRUE;
COMMIT;
