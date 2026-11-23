-- gen_v_corpus.sql: the scaled corpus for the route V theory proof (#10880 PR-B2a).
--
-- Run with psql against a database that ApplyBootstrap has bootstrapped:
--   psql -v keys=80000 -v per=100 -v scopes=2000 -v axios_keys=15000 -f gen_v_corpus.sql
--
-- Shape (all synthetic):
--   * keys x per supports, each key's supports in `per` distinct active scopes;
--   * one package, pkg:npm/axios, holds axios_keys keys (1.5M supports at 15,000);
--   * key-determined columns (the SQL canonical key's parts) never vary inside a
--     key: impact_status, observed_version, cve, package, repository;
--   * per-support columns vary inside a key: priority_score, match_reason (about
--     70% precise), image_ref (5% empty), severity_bucket;
--   * a 281-precise-support key (key:281) and 5,000 "trap" keys whose
--     highest-priority support is not precise-eligible while exactly one low
--     priority support is (the winner fails precise, the supplement must find it);
--   * the winners table is built from the supports with the maintainer's order
--     (priority_score DESC, has_payload_finding_id DESC, fact_id ASC).
-- Payloads are small (about 200 bytes), so the measured payload fetch is cheaper
-- than production's; that limit is stated in the results.

\set ON_ERROR_STOP on
\timing on

INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'scope:b2a:' || n, 'synthetic', 'synthetic', 'k' || n, 'synthetic', 'k' || n,
       now(), now(), 'active', 'gen:b2a:' || n, '{}'::jsonb
FROM generate_series(1, :scopes) AS n;

INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
SELECT 'gen:b2a:' || n, 'scope:b2a:' || n, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb
FROM generate_series(1, :scopes) AS n;

-- Precise reason list, the same twelve reasons the read's predicate names.
CREATE TEMP TABLE b2a_reasons (n int, reason text);
INSERT INTO b2a_reasons VALUES
  (0, 'npm_semver_affected_range'), (1, 'npm_semver_known_fixed'),
  (2, 'nuget_semver_affected_range'), (3, 'nuget_semver_known_fixed'),
  (4, 'cargo_semver_affected_range'), (5, 'cargo_semver_known_fixed'),
  (6, 'hex_semver_affected_range'), (7, 'hex_semver_known_fixed'),
  (8, 'maven_range_match'), (9, 'maven_known_fixed'),
  (10, 'swift_semver_affected_range'), (11, 'swift_semver_known_fixed');

INSERT INTO supply_chain_impact_finding_supports (
  fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id,
  priority_score, source_confidence, suppression_state, impact_status, ecosystem,
  severity_bucket, repository_id, cve_id, advisory_id, package_id, subject_digest,
  image_ref, priority_bucket, detection_profile, observed_version, match_reason, payload)
SELECT
  'fact:' || k || ':' || j,
  'key:' || k,
  'scope:b2a:' || (((k * 7 + j) % :scopes) + 1),
  'gen:b2a:' || (((k * 7 + j) % :scopes) + 1),
  'finding:' || k, 1,
  prio, 'observed', 'active',
  CASE WHEN k % 10 < 8 THEN 'affected_exact' ELSE 'possibly_affected' END,
  'npm',
  (ARRAY['critical', 'high', 'medium', 'low'])[((h / 13) % 4) + 1],
  'repo:' || (k % 3000),
  'CVE-2026-' || (k % 40000),
  '',
  CASE WHEN k <= :axios_keys THEN 'pkg:npm/axios' ELSE 'pkg:npm/p' || (k % 5000) END,
  '',
  CASE WHEN h % 20 = 0 THEN '' ELSE 'registry.example/img' || ((h / 7) % 20000) || ':1' END,
  CASE WHEN prio >= 80 THEN 'critical' WHEN prio >= 60 THEN 'high' WHEN prio >= 30 THEN 'medium' ELSE 'low' END,
  '',
  CASE WHEN k % 20 < 19 THEN '1.' || (k % 50) ELSE '' END,
  CASE WHEN (h / 101) % 10 < 7 THEN (SELECT reason FROM b2a_reasons WHERE n = (h / 1009) % 12)
       ELSE 'dpkg_affected_range' END,
  jsonb_build_object('finding_id', 'finding:' || k, 'priority_score', prio,
                     'impact_status', CASE WHEN k % 10 < 8 THEN 'affected_exact' ELSE 'possibly_affected' END,
                     'cve_id', 'CVE-2026-' || (k % 40000), 'repository_id', 'repo:' || (k % 3000))
FROM generate_series(1, :keys) AS k
CROSS JOIN generate_series(1, :per) AS j
CROSS JOIN LATERAL (SELECT (hashtext(k || '/' || j) & 2147483647) AS h) AS hh
CROSS JOIN LATERAL (SELECT h % 101 AS prio) AS pp;

-- Trap keys: the highest-priority support is non-precise, one low-priority support
-- is precise. The winner fails precise and only the partial index finds the pick.
UPDATE supply_chain_impact_finding_supports
SET match_reason = CASE WHEN fact_id LIKE '%:1' THEN 'dpkg_affected_range' ELSE 'dpkg_affected_range' END,
    priority_score = CASE WHEN fact_id LIKE '%:1' THEN 100 ELSE LEAST(priority_score, 90) END
WHERE canonical_key IN (SELECT 'key:' || k FROM generate_series(:axios_keys + 1, :axios_keys + 5000) k)
  AND impact_status = 'affected_exact' AND observed_version <> '';
UPDATE supply_chain_impact_finding_supports
SET match_reason = 'npm_semver_affected_range', priority_score = 5
WHERE canonical_key IN (SELECT 'key:' || k FROM generate_series(:axios_keys + 1, :axios_keys + 5000) k)
  AND impact_status = 'affected_exact' AND observed_version <> ''
  AND fact_id LIKE '%:50';
-- Key 281: affected_exact, versioned, and every one of its supports precise.
UPDATE supply_chain_impact_finding_supports
SET match_reason = 'npm_semver_affected_range', impact_status = 'affected_exact', observed_version = '1.1'
WHERE canonical_key = 'key:281';

-- One extra key with 281 supports (the QA maximum per key), all precise, built on
-- top of the base corpus so the base counts stay exact.
INSERT INTO supply_chain_impact_finding_supports (
  fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id,
  priority_score, source_confidence, suppression_state, impact_status, ecosystem,
  severity_bucket, repository_id, cve_id, advisory_id, package_id, subject_digest,
  image_ref, priority_bucket, detection_profile, observed_version, match_reason, payload)
SELECT 'fact:big:' || j, 'key:big', 'scope:b2a:' || (j % :scopes + 1), 'gen:b2a:' || (j % :scopes + 1),
       'finding:big', 1, j % 100, 'observed', 'active', 'affected_exact', 'npm', 'high',
       'repo:big', 'CVE-2026-big', '', 'pkg:npm/axios', '', 'registry.example/big:1', 'high', '',
       '1.0', 'npm_semver_affected_range',
       jsonb_build_object('finding_id', 'finding:big', 'priority_score', j % 100)
FROM generate_series(1, 281) AS j;

-- Operator overrides for 200 keys (the Q1 lateral probe's partial index).
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ('operator:vulnerability_suppressions', 'synthetic', 'synthetic', 'op', 'synthetic', 'op',
        now(), now(), 'active', 'gen:b2a:operator', '{}'::jsonb);
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ('gen:b2a:operator', 'operator:vulnerability_suppressions', 'synthetic', now(), now(), 'active', now(), '{}'::jsonb);
INSERT INTO supply_chain_impact_finding_supports (
  fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id,
  priority_score, source_confidence, suppression_state, payload)
SELECT 'fact:op:' || k, 'key:' || k, 'operator:vulnerability_suppressions', 'gen:b2a:operator',
       'finding:' || k, 1, 50, 'operator', 'accepted_risk',
       jsonb_build_object('suppression_state', 'accepted_risk', 'suppression', jsonb_build_object('state', 'accepted_risk'))
FROM generate_series(1, 200) AS k;

-- The winners table: one row per canonical key from the active, non-operator
-- supports, in the maintainer's order.
INSERT INTO supply_chain_impact_canonical_winners (
  canonical_key, winner_fact_id, winner_scope_id, finding_id, priority_score, source_count,
  impact_status, ecosystem, severity_bucket, repository_id, cve_id, advisory_id, package_id,
  subject_digest, image_ref, priority_bucket, detection_profile, observed_version, match_reason,
  suppression_state, materialized_at, payload, source_confidence)
SELECT DISTINCT ON (s.canonical_key)
  s.canonical_key, s.fact_id, s.scope_id, s.finding_id, s.priority_score,
  count(*) OVER (PARTITION BY s.canonical_key)::int,
  s.impact_status, s.ecosystem, s.severity_bucket, s.repository_id, s.cve_id, s.advisory_id,
  s.package_id, s.subject_digest, s.image_ref, s.priority_bucket, s.detection_profile,
  s.observed_version, s.match_reason, s.suppression_state, now(), s.payload, s.source_confidence
FROM supply_chain_impact_finding_supports AS s
WHERE s.scope_id <> 'operator:vulnerability_suppressions'
ORDER BY s.canonical_key, s.priority_score DESC, s.has_payload_finding_id DESC, s.fact_id ASC;

ANALYZE supply_chain_impact_finding_supports;
ANALYZE supply_chain_impact_canonical_winners;
ANALYZE ingestion_scopes;
ANALYZE scope_generations;

SELECT 'supports' AS what, count(*) FROM supply_chain_impact_finding_supports
UNION ALL SELECT 'winners', count(*) FROM supply_chain_impact_canonical_winners
UNION ALL SELECT 'axios supports', count(*) FROM supply_chain_impact_finding_supports WHERE package_id = 'pkg:npm/axios';
