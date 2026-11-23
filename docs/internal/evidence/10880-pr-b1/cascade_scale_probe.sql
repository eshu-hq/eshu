-- One variant of the cascade measurement; see cascade_scale.sh.
SET search_path = shim;
SELECT 'variant' AS k, :'variant' AS v;
DROP INDEX IF EXISTS shim_generation_idx;
SELECT (:'variant' = 'with') AS with_index \gset
\if :with_index
CREATE INDEX shim_generation_idx ON shim_supports (generation_id);
\endif
VACUUM ANALYZE shim_supports;
SELECT pg_size_pretty(pg_relation_size('shim_scope_generation_idx')) AS scope_generation_idx,
       coalesce(pg_size_pretty(pg_relation_size(to_regclass('shim.shim_generation_idx'))), 'none') AS generation_idx;
SET plan_cache_mode = force_generic_plan;
PREPARE probe(text) AS DELETE FROM ONLY shim_supports WHERE generation_id = $1;
\echo '--- cascade probe, three generations (first, middle, last), buffers and Index Searches'
BEGIN;
EXPLAIN (ANALYZE, BUFFERS, COSTS off) EXECUTE probe('gen:1');
ROLLBACK;
BEGIN;
EXPLAIN (ANALYZE, BUFFERS, COSTS off) EXECUTE probe('gen:' || (:gens / 2));
ROLLBACK;
BEGIN;
EXPLAIN (ANALYZE, BUFFERS, COSTS off) EXECUTE probe('gen:' || :gens);
ROLLBACK;
\echo '--- real cascade, 50 generations in one DELETE FROM the parent table'
RESET plan_cache_mode;
BEGIN;
EXPLAIN (ANALYZE, BUFFERS, COSTS off) DELETE FROM shim_generations
 WHERE generation_id IN (SELECT 'gen:' || g FROM generate_series(1000, 1049) AS g);
ROLLBACK;
