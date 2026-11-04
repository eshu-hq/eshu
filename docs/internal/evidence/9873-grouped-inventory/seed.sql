-- Synthetic corpus for the #9873 grouped inventory measurements.
-- Usage (disposable database with the production schema applied):
--   psql "$DSN" -v mult=1 -v bgper=8 -f seed.sql   # 1x
--   psql "$DSN" -v mult=5 -v bgper=8 -f seed.sql   # 5x
-- Then rebuild winners (RebuildAllWinners) and VACUUM (ANALYZE); see README.md.
\set ON_ERROR_STOP on
\timing off
SET session_replication_role = replica;
SELECT setseed(0.42);
TRUNCATE ingestion_scopes CASCADE;
\set nrepo `echo $((400 * :mult))`
\set nfind `echo $((20000 * :mult))`
\set nsvc `echo $((150 * :mult))`
\set nwl `echo $((220 * :mult))`
\set nbg `echo $((20000 * :mult * :bgper))`

-- repo roster with mapping class
CREATE TEMP TABLE repo_seed AS
SELECT i AS idx,
       'repository:r-' || i AS repository_id,
       'git-repository-scope:repository:r-' || i AS scope_id,
       'gen:' || i || ':2' AS gen_id,
       r AS rnd,
       CASE WHEN r < 0.25 THEN 0
            WHEN r < 0.25 + 0.75*0.60 THEN 1
            WHEN r < 0.25 + 0.75*0.85 THEN 2
            ELSE 3 END AS n_svc
FROM (SELECT g AS i, random() AS r FROM generate_series(1, :nrepo) g) t;

INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
SELECT scope_id, 'repository', 'git', scope_id, 'git', scope_id, now(), now(), 'active', '{}'::jsonb FROM repo_seed;
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
VALUES ('operator:vulnerability_suppressions','operator','operator','op','operator','op',now(),now(),'active','{}'::jsonb);
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
SELECT 'gen:' || idx || ':1', scope_id, 'synthetic', now()-interval '2 days', now()-interval '2 days', 'superseded', now()-interval '2 days', '{}'::jsonb FROM repo_seed;
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
SELECT gen_id, scope_id, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb FROM repo_seed;
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ('gen:op:1','operator:vulnerability_suppressions','synthetic',now(),now(),'active',now(),'{}'::jsonb);
UPDATE ingestion_scopes s SET active_generation_id = r.gen_id FROM repo_seed r WHERE s.scope_id = r.scope_id;
UPDATE ingestion_scopes SET active_generation_id = 'gen:op:1' WHERE scope_id='operator:vulnerability_suppressions';

-- repo -> services (shared services across repos)
CREATE TEMP TABLE repo_service AS
SELECT r.idx, r.repository_id, r.scope_id, r.gen_id, j,
       'service:svc-' || (((r.idx * 7 + j * 13) % :nsvc) + 1) AS service_id,
       CASE WHEN random() < 0.7 THEN 'workload:wl-' || (((r.idx * 11 + j * 5) % :nwl) + 1) END AS workload_id,
       random() AS o
FROM repo_seed r, LATERAL generate_series(1, r.n_svc) j;

INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'cat:' || idx || ':' || j || ':' || k, scope_id, gen_id, 'reducer_service_catalog_correlation',
       'cat:' || idx || ':' || j || ':' || k, 'reducer', 'cat', now(), now(),
       jsonb_build_object('service_id', service_id, 'repository_id', repository_id,
         'outcome', CASE WHEN o < 0.80 THEN 'exact' WHEN o < 0.90 THEN 'derived' WHEN o < 0.95 THEN 'ambiguous' ELSE '' END,
         'provenance_only', 'false', 'entity_ref', 'component:default/' || service_id, 'owner_ref', 'group:default/team-' || (idx % 40),
         'provider', 'backstage', 'drift_status', 'none', 'notes', repeat('x', 300))
       || CASE WHEN workload_id IS NOT NULL THEN jsonb_build_object('workload_id', workload_id) ELSE '{}'::jsonb END
FROM repo_service, LATERAL generate_series(1, CASE WHEN o > 0.7 THEN 2 ELSE 1 END) k;
-- a handful of stale-generation catalog facts (must be filtered)
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'catstale:' || idx || ':' || j, scope_id, 'gen:' || idx || ':1', 'reducer_service_catalog_correlation', 'catstale:' || idx || ':' || j, 'reducer', 'cat', now(), now(),
       jsonb_build_object('service_id', 'service:stale-' || idx, 'repository_id', repository_id, 'outcome', 'exact', 'provenance_only', 'false')
FROM repo_service WHERE o < 0.3;

-- workload identity (1-3 per mapped repo)
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'wid:' || r.idx || ':' || j, r.scope_id, r.gen_id, 'reducer_workload_identity', 'wid:' || r.idx || ':' || j, 'reducer', 'wid', now(), now(),
       jsonb_build_object('workload_id', 'workload:wl-' || (((r.idx * 11 + j * 5) % :nwl) + 1), 'repository_id', r.repository_id,
         'entity_keys', jsonb_build_array('workload:wl-' || (((r.idx * 11 + j * 5) % :nwl) + 1), 'k8s:' || r.idx || ':' || j), 'notes', repeat('y', 300))
FROM repo_seed r, LATERAL generate_series(1, 1 + (r.idx % 3)) j WHERE r.n_svc > 0;

-- ci/cd run correlation: env histories (10 runs per repo-env)
CREATE TEMP TABLE env_names(ord int, env text, p float);
INSERT INTO env_names VALUES (1,'dev',1.0),(2,'qa',0.8),(3,'stage',0.6),(4,'prod-us',0.5),(5,'prod-eu',0.25),(6,'sandbox',0.2);
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'cicd:' || r.idx || ':' || e.ord || ':' || k, r.scope_id, r.gen_id, 'reducer_ci_cd_run_correlation', 'cicd:' || r.idx || ':' || e.ord || ':' || k, 'reducer', 'cicd', now(), now(),
       jsonb_build_object('environment', e.env, 'outcome', CASE WHEN k % 10 = 0 THEN 'ambiguous' ELSE 'exact' END, 'provenance_only', 'false',
         'run_id', 'run-' || r.idx || '-' || e.ord || '-' || k, 'provider', 'github', 'commit_sha', md5(r.idx || ':' || k),
         'artifact_digest', 'sha256:' || md5(r.idx || ':' || e.ord), 'notes', repeat('z', 400))
       || CASE WHEN (r.idx % 5) <> 0 THEN jsonb_build_object('repository_id', r.repository_id) ELSE '{}'::jsonb END
FROM repo_seed r
JOIN env_names e ON (r.n_svc > 0 OR r.rnd < 0.25 * 0.3) AND (((r.idx * 31 + e.ord * 17) % 100) / 100.0) < e.p
, LATERAL generate_series(1, 10) k;

-- findings
CREATE TEMP TABLE find_seed AS
SELECT i, 1 + floor(:nrepo * power(random(), 1.8))::int AS ridx, random() AS r_status, random() AS r_prio, random() AS r_eco, random() AS r_prof,
       random() AS r_sup, random() AS r_cvss, random() AS r_baked
FROM generate_series(1, :nfind) i;

INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'find:' || f.i, r.scope_id, r.gen_id, 'reducer_supply_chain_impact_finding', 'find:' || f.i, 'reducer', 'find', now(), now(),
  jsonb_build_object(
    'finding_id', 'finding:' || md5(f.i::text),
    'cve_id', 'CVE-2025-' || (f.i % 6000), 'advisory_id', 'GHSA-' || md5((f.i % 6000)::text),
    'package_id', 'pkg:' || eco.e || '/p' || (f.i % 3000), 'purl', 'pkg:' || eco.e || '/p' || (f.i % 3000) || '@1.' || (f.i % 97),
    'match_criteria_id', 'mc-' || f.i, 'observed_version', '1.' || (f.i % 97) || '.0', 'requested_range', '^1.' || (f.i % 97),
    'repository_id', r.repository_id, 'subject_digest', '',
    'impact_status', st.s, 'ecosystem', eco.e,
    'detection_profile', CASE WHEN f.r_prof < 0.45 THEN 'precise' WHEN f.r_prof < 0.90 THEN 'comprehensive' ELSE '' END,
    'match_reason', CASE WHEN st.s IN ('affected_exact','not_affected_known_fixed') THEN eco.reason || CASE WHEN st.s = 'affected_exact' THEN '_affected_range' ELSE '_known_fixed' END ELSE 'comprehensive_owned_anchor' END,
    'priority_bucket', pb.b, 'priority_score', pb.score::text,
    'suppression_state', CASE WHEN f.r_sup < 0.92 THEN 'active' WHEN f.r_sup < 0.96 THEN 'accepted_risk' WHEN f.r_sup < 0.98 THEN 'false_positive' ELSE 'ignored' END,
    'cvss_score', CASE WHEN f.r_cvss < 0.8 THEN round((f.r_cvss/0.8*10)::numeric, 1)::text ELSE '' END,
    'provenance', jsonb_build_object('selected_severity_label', CASE WHEN f.r_cvss >= 0.8 THEN (ARRAY['critical','high','moderate','low'])[1 + (f.i % 4)] ELSE 'unknown' END, 'source', 'osv'),
    'service_ids', CASE WHEN f.r_baked < 0.4 THEN COALESCE((SELECT jsonb_agg(DISTINCT service_id) FROM repo_service rs WHERE rs.idx = r.idx), '[]'::jsonb) ELSE '[]'::jsonb END,
    'workload_ids', CASE WHEN f.r_baked < 0.4 THEN COALESCE((SELECT jsonb_agg(DISTINCT workload_id) FROM repo_service rs WHERE rs.idx = r.idx AND workload_id IS NOT NULL), '[]'::jsonb) ELSE '[]'::jsonb END,
    'environments', CASE WHEN f.r_baked < 0.3 THEN '["prod-us","qa"]'::jsonb ELSE '[]'::jsonb END,
    'evidence_fact_ids', jsonb_build_array('ev:' || f.i || ':1', 'ev:' || f.i || ':2', 'ev:' || f.i || ':3', 'ev:' || f.i || ':4'),
    'evidence_path', jsonb_build_array('sbom', 'package', 'vuln', 'catalog'),
    'dependency_path', jsonb_build_array('root', 'mid-' || (f.i % 50), 'p' || (f.i % 3000)),
    'remediation', jsonb_build_object('fixed_versions', jsonb_build_array('2.0.0'), 'advice', repeat('upgrade ', 20)),
    'notes', repeat('n', 500))
FROM find_seed f
JOIN repo_seed r ON r.idx = f.ridx
CROSS JOIN LATERAL (SELECT CASE WHEN f.r_status < 0.25 THEN 'affected_exact' WHEN f.r_status < 0.40 THEN 'affected_derived' WHEN f.r_status < 0.65 THEN 'possibly_affected' WHEN f.r_status < 0.90 THEN 'not_affected_known_fixed' ELSE 'not_affected_unknown' END AS s) st
CROSS JOIN LATERAL (SELECT CASE WHEN f.r_eco < 0.35 THEN 'npm' WHEN f.r_eco < 0.60 THEN 'maven' WHEN f.r_eco < 0.75 THEN 'pypi' WHEN f.r_eco < 0.85 THEN 'go' WHEN f.r_eco < 0.90 THEN 'cargo' ELSE 'nuget' END AS e,
                         CASE WHEN f.r_eco < 0.35 THEN 'npm_semver' WHEN f.r_eco < 0.60 THEN 'maven' WHEN f.r_eco < 0.90 AND f.r_eco >= 0.85 THEN 'cargo_semver' WHEN f.r_eco >= 0.90 THEN 'nuget_semver' ELSE 'other' END AS reason) eco
CROSS JOIN LATERAL (SELECT CASE WHEN f.r_prio < 0.05 THEN 'critical' WHEN f.r_prio < 0.25 THEN 'high' WHEN f.r_prio < 0.65 THEN 'medium' WHEN f.r_prio < 0.95 THEN 'low' ELSE '' END AS b,
                          CASE WHEN f.r_prio < 0.05 THEN 90 + (f.i % 10) WHEN f.r_prio < 0.25 THEN 70 + (f.i % 20) WHEN f.r_prio < 0.65 THEN 40 + (f.i % 30) WHEN f.r_prio < 0.95 THEN 10 + (f.i % 30) ELSE 0 END AS score) pb;


-- 10% duplicate-source facts: identical canonical key written by a second (secondary) scope, lower priority
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
SELECT 'git-repository-scope:dup-' || g, 'repository', 'git', 'dup-' || g, 'git', 'dup-' || g, now(), now(), 'active', '{}'::jsonb FROM generate_series(0,19) g;
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
SELECT 'gen:dup:' || g, 'git-repository-scope:dup-' || g, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb FROM generate_series(0,19) g;
UPDATE ingestion_scopes SET active_generation_id = 'gen:dup:' || substr(scope_id, length('git-repository-scope:dup-')+1) WHERE scope_id LIKE 'git-repository-scope:dup-%';
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'dup:' || f.fact_id, 'git-repository-scope:dup-' || (abs(hashtext(f.fact_id)) % 20), 'gen:dup:' || (abs(hashtext(f.fact_id)) % 20), f.fact_kind, 'dup:' || f.fact_id, 'reducer', 'find', now(), now(),
       jsonb_set(f.payload, '{priority_score}', to_jsonb(GREATEST((f.payload->>'priority_score')::int - 3, 0)::text))
FROM fact_records f WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND f.fact_id LIKE 'find:%' AND (abs(hashtext(f.fact_id)) % 10) = 0;
-- 5% stale-generation findings (superseded generation of the owning repo scope)
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'stale:' || f.fact_id, f.scope_id, regexp_replace(f.generation_id, ':2$', ':1'), f.fact_kind, 'stale:' || f.fact_id, 'reducer', 'find', now(), now(), f.payload
FROM fact_records f WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND f.fact_id LIKE 'find:%' AND (abs(hashtext(f.fact_id)) % 20) = 1;
-- operator suppression overlay: 1% of keys carry an operator decision
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'op:' || f.fact_id, 'operator:vulnerability_suppressions', 'gen:op:1', f.fact_kind, 'op:' || f.fact_id, 'operator', 'op', now(), now(),
       (f.payload - 'finding_id') || jsonb_build_object('suppression_state', 'accepted_risk', 'suppression', jsonb_build_object('expires_at', (now() + interval '30 days')::text))
FROM fact_records f WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND f.fact_id LIKE 'find:%' AND (abs(hashtext(f.fact_id)) % 100) = 2;

-- background noise: unrelated kinds sharing the same scopes
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'bg:' || g, r.scope_id, r.gen_id, CASE WHEN g % 3 = 0 THEN 'file' ELSE 'content_entity' END, 'bg:' || g, 'git', 'bg', now(), now(),
       jsonb_build_object('repo_id', r.repository_id, 'relative_path', 'src/f' || g || '.go', 'entity_id', 'e' || g, 'entity_type', 'Function', 'entity_name', 'fn' || g, 'body', repeat('b', 600))
FROM generate_series(1, :nbg) g
JOIN repo_seed r ON r.idx = 1 + (g % :nrepo);

SET session_replication_role = origin;
