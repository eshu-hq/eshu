-- 9431: one-time operational retire of stale OSV generations on QA.
--
-- Authorization: arbiter ruling docs/internal/evidence/9431-arbiter-ruling.md
-- (QA environment ONLY; UPDATE of scope_generations.status / superseded_at /
-- payload marker and ingestion_scopes.active_generation_id / status for rows
-- matching the candidate predicate below. No DELETE, no DDL, no hand-written
-- facts, no other environment. Any other environment needs its own recorded
-- authorization reusing this script and proof.)
--
-- Class W list source: docs/internal/evidence/9431-class-w-advisories.txt
-- (229 GHSA ids whose api.osv.dev affected[] carries one package name twice
-- or more; fetched 2026-10-30, all 523 candidate advisories resolved).
--
-- Run read-only sections (instance ids are psql variables so this file stays
-- free of environment identifiers; values are recorded on #9431):
--   psql "$DSN" -v ON_ERROR_STOP=1 -v retired_instance=<retired QA instance> \
--     -v satellite_instance=<satellite QA instance> \
--     -f docs/internal/evidence/9431-qa-retire.sql
-- Dry run (BEGIN ... ROLLBACK, always safe) runs by default.
-- Retire transaction runs ONLY with -v retire_ok=1, after step 4 of the ruling
-- (replacement pass) is recorded in the evidence.
--
-- Scope-status choice (ruling step 3): set ingestion_scopes.status='failed'
-- with active_generation_id=NULL, the value the existing no-active-generation
-- path writes (failProjectorWorkQuery clears the pointer with status='failed',
-- go/internal/storage/postgres/projector_queue_sql.go:239-254). Consumers:
-- impact matcher keys on the pointer plus generation status and never reads
-- scope.status (facts/impact/branches.go:77-91); recover-generations reports a
-- failed scope with only superseded generations as no_recoverable_generation,
-- i.e. "only a new collection heals it" (docs/internal/evidence/
-- 7116-recover-failed-scopes.md:50-57); the scope upsert heals both columns on
-- re-collection (ingestion_queries.go:108-118) and Ack re-sets them
-- (updateProjectorScopeGenerationQuery). No invented status value.

\timing off
\set ON_ERROR_STOP on
-- Fail fast when the instance variables are missing (ON_ERROR_STOP exits).
SELECT :'retired_instance' AS retired_instance, :'satellite_instance' AS satellite_instance \g /dev/null
SET lock_timeout = '10s';
SET statement_timeout = '300s';

-- ---------------------------------------------------------------- Section 0
-- Class W advisory ids (inline copy of 9431-class-w-advisories.txt so the
-- reviewed predicate is byte-exact; regenerate the IN list from that file).
-- --------------------------------------------------------------------------
DROP TABLE IF EXISTS pg_temp.class_w;
CREATE TEMP TABLE pg_temp.class_w (advisory_id TEXT PRIMARY KEY);
INSERT INTO pg_temp.class_w VALUES
  ('GHSA-23c5-xmqv-rm74'),('GHSA-2464-fhr4-w9xw'),('GHSA-253c-mchw-3w2r'),('GHSA-267c-6grr-h53f'),('GHSA-26hh-7cqf-hhc6'),('GHSA-27r3-jxmh-p828'),('GHSA-2883-xcg3-v3hh'),('GHSA-28wg-ghj8-5hjv'),
  ('GHSA-2m8v-j782-fhvr'),('GHSA-2p49-hgcm-8545'),('GHSA-2pfh-q76x-gwvm'),('GHSA-2pr8-phx7-x9h3'),('GHSA-2v37-7h3g-55p8'),('GHSA-2xp9-vwfh-vxw4'),('GHSA-32p4-gm2c-wmch'),('GHSA-36qx-fr4f-26g5'),
  ('GHSA-378v-28hj-76wf'),('GHSA-3g43-6gmg-66jw'),('GHSA-3g8h-86w9-wvmq'),('GHSA-3jfq-g458-7qm9'),('GHSA-3jxr-9vmj-r5cp'),('GHSA-3m93-m4q6-mc6v'),('GHSA-3p68-rc4w-qgx5'),('GHSA-3pjw-73gf-8qr5'),
  ('GHSA-3ppc-4f35-3m26'),('GHSA-3rg7-wf37-54rm'),('GHSA-3wcq-x3mq-6r9p'),('GHSA-3x4c-7xq6-9pq8'),('GHSA-3xgq-45jj-v275'),('GHSA-42h9-826w-cgv3'),('GHSA-43fc-jf86-j433'),('GHSA-4633-3j49-mh5q'),
  ('GHSA-48c2-rrv3-qjmp'),('GHSA-492v-c6pp-mqqv'),('GHSA-4c39-4ccg-62r3'),('GHSA-4hjh-wcwx-xvwj'),('GHSA-4jqv-mc3x-m676'),('GHSA-4vpr-x523-8j87'),('GHSA-4wf5-vphf-c2xc'),('GHSA-4www-5p9h-95mh'),
  ('GHSA-4x5r-pxfx-6jf8'),('GHSA-52cp-r559-cp3m'),('GHSA-5955-9wpr-37jh'),('GHSA-5c9x-8gcm-mpgx'),('GHSA-5gvw-p9qm-jgwh'),('GHSA-5jmj-h7xm-6q6v'),('GHSA-5p4m-2wfm-xmqj'),('GHSA-5pv8-ppvj-4h68'),
  ('GHSA-5rrg-rr89-x9mv'),('GHSA-62hf-57xw-28j9'),('GHSA-64mm-vxmg-q3vj'),('GHSA-657c-g7qc-r9j2'),('GHSA-66ff-xgx4-vchm'),('GHSA-677m-j7p3-52f9'),('GHSA-67hx-6x53-jw92'),('GHSA-685m-2w69-288q'),
  ('GHSA-68g3-v927-f742'),('GHSA-6chq-wfr3-2hj9'),('GHSA-6fq2-x65v-v9h7'),('GHSA-6j4f-fj2g-mc7p'),('GHSA-6qh9-h6wf-jgqc'),('GHSA-6r35-46g8-jcw9'),('GHSA-6vc5-vf29-ffr2'),('GHSA-72hv-8253-57qq'),
  ('GHSA-72xp-p242-47p9'),('GHSA-73rr-hh4g-fpgx'),('GHSA-74vq-h4q8-x6jv'),('GHSA-75px-5xx7-5xc7'),('GHSA-77g3-3j5w-64w4'),('GHSA-78xj-cgh5-2h22'),('GHSA-7hhh-6rmp-j9qf'),('GHSA-7j69-qfc3-2fq9'),
  ('GHSA-7q8q-rj6j-mhjq'),('GHSA-7r86-cg39-jmmj'),('GHSA-8452-54wp-rmv6'),('GHSA-869p-cjfg-cm3x'),('GHSA-86hp-cj9j-33vv'),('GHSA-893h-35v4-mxqx'),('GHSA-898c-q2cr-xwhg'),('GHSA-89xv-2m56-2m9x'),
  ('GHSA-8cj5-5rvv-wf4v'),('GHSA-8f4m-hccc-8qph'),('GHSA-8h8q-6873-q5fj'),('GHSA-8wgj-6wx8-h5hq'),('GHSA-923p-fr2c-g5m2'),('GHSA-93q8-gq69-wqmw'),('GHSA-955p-x3mx-jcvp'),('GHSA-968p-4wvh-cqc8'),
  ('GHSA-99w6-3xph-cx78'),('GHSA-9c47-m6qq-7p4h'),('GHSA-9fr6-4gfg-395g'),('GHSA-9g9p-9gw9-jx7f'),('GHSA-9gqv-wp59-fq42'),('GHSA-9qr9-h5gf-34mp'),('GHSA-9r2w-394v-53qc'),('GHSA-9wv6-86v2-598j'),
  ('GHSA-c2qf-rxjj-qqgw'),('GHSA-c4j6-fc7j-m34r'),('GHSA-c7qv-q95q-8v27'),('GHSA-cqmr-rcpr-cxh3'),('GHSA-cxp5-3px4-pw24'),('GHSA-f2ph-cmg5-wxc8'),('GHSA-f38q-mgvj-vph7'),('GHSA-f4gw-2p7v-4548'),
  ('GHSA-f84p-cvgm-xgjj'),('GHSA-f85h-23mf-2fwh'),('GHSA-f886-m6hf-6m8v'),('GHSA-ffhc-5mcf-pf4q'),('GHSA-fh5v-5f35-2rv2'),('GHSA-frxj-5j27-f8rf'),('GHSA-fv7c-fp4j-7gwp'),('GHSA-fvcv-3m26-pcqx'),
  ('GHSA-fx83-v9x8-x52w'),('GHSA-g4mq-6fp5-qwcf'),('GHSA-gcfj-64vw-6mp9'),('GHSA-gfr2-qpxh-qj9m'),('GHSA-gfr2-rv44-r23c'),('GHSA-ggv3-7p47-pfv8'),('GHSA-gpj5-g38j-94v9'),('GHSA-grgm-pph5-j5h7'),
  ('GHSA-gwr8-5j83-483c'),('GHSA-gx5p-jg67-6x7h'),('GHSA-gx83-3vf8-gh7j'),('GHSA-gxpj-cx7g-858c'),('GHSA-h24r-m9qc-pvpg'),('GHSA-h39q-95q5-9jfp'),('GHSA-h5x3-xfc9-m39h'),('GHSA-h64f-5h5j-jqjh'),
  ('GHSA-h653-95qw-h2mp'),('GHSA-h67p-54hq-rp68'),('GHSA-h7vf-5wrv-9fhv'),('GHSA-hcpx-6fm6-wx23'),('GHSA-hfxv-24rg-xrqf'),('GHSA-hgj6-7826-r7m5'),('GHSA-hrvq-x7jp-36xv'),('GHSA-j2h6-73x8-22c4'),
  ('GHSA-j3f2-48v5-ccww'),('GHSA-j3rv-43j4-c7qm'),('GHSA-j5f8-grm9-p9fc'),('GHSA-j667-c2hm-f2wp'),('GHSA-jggg-4jg4-v7c6'),('GHSA-jpxc-vmjf-9fcj'),('GHSA-jr5f-v2jv-69x6'),('GHSA-jvwf-75h9-cwgg'),
  ('GHSA-m429-fhmv-c6q2'),('GHSA-m7pr-hjqh-92cm'),('GHSA-m99w-x7hq-7vfj'),('GHSA-mcj8-r9mp-w47p'),('GHSA-mg66-mrh9-m8jx'),('GHSA-mh29-5h37-fv8m'),('GHSA-mh99-v99m-4gvg'),('GHSA-mhm7-754m-9p8w'),
  ('GHSA-mjf5-7g4m-gx5w'),('GHSA-mmx7-hfxf-jppx'),('GHSA-mrqx-rp3w-jpjp'),('GHSA-mwcw-c2x4-8c55'),('GHSA-p293-qw3h-jr36'),('GHSA-p3m5-59mp-jv5r'),('GHSA-p62g-jhg6-v3rq'),('GHSA-p6pp-m3f8-5c89'),
  ('GHSA-p92q-9vqr-4j8v'),('GHSA-p9j2-gv94-2wf4'),('GHSA-p9pc-299p-vxgp'),('GHSA-pf86-5x62-jrwf'),('GHSA-pfrx-2q88-qq97'),('GHSA-pjwm-pj3p-43mv'),('GHSA-pm48-cvv2-29q5'),('GHSA-pmv8-rq9r-6j72'),
  ('GHSA-pmwg-cvhr-8vh7'),('GHSA-pq67-2wwv-3xjx'),('GHSA-pr59-h9ph-3fr8'),('GHSA-px85-f4q5-jh77'),('GHSA-px8p-9vwx-vf98'),('GHSA-q2hr-2g5m-vwhr'),('GHSA-q4gf-8mx6-v5v3'),('GHSA-q4xh-88c3-wmh7'),
  ('GHSA-q6x5-8v7m-xcrf'),('GHSA-q8hg-pf8v-cxrv'),('GHSA-q8wf-6r8g-63ch'),('GHSA-qhr7-859c-m2p7'),('GHSA-qq5c-677p-737q'),('GHSA-qq89-hq3f-393p'),('GHSA-qq9h-g4jm-xgf3'),('GHSA-r2rq-3h56-fqm4'),
  ('GHSA-r39x-jcww-82v6'),('GHSA-r628-mhmh-qjhw'),('GHSA-r683-j2x4-v87g'),('GHSA-r7fv-28h4-cvq7'),('GHSA-r7wm-3cxj-wff9'),('GHSA-rgw5-rvv9-x895'),('GHSA-rmj7-2vxq-3g9f'),('GHSA-v2wj-7wpq-c8vv'),
  ('GHSA-v422-hmwv-36x6'),('GHSA-v56q-mh7h-f735'),('GHSA-v6h2-p8h4-qcjw'),('GHSA-v8jm-5vwx-cfxm'),('GHSA-vcg8-98q8-g7mj'),('GHSA-vf2m-468p-8v99'),('GHSA-vfgh-jcmw-g37q'),('GHSA-vfv6-92ff-j949'),
  ('GHSA-vh95-rmgr-6w4m'),('GHSA-vj76-c3g6-qr5v'),('GHSA-vp3h-ghgh-jr7g'),('GHSA-vp9j-rghq-8jhh'),('GHSA-vvgp-rfg2-7rr6'),('GHSA-w27v-7q3p-w38r'),('GHSA-w2vw-w76x-qr89'),('GHSA-w3vv-58gj-gw77'),
  ('GHSA-w5hq-g745-h8pq'),('GHSA-w8p5-mx5w-cpqj'),('GHSA-w9j2-pvgh-6h63'),('GHSA-wcpc-wj8m-hjx6'),('GHSA-wf5p-g6vw-rhxx'),('GHSA-wf6x-7x77-mvgw'),('GHSA-wfc6-r584-vfw7'),('GHSA-wjgm-6hv5-3cvf'),
  ('GHSA-wjv8-pxr6-5f4r'),('GHSA-wv8q-qhhj-9h54'),('GHSA-x6g4-fwcc-jj8w'),('GHSA-x92h-wmg2-6hp7'),('GHSA-x97p-jq2g-jp4f'),('GHSA-xhh6-956q-4q69'),('GHSA-xhjh-pmcv-23jw'),('GHSA-xpqw-6gx7-v673'),
  ('GHSA-xq3m-2v4x-88gg'),('GHSA-xvch-5gv4-984h'),('GHSA-xvcm-6775-5m9r'),('GHSA-xwg4-73v4-xw9w'),('GHSA-xx6v-rp6x-q39c');
SELECT count(*) AS class_w_ids FROM pg_temp.class_w;

-- ---------------------------------------------------------------- Section 1
-- Candidate set, fail-closed (ruling step 2). Read-only.
-- Active generation, OSV batch scope, every live affected_package fact stamped
-- the retired instance, none stamped any other instance, no in-flight work
-- (claimed/running/retrying, mirroring targeted_candidate_lock.go:140-144).
-- --------------------------------------------------------------------------
DROP TABLE IF EXISTS pg_temp.cand;
CREATE TEMP TABLE pg_temp.cand AS
SELECT s.scope_id, g.generation_id, s.status AS scope_status
FROM ingestion_scopes s
JOIN scope_generations g ON g.generation_id = s.active_generation_id
WHERE s.scope_id LIKE 'vuln-intel://osv/%'
  AND g.status = 'active'
  AND EXISTS (
    SELECT 1 FROM fact_records f
    WHERE f.scope_id = s.scope_id AND f.generation_id = g.generation_id
      AND f.fact_kind = 'vulnerability.affected_package' AND f.is_tombstone = false
      AND f.payload ->> 'collector_instance_id' = :'retired_instance');

DROP TABLE IF EXISTS pg_temp.classified;
CREATE TEMP TABLE pg_temp.classified AS
SELECT c.scope_id, c.generation_id, c.scope_status,
  EXISTS (
    SELECT 1 FROM fact_records f
    WHERE f.scope_id = c.scope_id AND f.generation_id = c.generation_id
      AND f.fact_kind = 'vulnerability.affected_package' AND f.is_tombstone = false
      AND (f.payload ->> 'collector_instance_id' IS DISTINCT FROM :'retired_instance')
  ) AS is_mixed,
  EXISTS (
    SELECT 1 FROM fact_work_items w
    WHERE w.scope_id = c.scope_id AND w.generation_id = c.generation_id
      AND w.status IN ('claimed', 'running', 'retrying')
  ) AS is_inflight,
  EXISTS (
    SELECT 1 FROM fact_records f
    WHERE f.scope_id = c.scope_id AND f.generation_id = c.generation_id
      AND f.fact_kind = 'vulnerability.affected_package' AND f.is_tombstone = false
      AND f.payload ->> 'collector_instance_id' = :'retired_instance'
      AND f.payload ->> 'advisory_id' IN (SELECT advisory_id FROM pg_temp.class_w)
  ) AS is_class_w
FROM pg_temp.cand c;

-- Satellite replacement per candidate advisory (Class S gates on this).
DROP TABLE IF EXISTS pg_temp.sat_adv;
CREATE TEMP TABLE pg_temp.sat_adv AS
SELECT DISTINCT f.payload ->> 'advisory_id' AS advisory_id
FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'vulnerability.affected_package' AND f.is_tombstone = false
  AND f.scope_id LIKE 'vuln-intel://osv/%'
  AND f.payload ->> 'collector_instance_id' = :'satellite_instance';

SELECT 'candidates=' || count(*)
  || ' classW=' || count(*) FILTER (WHERE is_class_w AND NOT is_mixed AND NOT is_inflight)
  || ' classS_eligible=' || count(*) FILTER (WHERE NOT is_class_w AND NOT is_mixed AND NOT is_inflight
      AND NOT EXISTS (SELECT 1 FROM fact_records f WHERE f.scope_id = classified.scope_id
        AND f.generation_id = classified.generation_id AND f.fact_kind = 'vulnerability.affected_package'
        AND f.is_tombstone = false AND f.payload ->> 'collector_instance_id' = :'retired_instance'
        AND f.payload ->> 'advisory_id' NOT IN (SELECT advisory_id FROM pg_temp.sat_adv)))
  || ' mixed=' || count(*) FILTER (WHERE is_mixed)
  || ' inflight=' || count(*) FILTER (WHERE is_inflight)
  AS classification
FROM pg_temp.classified;

-- Mixed-instance generations: must be empty; each row aborts the run.
SELECT scope_id, generation_id FROM pg_temp.classified WHERE is_mixed ORDER BY 1;
-- In-flight generations: must be empty; each row aborts the run.
SELECT scope_id, generation_id FROM pg_temp.classified WHERE is_inflight ORDER BY 1;
-- Class S remainder (stays active; follow-up re-collection issue).
SELECT c.scope_id, string_agg(DISTINCT f.payload ->> 'advisory_id', ',' ORDER BY (f.payload ->> 'advisory_id')) AS uncovered_advisories
FROM pg_temp.classified c
JOIN fact_records f ON f.scope_id = c.scope_id AND f.generation_id = c.generation_id
WHERE NOT c.is_class_w AND NOT c.is_mixed AND NOT c.is_inflight
  AND f.fact_kind = 'vulnerability.affected_package' AND f.is_tombstone = false
  AND f.payload ->> 'collector_instance_id' = :'retired_instance'
  AND f.payload ->> 'advisory_id' NOT IN (SELECT advisory_id FROM pg_temp.sat_adv)
GROUP BY c.scope_id ORDER BY c.scope_id;

-- ---------------------------------------------------------------- Section 2
-- Retire set = Class W + Class S fully satellite-covered, fail-closed on
-- mixed / in-flight. Recomputed at run time, never from a stale export.
-- --------------------------------------------------------------------------
DROP TABLE IF EXISTS pg_temp.retire_set;
CREATE TEMP TABLE pg_temp.retire_set AS
SELECT c.scope_id, c.generation_id, c.scope_status AS prior_scope_status
FROM pg_temp.classified c
WHERE NOT c.is_mixed AND NOT c.is_inflight
  AND (c.is_class_w OR NOT EXISTS (
    SELECT 1 FROM fact_records f
    WHERE f.scope_id = c.scope_id AND f.generation_id = c.generation_id
      AND f.fact_kind = 'vulnerability.affected_package' AND f.is_tombstone = false
      AND f.payload ->> 'collector_instance_id' = :'retired_instance'
      AND f.payload ->> 'advisory_id' NOT IN (SELECT advisory_id FROM pg_temp.sat_adv)));

SELECT count(*) AS retire_generations,
  count(*) FILTER (WHERE (scope_id, generation_id) IN (
    SELECT scope_id, generation_id FROM pg_temp.classified WHERE is_class_w)) AS retire_class_w,
  (SELECT count(*) FROM pg_temp.classified WHERE is_mixed OR is_inflight) AS must_be_zero_blockers
FROM pg_temp.retire_set;

-- ---------------------------------------------------------------- Section 3
-- Dry run: the exact retire body inside BEGIN ... ROLLBACK (ruling step 3).
-- The Section 4 body is byte-identical except for the COMMIT vs ROLLBACK tail;
-- The lock + UPDATE statements match Section 4 lines-for-line (verified by diff
-- of the two blocks); only the reporting tail and COMMIT vs ROLLBACK differ.
-- --------------------------------------------------------------------------
BEGIN;
SELECT g.scope_id, g.generation_id
FROM scope_generations g JOIN pg_temp.retire_set r USING (scope_id, generation_id)
ORDER BY g.scope_id, g.generation_id FOR UPDATE OF g;
SELECT count(*) AS must_be_zero_inflight_locked
FROM fact_work_items w JOIN pg_temp.retire_set r USING (scope_id, generation_id)
WHERE w.status IN ('claimed', 'running', 'retrying');
UPDATE scope_generations g
SET status = 'superseded', superseded_at = now(),
    payload = COALESCE(g.payload, '{}'::jsonb)
      || jsonb_build_object('retired_issue', '9431', 'retired_at', now())
FROM pg_temp.retire_set r
WHERE g.scope_id = r.scope_id AND g.generation_id = r.generation_id AND g.status = 'active';
UPDATE ingestion_scopes s
SET status = 'failed', active_generation_id = NULL
FROM pg_temp.retire_set r
WHERE s.scope_id = r.scope_id AND s.active_generation_id = r.generation_id;
SELECT 'dryrun_updated_generations=' || count(*) FROM scope_generations g
JOIN pg_temp.retire_set r USING (scope_id, generation_id)
WHERE g.status = 'superseded' AND g.payload ->> 'retired_issue' = '9431';
SELECT 'dryrun_cleared_scopes=' || count(*) FROM ingestion_scopes s
JOIN pg_temp.retire_set r USING (scope_id)
WHERE s.active_generation_id IS NULL;
ROLLBACK;

-- ---------------------------------------------------------------- Section 4
-- Retire transaction (ruling step 5). RUNS ONLY WITH -v retire_ok=1, and only
-- after the replacement pass (ruling step 4) is recorded in the evidence.
-- One transaction; candidates locked in (scope_id, generation_id) order
-- FOR UPDATE (retention lock doctrine). The rollback key (scope_id,
-- generation_id, prior scope status) is selected before COMMIT; capture the
-- psql output as the evidence rollback key.
-- --------------------------------------------------------------------------
\if :{?retire_ok}
BEGIN;
SELECT g.scope_id, g.generation_id
FROM scope_generations g JOIN pg_temp.retire_set r USING (scope_id, generation_id)
ORDER BY g.scope_id, g.generation_id FOR UPDATE OF g;
SELECT count(*) AS must_be_zero_inflight_locked
FROM fact_work_items w JOIN pg_temp.retire_set r USING (scope_id, generation_id)
WHERE w.status IN ('claimed', 'running', 'retrying');
UPDATE scope_generations g
SET status = 'superseded', superseded_at = now(),
    payload = COALESCE(g.payload, '{}'::jsonb)
      || jsonb_build_object('retired_issue', '9431', 'retired_at', now())
FROM pg_temp.retire_set r
WHERE g.scope_id = r.scope_id AND g.generation_id = r.generation_id AND g.status = 'active';
UPDATE ingestion_scopes s
SET status = 'failed', active_generation_id = NULL
FROM pg_temp.retire_set r
WHERE s.scope_id = r.scope_id AND s.active_generation_id = r.generation_id;
SELECT scope_id, generation_id, prior_scope_status AS rollback_key
FROM pg_temp.retire_set ORDER BY 1, 2;
COMMIT;
\else
\echo 'Section 4 retire transaction SKIPPED: re-run with -v retire_ok=1 after step 4 is evidenced.'
\endif

-- ---------------------------------------------------------------- Section 5
-- Rollback (ruling): operational SQL restoring marker rows, usable BEFORE the
-- first retention prune touches a retired row. Superseded is terminal for
-- ack/replay, so this restores status/pointer directly, restricted to rows
-- carrying the step-4 marker, still superseded, with the scope pointer still
-- NULL. After a prune, or once a satellite generation activates on the same
-- scope, rollback is re-collection only.
-- --------------------------------------------------------------------------
-- BEGIN;
-- UPDATE scope_generations g
-- SET status = 'active', superseded_at = NULL
-- WHERE g.payload ->> 'retired_issue' = '9431'
--   AND g.status = 'superseded'
--   AND EXISTS (SELECT 1 FROM ingestion_scopes s
--     WHERE s.scope_id = g.scope_id AND s.active_generation_id IS NULL);
-- UPDATE ingestion_scopes s
-- SET status = r.prior_scope_status, active_generation_id = r.generation_id
-- FROM pg_temp.retire_set r
-- WHERE s.scope_id = r.scope_id AND s.active_generation_id IS NULL
--   AND EXISTS (SELECT 1 FROM scope_generations g
--     WHERE g.scope_id = r.scope_id AND g.generation_id = r.generation_id
--       AND g.status = 'active' AND g.payload ->> 'retired_issue' = '9431');
-- COMMIT;
-- (Rollback block is commented: uncomment to execute, only inside the window.)

-- ---------------------------------------------------------------- Section 6
-- AC3 proof queries (read-only; run after ruling step 6, the post-commit
-- reducer supply-chain-impact cycle). All must hold; record exits.
-- --------------------------------------------------------------------------
-- AC3.1: candidate predicate re-run returns 0 Class W rows.
SELECT count(*) AS ac3_1_class_w_remaining
FROM ingestion_scopes s
JOIN scope_generations g ON g.generation_id = s.active_generation_id
WHERE s.scope_id LIKE 'vuln-intel://osv/%' AND g.status = 'active'
  AND EXISTS (
    SELECT 1 FROM fact_records f
    WHERE f.scope_id = s.scope_id AND f.generation_id = g.generation_id
      AND f.fact_kind = 'vulnerability.affected_package' AND f.is_tombstone = false
      AND f.payload ->> 'collector_instance_id' = :'retired_instance'
      AND f.payload ->> 'advisory_id' IN (SELECT advisory_id FROM pg_temp.class_w));
-- AC3.2: zero live retired-instance facts reachable via active_generation_id
-- for Class W advisories.
SELECT count(*) AS ac3_2_live_retired_class_w_facts
FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'vulnerability.affected_package' AND f.is_tombstone = false
  AND f.payload ->> 'collector_instance_id' = :'retired_instance'
  AND f.payload ->> 'advisory_id' IN (SELECT advisory_id FROM pg_temp.class_w);
-- AC3.3a: GHSA-67hx-6x53-jw92 live satellite facts carry both release lines.
SELECT f.scope_id, f.payload ->> 'package_name' AS pkg,
  f.payload -> 'affected_ranges' AS ranges, f.payload -> 'fixed_versions' AS fixed
FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'vulnerability.affected_package' AND f.is_tombstone = false
  AND f.payload ->> 'collector_instance_id' = :'satellite_instance'
  AND f.payload ->> 'advisory_id' = 'GHSA-67hx-6x53-jw92';
-- AC3.3b: impact answer for the 7.29.7 pin reads affected, not
-- possibly_affected / version_not_in_advisory_range.
SELECT payload ->> 'impact_status' AS impact_status, payload ->> 'match_reason' AS match_reason,
  payload ->> 'vulnerable_range' AS vulnerable_range, payload ->> 'package_name' AS pkg,
  payload ->> 'observed_version' AS observed_version
FROM fact_records
WHERE fact_kind = 'reducer_supply_chain_impact_finding' AND is_tombstone = false
  AND payload ->> 'advisory_id' = 'GHSA-67hx-6x53-jw92'
  AND payload ->> 'observed_version' = '7.29.7';
-- AC3.4: no collateral: live satellite fact count >= run-time baseline.
SELECT count(*) AS ac3_4_live_satellite_facts
FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'vulnerability.affected_package' AND f.is_tombstone = false
  AND f.scope_id LIKE 'vuln-intel://osv/%'
  AND f.payload ->> 'collector_instance_id' = :'satellite_instance';
