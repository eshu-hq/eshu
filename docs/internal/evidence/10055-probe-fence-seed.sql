-- Fence EXPLAIN probe (#10055, post-purge tree): representative seed + plans.
-- Database: scratch eshu_fence_probe with all 318 migrations applied.
-- Seed: 20 repos x (1 active gen at C + 50 superseded gens at E), 20 derived
-- scopes with Go evidence, 20 holders x 50 findings, ~12k noise rows.

-- 20 repository scopes, active at commit C (distinct per repo).
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'repository:r_probe' || lpad(g::text, 2, '0'), 'repository', 'git', 'repository:r_probe' || lpad(g::text, 2, '0'),
       'git-source', 'pk-probe', now(), now(), 'active', 'repo-active-' || g,
       jsonb_build_object('remote_url', 'https://example.invalid/o/r' || g)
FROM generate_series(1, 20) g;

-- 50 superseded generations at E per repo (the A1.2 depth), superseded ~8 days ago.
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, source_commit_sha, observed_at, ingested_at, status, activated_at, superseded_at)
SELECT 'repo-hist-' || g || '-' || h, 'repository:r_probe' || lpad(g::text, 2, '0'),
       'snapshot', 'h', 'E-commit',
       now() - interval '8 days' - (h || ' minutes')::interval,
       now() - interval '8 days' - (h || ' minutes')::interval,
       'superseded',
       now() - interval '8 days' - (h || ' minutes')::interval,
       now() - interval '8 days' - (h || ' minutes')::interval
FROM generate_series(1, 20) g CROSS JOIN generate_series(1, 50) h;

-- Active generation at C per repo.
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, source_commit_sha, observed_at, ingested_at, status, activated_at, superseded_at)
SELECT 'repo-active-' || g, 'repository:r_probe' || lpad(g::text, 2, '0'),
       'snapshot', 'h', 'C-commit-' || g, now(), now(), 'active', now(), NULL
FROM generate_series(1, 20) g;

-- Derived go-reachability scopes with an active generation at E.
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'go-reachability://repository:r_probe' || lpad(g::text, 2, '0'), 'derived', 'scanner_worker',
       'repository:r_probe' || lpad(g::text, 2, '0'), 'scanner_worker', 'pk-probe', now(), now(), 'active',
       'derived-gen-' || g, '{}'
FROM generate_series(1, 20) g;

INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, source_commit_sha, observed_at, ingested_at, status, activated_at, superseded_at)
SELECT 'derived-gen-' || g, 'go-reachability://repository:r_probe' || lpad(g::text, 2, '0'),
       'snapshot', 'h', 'E-commit', now() - interval '9 days', now() - interval '9 days', 'active',
       now() - interval '9 days', NULL
FROM generate_series(1, 20) g;

-- Go evidence facts on each derived generation (10 per scope).
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'goev-' || g || '-' || k, 'go-reachability://repository:r_probe' || lpad(g::text, 2, '0'),
       'derived-gen-' || g, 'vulnerability.go_module_evidence', 'k' || k, 'scanner_worker', 'sf' || k,
       now(), now(), jsonb_build_object('repository_id', 'repository:r_probe' || lpad(g::text, 2, '0'))
FROM generate_series(1, 20) g CROSS JOIN generate_series(1, 10) k;

-- content_files go.mod marker per repo (has_go_module probe input).
INSERT INTO content_files (repo_id, relative_path, commit_sha, content, content_hash, line_count, indexed_at)
SELECT 'repository:r_probe' || lpad(g::text, 2, '0'), 'go.mod', 'C-commit-' || g, 'module example.invalid/r', 'h' || g, 3, now()
FROM generate_series(1, 20) g;

-- 20 holder advisory scopes, each with an active generation.
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'advisory:probe-' || lpad(g::text, 2, '0'), 'advisory', 'osv', 'cve-probe-' || g,
       'osv-intel', 'pk-probe', now(), now(), 'active', 'holder-gen-' || g, '{}'
FROM generate_series(1, 20) g;

INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, source_commit_sha, observed_at, ingested_at, status, activated_at, superseded_at)
SELECT 'holder-gen-' || g, 'advisory:probe-' || lpad(g::text, 2, '0'),
       'snapshot', 'h', 'H-commit', now(), now(), 'active', now(), NULL
FROM generate_series(1, 20) g;

-- 50 finding rows per holder naming r_probe01 (the fenced repository).
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'finding-01-' || k || '-' || h, 'advisory:probe-' || lpad(k::text, 2, '0'), 'holder-gen-' || k,
       'reducer_supply_chain_impact_finding', 'sk' || h, 'reducer', 'sf' || h, now(), now(),
       jsonb_build_object('repository_id', 'repository:r_probe01', 'cve_id', 'CVE-PROBE-' || h)
FROM generate_series(1, 20) k CROSS JOIN generate_series(1, 50) h;

-- Noise: 10k finding rows for other repositories (index selectivity proof).
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'noise-' || k, 'advisory:probe-' || lpad((k % 20 + 1)::text, 2, '0'), 'holder-gen-' || (k % 20 + 1),
       'reducer_supply_chain_impact_finding', 'nsk' || k, 'reducer', 'nsf' || k, now(), now(),
       jsonb_build_object('repository_id', 'r_other' || (k % 500), 'cve_id', 'CVE-NOISE-' || k)
FROM generate_series(1, 10000) k;

-- Noise: 2k tombstones + 2k other-kind rows.
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'tomb-' || k, 'advisory:probe-01', 'holder-gen-1',
       'reducer_supply_chain_impact_finding', 'tsk' || k, 'reducer', 'tsf' || k, now(), now(), TRUE,
       jsonb_build_object('repository_id', 'repository:r_probe01')
FROM generate_series(1, 2000) k;

INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'other-' || k, 'advisory:probe-01', 'holder-gen-1',
       'vulnerability.os_package', 'osk' || k, 'osv', 'osf' || k, now(), now(),
       jsonb_build_object('repository_id', 'repository:r_probe01')
FROM generate_series(1, 2000) k;

VACUUM ANALYZE ingestion_scopes;
VACUUM ANALYZE scope_generations;
VACUUM ANALYZE fact_records;
VACUUM ANALYZE content_files;
