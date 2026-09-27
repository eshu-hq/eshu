-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #7127 PR-3a scale fixture for the changed-since link writer (gates G5-G8).
-- Generator of record: docs/internal/evidence/7127-link-writer-scale.py, which
-- applies the schema, runs this file, builds the indexes and drives
-- go/internal/storage/postgres/freshness/links/scale_live_test.go.
--
-- Deterministic: every value derives from the scope name, the generation
-- label and generate_series; no random() except the load order, which only
-- spreads heap correlation (the shim's 0.11). The row generator is the
-- #7127 shim's (scratchpad shim7127b/fixture_fn.sql), unchanged: a 1.0x scope
-- is 12,400 files, 242,000 content entities, 506,000 other facts and 260,000
-- reducer_ rows per full generation, about 1.03M rows.
--
-- Scopes: four 1.0x targets r_t1..r_t4 with full generations F0 (superseded)
-- and F1 (active), and 30 noise scopes of 25,000 rows per generation.
CREATE OR REPLACE FUNCTION fpath(f int) RETURNS text LANGUAGE sql IMMUTABLE AS
$$ SELECT 'src/pkg' || (f % 97) || '/file_' || f || '.go' $$;
CREATE OR REPLACE FUNCTION filler(seed text, n int) RETURNS text LANGUAGE sql IMMUTABLE AS
$$ SELECT string_agg(md5(seed || g::text), '') FROM generate_series(1, n) g $$;
CREATE OR REPLACE FUNCTION fdeleted(f int, g int) RETURNS boolean LANGUAGE sql IMMUTABLE AS
$$ SELECT (g >= 3 AND f BETWEEN 361 AND 365) OR (g >= 4 AND f BETWEEN 461 AND 465) $$;
CREATE OR REPLACE FUNCTION fchanged_at(f int, g int) RETURNS boolean LANGUAGE sql IMMUTABLE AS
$$ SELECT (g = 3 AND f BETWEEN 301 AND 360) OR (g = 4 AND f BETWEEN 401 AND 460) $$;
CREATE OR REPLACE FUNCTION fver(f int, g int) RETURNS int LANGUAGE sql IMMUTABLE AS
$$ SELECT (g >= 3 AND f BETWEEN 301 AND 360)::int + (g >= 4 AND f BETWEEN 401 AND 460)::int $$;

-- Full truth of a scope at state g (0..4): non-tombstone collector rows only.
CREATE OR REPLACE FUNCTION truth(sc text, g int, nf int, ne int, nd int)
RETURNS TABLE (fact_kind text, stable_fact_key text, source_uri text, payload jsonb, dup int, owner int)
LANGUAGE sql STABLE AS $$
  SELECT 'file', 'file:' || fpath(f), fpath(f),
         jsonb_build_object('path', fpath(f), 'lang', 'go', 'v', fver(f, g), 'parsed_file_data', filler('f' || f, 90)), 1, f
  FROM generate_series(1, nf) f WHERE NOT fdeleted(f, g)
  UNION ALL
  SELECT 'content', 'content:' || fpath(f), fpath(f),
         jsonb_build_object('path', fpath(f), 'v', fver(f, g), 'content_body', filler('c' || f, 75)), 1, f
  FROM generate_series(1, nf) f WHERE NOT fdeleted(f, g)
  UNION ALL
  SELECT 'content_entity', 'content_entity:content-entity:' || md5('e' || e), fpath(f),
         jsonb_build_object('entity_id', md5('e' || e), 'entity_type', 'Function', 'relative_path', fpath(f),
           'entity_name', 'fn' || e || '_v' || ((g >= 1 AND e BETWEEN 1 AND 2420)::int + (g >= 2 AND e BETWEEN 3001 AND 5420)::int
                                                + (g >= 3 AND f BETWEEN 301 AND 360 AND e % 10 = 3)::int
                                                + (g >= 4 AND f BETWEEN 401 AND 460 AND e % 10 = 4)::int),
           'source_cache', repeat(md5('sc' || e), 25),
           'indexed_at', '2026-09-2' || g || 'T00:00:00Z'), 1, f
  FROM generate_series(1, ne) e, LATERAL (SELECT (e - 1) % nf + 1 AS f, (e - 1) / nf AS k) x
  WHERE NOT fdeleted(f, g)
    AND NOT (g >= 3 AND f BETWEEN 301 AND 360 AND k % 6 = 0)
    AND NOT (g >= 4 AND f BETWEEN 401 AND 460 AND k % 6 = 0)
  UNION ALL
  SELECT 'content_entity', 'content_entity:new' || h || ':' || f, fpath(f),
         jsonb_build_object('entity_id', 'new' || h || f, 'entity_type', 'Function', 'relative_path', fpath(f),
           'entity_name', 'new' || h || '_' || f, 'source_cache', repeat(md5('n' || f), 25),
           'indexed_at', '2026-09-2' || g || 'T00:00:00Z'), 1, f
  FROM (VALUES (3, 301), (4, 401)) hv(h, lo), generate_series(lo, lo + 59) f
  WHERE g >= h
  UNION ALL
  SELECT CASE WHEN d = 40001 THEN CASE WHEN g = 1 THEN 'kindflip_b' ELSE 'kindflip_a' END
              ELSE (ARRAY['documentation_link','documentation_section','terraform_block','kubernetes_resource'])[d % 4 + 1] END,
         'fact:' || md5('d' || d), fpath(f),
         CASE WHEN r = 1 THEN jsonb_build_object('ref', d, 'body', repeat(md5('b' || d), 17))
              ELSE jsonb_build_object('ref', d, 'dup', 2, 'x', CASE WHEN d <= 30100 THEN 'x' || least(g, 2) ELSE 'x' END) END,
         r, f
  FROM generate_series(1, nd) d, LATERAL (SELECT (d - 1) % nf + 1 AS f) x,
       LATERAL generate_series(1, CASE WHEN d BETWEEN 30001 AND 30500 THEN 2 ELSE 1 END) r
  WHERE NOT fdeleted(f, g)
    AND NOT (d > nd - 1600 AND d > nd - 1600 + 800 * least(g, 2))
    AND NOT (g >= 1 AND d BETWEEN 11001 AND 11320) AND NOT (g >= 2 AND d BETWEEN 12001 AND 12320)
    AND NOT (g >= 1 AND d BETWEEN 21001 AND 21800) AND NOT (g >= 2 AND d BETWEEN 22001 AND 22800)
  UNION ALL
  SELECT 'repository', 'repository:' || sc, NULL, jsonb_build_object('repo_id', sc, 'source_run_id', 'run' || g), 1, 0
$$;

-- Rows of one generation. label F0/F1/F2 (full, activated, +reducer), D3/D4 (delta), O4 (full oracle of D4).
CREATE OR REPLACE FUNCTION gen_rows(sc text, label text, nf int, ne int, nd int, nred int)
RETURNS TABLE (fact_kind text, stable_fact_key text, source_uri text, payload jsonb, dup int, is_tombstone boolean)
LANGUAGE sql STABLE AS $$
  WITH p AS (SELECT substr(label, 2)::int AS g, left(label, 1) AS t)
  -- full generations: truth
  SELECT t.fact_kind, t.stable_fact_key, t.source_uri, t.payload, t.dup, false
  FROM p, truth(sc, p.g, nf, ne, nd) t WHERE p.t IN ('F', 'O')
  UNION ALL
  -- full-gen tombstones (F1, F2): the 320 retired fact keys
  SELECT (ARRAY['documentation_link','documentation_section','terraform_block','kubernetes_resource'])[d % 4 + 1],
         'fact:' || md5('d' || d), fpath((d - 1) % nf + 1), '{}'::jsonb, 1, true
  FROM p, generate_series(10000 + p.g * 1000 + 1, 10000 + p.g * 1000 + 320) d WHERE p.t = 'F' AND p.g IN (1, 2)
  UNION ALL
  -- reducer rows in activated full generations
  SELECT 'reducer_eshu_search_document', 'reducer_doc:' || i, NULL, jsonb_build_object('d', repeat(md5('rd' || i), 20)), 1, false
  FROM p, generate_series(1, nred) i WHERE p.t = 'F'
  UNION ALL
  -- delta generations: every truth row owned by a changed file
  SELECT t.fact_kind, t.stable_fact_key, t.source_uri, t.payload, t.dup, false
  FROM p, truth(sc, p.g, nf, ne, nd) t WHERE p.t = 'D' AND fchanged_at(t.owner, p.g)
  UNION ALL
  -- delta generations: repository fact carrying the delta paths
  SELECT 'repository', 'repository:' || sc, NULL,
         jsonb_build_object('repo_id', sc, 'source_run_id', 'run' || p.g, 'delta_generation', true,
           'delta_relative_paths', (SELECT jsonb_agg(fpath(f) ORDER BY f) FROM generate_series(p.g * 100 + 1, p.g * 100 + 60) f),
           'delta_deleted_relative_paths', (SELECT jsonb_agg(fpath(f) ORDER BY f) FROM generate_series(p.g * 100 + 61, p.g * 100 + 65) f)), 1, false
  FROM p WHERE p.t = 'D'
  UNION ALL
  -- file/content tombstones for deleted paths: in the delta, and in the oracle for the same step
  SELECT k, k2 || fpath(f), fpath(f), '{}'::jsonb, 1, true
  FROM p, generate_series(p.g * 100 + 61, p.g * 100 + 65) f, (VALUES ('file', 'file:'), ('content', 'content:')) kk(k, k2)
  WHERE p.t IN ('D', 'O')
$$;

CREATE OR REPLACE FUNCTION gid(sc text, label text) RETURNS text LANGUAGE sql IMMUTABLE AS
$$ SELECT md5(sc || label) || md5(label || sc) $$;

CREATE TABLE fixture_scopes (sc text PRIMARY KEY, nf int, ne int, nd int, nred int);
INSERT INTO fixture_scopes
SELECT 'git-repository-scope:repository:r_t' || i, 12400, 242000, 506000, 260000 FROM generate_series(1, 4) i;

INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
                              observed_at, ingested_at, status, active_generation_id)
SELECT sc, 'repository', 'git', sc, 'git', sc, now(), now(), 'active', NULL FROM fixture_scopes
UNION ALL
SELECT 'git-repository-scope:repository:r_noise' || s, 'repository', 'git', 'r_noise' || s, 'git', 'n' || s,
       now(), now(), 'active', NULL
FROM generate_series(1, 30) s;

INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status,
                               activated_at, superseded_at)
SELECT gid(sc, l), sc, 'snapshot', false, now() - (10 - o) * interval '1 hour', now(),
       CASE WHEN l = 'F1' THEN 'active' ELSE 'superseded' END,
       now() - (10 - o) * interval '1 hour',
       CASE WHEN l = 'F0' THEN now() - 9 * interval '1 hour' END
FROM fixture_scopes, (VALUES ('F0', 0), ('F1', 1)) v(l, o)
UNION ALL
SELECT gid('noise' || s, g), 'git-repository-scope:repository:r_noise' || s, 'snapshot', false, now(), now(),
       CASE WHEN g = 'b' THEN 'active' ELSE 'superseded' END, now(), NULL
FROM generate_series(1, 30) s, (VALUES ('a'), ('b')) v(g);

SET work_mem = '2GB';
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_confidence, source_system, source_fact_key, source_uri, observed_at, ingested_at,
                          is_tombstone, payload)
SELECT md5(gen || '|' || k || '|' || dup || '|' || tomb) || md5(k || gen || dup), scope_id, gen, kind, k, 'git',
       'observed', 'git', k, uri, now(), now(), tomb, payload
FROM (
  SELECT s.sc AS scope_id, gid(s.sc, l) AS gen, r.fact_kind AS kind, r.stable_fact_key AS k,
         r.source_uri AS uri, r.payload, r.dup, r.is_tombstone AS tomb
  FROM fixture_scopes s, (VALUES ('F0'), ('F1')) v(l), LATERAL gen_rows(s.sc, l, s.nf, s.ne, s.nd, s.nred) r
  UNION ALL
  SELECT 'git-repository-scope:repository:r_noise' || s, gid('noise' || s, g), 'documentation_link',
         'noise:' || i, 'docs/n' || (i % 500) || '.md',
         jsonb_build_object('ref', i, 'body', repeat(md5('n' || s || g || i), 18)), 1, false
  FROM generate_series(1, 30) s, (VALUES ('a'), ('b')) v(g), generate_series(1, 25000) i
) x
ORDER BY random();

UPDATE ingestion_scopes s SET active_generation_id = g.generation_id
FROM scope_generations g WHERE g.scope_id = s.scope_id AND g.status = 'active';
