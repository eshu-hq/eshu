-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #7127 PR-3d shim fixture: changed-since ledger retention at scale.
-- See docs/internal/evidence/7127-changed-since-ledger-retention.md.
--
-- Apply migration 136 as shipped, then this file twice with psql variables:
--   psql -v lo=1 -v hi=5 -v big=0 -f 7127-ledger-retention-shim.sql   -- then ANALYZE (stale statistics)
--   psql -v lo=6 -v hi=800 -v big=1 -f 7127-ledger-retention-shim.sql -- no ANALYZE
-- Result: 800 scopes, each a chain g0..g25 (one root and 25 incremental links of
-- 100 deltas, 3 bucket rows each), plus scope 'big' whose g12 -> g13 link has
-- 771,201 deltas (a full-rewrite worst case at the 1.0x key count) and 4,400
-- on each other link: 2,876,801 delta rows, inserted in random order.
-- The last section adds a two-column scope_generations and the probe batches.
-- #7127 PR-3d shim fixture. Ledger tables from migration 136 as shipped.
-- $scopes typical scopes, each a chain g0..g25 (25 incremental links, 100 deltas each, 6 buckets each),
-- plus scope 'big' with 25 links of 4,400 deltas and one 771,201-delta link (g12 -> g13).
CREATE OR REPLACE FUNCTION gid(sc text, g int) RETURNS text LANGUAGE sql IMMUTABLE AS
$$ SELECT 'generation:' || md5(sc || ':' || g) $$;
CREATE OR REPLACE FUNCTION sid(i int) RETURNS text LANGUAGE sql IMMUTABLE AS
$$ SELECT 'git-repository-scope:' || md5('scope' || i) $$;

INSERT INTO changed_since_activations (scope_id, generation_id, prior_generation_id, source, activated_at)
SELECT sid(s), gid(sid(s), g), CASE WHEN g = 0 THEN NULL ELSE gid(sid(s), g - 1) END, 'sweeper',
       now() - make_interval(days => 30 - g)
FROM generate_series(:lo, :hi) s, generate_series(0, 25) g ORDER BY g, s;
INSERT INTO changed_since_activations (scope_id, generation_id, prior_generation_id, source, activated_at)
SELECT 'big', gid('big', g), CASE WHEN g = 0 THEN NULL ELSE gid('big', g - 1) END, 'sweeper', now() - make_interval(days => 30 - g)
FROM generate_series(0, 25) g WHERE :big = 1;

INSERT INTO changed_since_links (scope_id, generation_id, prior_generation_id, link_kind, digest_version, delta_rows,
    files_keys, content_entities_keys, facts_keys, computed_at)
SELECT sid(s), gid(sid(s), g), CASE WHEN g = 0 THEN '' ELSE gid(sid(s), g - 1) END,
       CASE WHEN g = 0 THEN 'root' ELSE 'incremental' END, 1, CASE WHEN g = 0 THEN 0 ELSE 100 END, 50, 400, 159, now()
FROM generate_series(:lo, :hi) s, generate_series(0, 25) g;
INSERT INTO changed_since_links (scope_id, generation_id, prior_generation_id, link_kind, digest_version, delta_rows,
    files_keys, content_entities_keys, facts_keys, computed_at)
SELECT 'big', gid('big', g), CASE WHEN g = 0 THEN '' ELSE gid('big', g - 1) END,
       CASE WHEN g = 0 THEN 'root' ELSE 'incremental' END, 1,
       CASE WHEN g = 0 THEN 0 WHEN g = 13 THEN 771201 ELSE 4400 END, 7000, 540000, 224201, now()
FROM generate_series(0, 25) g WHERE :big = 1;

-- deltas: classification cycles over 6 values, category over 3.
CREATE OR REPLACE FUNCTION cls(i int) RETURNS text LANGUAGE sql IMMUTABLE AS
$$ SELECT (ARRAY['added','updated','superseded','retired','dropped','unchanged'])[i % 6 + 1] $$;
CREATE OR REPLACE FUNCTION cat(i int) RETURNS text LANGUAGE sql IMMUTABLE AS
$$ SELECT (ARRAY['files','content_entities','facts'])[i % 3 + 1] $$;

INSERT INTO changed_since_link_deltas (scope_id, generation_id, prior_generation_id, fact_category, classification,
    stable_fact_key, prior_fact_kind, current_fact_kind, prior_state, current_state, current_tombstoned)
SELECT l.scope_id, l.generation_id, l.prior_generation_id, cat(k), cls(k),
       'content_entity:content-entity:' || md5(l.generation_id || k), 'content_entity', 'content_entity',
       sha256(convert_to('p' || k, 'UTF8')), sha256(convert_to('c' || k || l.generation_id, 'UTF8')), false
FROM changed_since_links l, LATERAL generate_series(1, l.delta_rows::int) k
WHERE l.link_kind = 'incremental'
  AND NOT EXISTS (SELECT 1 FROM changed_since_link_deltas x WHERE x.scope_id = l.scope_id AND x.generation_id = l.generation_id AND x.prior_generation_id = l.prior_generation_id)
ORDER BY random();

INSERT INTO changed_since_link_bucket_counts (scope_id, generation_id, prior_generation_id, fact_category, classification, key_count)
SELECT scope_id, generation_id, prior_generation_id, fact_category, classification, count(*)
FROM changed_since_link_deltas GROUP BY 1, 2, 3, 4, 5
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS scope_generations (generation_id text PRIMARY KEY, scope_id text NOT NULL);
INSERT INTO scope_generations SELECT generation_id, scope_id FROM changed_since_activations ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS shim_batches (name text PRIMARY KEY, scope_ids text[], gen_ids text[]);
TRUNCATE shim_batches;
INSERT INTO shim_batches VALUES
 ('B1', (SELECT array_agg(sid(s) ORDER BY s) FROM generate_series(1,100) s),
        (SELECT array_agg(gid(sid(s),0) ORDER BY s) FROM generate_series(1,100) s)),
 ('B2', ARRAY['big'], ARRAY[gid('big',12)]),
 ('B3', (SELECT array_agg(x ORDER BY o) FROM (SELECT sid(s) x, s o FROM generate_series(101,199) s UNION ALL SELECT 'big', 1000) q),
        (SELECT array_agg(x ORDER BY o) FROM (SELECT gid(sid(s),0) x, s o FROM generate_series(101,199) s UNION ALL SELECT gid('big',12), 1000) q)),
 ('B4', ARRAY[sid(1), sid(1)], ARRAY[gid(sid(1),0), gid(sid(1),1)]);
