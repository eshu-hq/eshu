-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
-- #7420 fixture. Run parents once, then the loader once per generation:
--   psql -v gen=gen-target -v scope=scope-1 -v n=150000 -f 7420-key-sidecar-fixture.sql
-- 20% share: gen-target plus gen-2..5-live; 5% share: add gen-6..20-live.
-- Parents, run once by hand before the first load:
-- INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
-- SELECT 'scope-'||s, 'repository','git','src-'||s,'git','p'||s,now(),now(),'active','gen-'||s||'-live' FROM generate_series(1,20) s;
-- INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, superseded_at)
-- SELECT 'gen-'||s||'-live','scope-'||s,'snapshot',now(),now(),'active',now(),NULL FROM generate_series(1,20) s;
-- INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, superseded_at)
-- VALUES ('gen-target','scope-1','snapshot',now(),now(),'superseded',now(),now()),
--        ('gen-stale','scope-1','snapshot',now(),now(),'superseded',now(),now());

\set ON_ERROR_STOP on
\timing on
SET synchronous_commit = off;
-- fact ids are md5 hex like production sha ids: a generation's ids scatter
-- across the whole fact_id primary key, never one contiguous range.
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT md5(:'gen' || '-' || i), :'scope', :'gen', 'content_entity', 'k-' || :'gen' || '-' || i, 'git', 'sf-' || i, now() + make_interval(secs => i/1000.0), now(),
       jsonb_build_object('repo_id','repo-'||:'scope','entity_id', :'gen' || '-' || i, 'entity_type','Function')
FROM generate_series(1, :n) i;
INSERT INTO relationship_reference_candidate_keys (fact_id, scope_id, generation_id, source_repo_id, reference_key)
SELECT fact_id, scope_id, generation_id, 'repo-x', 'ref-' || fact_id FROM fact_records WHERE scope_id=:'scope' AND generation_id=:'gen';
INSERT INTO package_manifest_consumption_keys (fact_id, scope_id, generation_id, repository_id, ecosystem, package_name)
SELECT fact_id, scope_id, generation_id, 'repo-x', 'npm', 'pkg-' || (hashtext(fact_id) & 1023) FROM fact_records
 WHERE scope_id=:'scope' AND generation_id=:'gen' AND (hashtext(fact_id || 'm') & 2147483647) % 100 < 20;
INSERT INTO package_registry_identity_keys (fact_id, scope_id, generation_id, ecosystem, package_name, package_id)
SELECT fact_id, scope_id, generation_id, 'npm', 'pkg-' || (hashtext(fact_id) & 1023), 'pid-' || fact_id FROM fact_records
 WHERE scope_id=:'scope' AND generation_id=:'gen' AND (hashtext(fact_id || 'p') & 2147483647) % 100 < 5;
