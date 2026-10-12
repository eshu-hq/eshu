# Evidence: #7735 — the language-query ordered page read heap-walks other repositories' rows before LIMIT

Scope: `SearchEntitiesByLanguageAndTypeForAccess`
(`go/internal/query/content_reader_entity_search.go`), the content read behind
`POST /api/v0/code/language-query`. The fix is one index, migration
`174_content_entities_repo_language_type_path_idx.sql`:
`content_entities (repo_id, language, entity_type, relative_path, start_line,
entity_name)`. No Go SQL changes.

This is a **Prove-The-Theory-First** record. The theory was re-derived on a
scratch clone (not trusted blind): the BEFORE plan was reproduced there at the
issue's magnitude (~80k buffers), then the candidate index was built and the
AFTER plan measured on the same parameters. No DDL ran on production.

Repository ids in this note are scratch placeholders, not production ids.

## The defect

The ordered page read filters by `(repo_id, language, entity_type)` and orders
by `(relative_path, start_line, entity_name)` with `LIMIT 10`. For a
repository whose matching rows sort late in the corpus-wide
`(language, entity_type, relative_path, ...)` index (migration 107), the
planner serves the ORDER BY from that index and applies `repo_id` as a heap
Filter, walking and heap-fetching every earlier repository's matching rows
before the page fills.

Measured on the scratch clone (container `eshu-7735-theory`, PG 18.6,
`shared_buffers` 128 MB, defaults otherwise, `track_io_timing = on`,
520,000 `content_entities` rows; `repo_big_php` holds 182,000 rows, 32,000 of
them php Function, with `zzz_top/` paths that sort after the corpus's
70,000 `aaa/` php Function rows in other repositories; `ANALYZE` after every
shape change; per-statement custom plans via psql):

```text
->  Index Scan using content_entities_language_type_path_idx
      Index Cond: (language = 'php' AND entity_type = 'Function')
      Filter: (repo_id = 'repo_big_php')   Rows Removed by Filter: 78933
      Buffers: shared hit=80234   actual time=65.533..65.542 rows=10
Execution Time: 65.649 ms warm (first-touch 227.578 ms)
```

The full BEFORE plan is saved outside the repo (operator scratch
`/tmp/eshu7735/before_full.txt`). Cost follows the rows that sort before the
repository, not the page size — the issue's ~80,000 buffers at 150–171 ms warm
on the 1.88M-row snapshot is the same mechanism one corpus scale up.

## Hypothesis ledger

| candidate | cheapest proof | before | after | disposition |
| --- | --- | --- | --- | --- |
| Sort-friendly btree `(repo_id, language, entity_type, relative_path, start_line, entity_name)` | scratch `EXPLAIN (ANALYZE, BUFFERS)` | page 80,234 buffers, 65 ms warm | page 13 buffers, 0.07 ms warm, no Sort, rows=10 | proven on the clone, production confirmation pending |
| Covering index (INCLUDE payload) | rejected by inspection | — | — | rejected: `source_cache`/`metadata` are unbounded; a btree leaf tuple cannot exceed ~2.7 KiB, so covering them risks a failed build and an INVALID index |
| Predicate-only rewrite | prior art | — | — | rejected: #7729 already measured three rewrites trading failure classes; no rewrite puts the ORDER BY key behind the equality |
| Drop `content_entities_repo_idx` (prefix of the new key) | not in scope | — | — | owned by #7759; that review shows it is not idle, so a drop needs plan evidence first |

## Scratch proof of the index

Build: `CREATE INDEX CONCURRENTLY` took 3.774 s, size 45 MB at 520k rows
(~163 MB extrapolated to a 1.88M-row corpus, against 14 MB for the 164 gate
index at 1.88M rows).

AFTER, same parameters, five warm samples:

```text
->  Index Scan using content_entities_repo_language_type_path_idx
      Index Cond: (repo_id = 'repo_big_php' AND language = 'php' AND entity_type = 'Function')
      Buffers: shared hit=13   actual time=0.048..0.070 rows=10
Execution Time: 0.140–0.145 ms warm
```

No Sort node, no Filter, `rows=10`: the scan stops at the page. First-touch
(after a container restart, cold shared buffers): 29.444 ms total, of which
the page scan is 13 buffers with 0.070 ms of I/O; the remainder is the gate
InitPlan, which the planner answers as a Seq Scan on this synthetic clone
(`repo_big_php` is 35% of the table, so the estimate favors it). That Seq Scan
is a synthetic-stats artifact, orthogonal to this change: on production the
gate already costs 4 buffers through the 164 index, and it is byte-identical
before and after here.

The answer does not move: the 10 returned rows with and without the index are
byte-identical (`cmp` clean).

No-regression arms, same clone, with vs without the index:

| case | without | with |
| --- | --- | --- |
| Small matching repo (2,233 php Function rows), page | 0.443 ms, Incremental Sort over `repo_path_start_idx` | 0.302 ms, new index, no sort |
| Corpus-empty zero-match (hcl Function, unscoped) | 0.049 ms, page never executed | 0.041 ms, same shape |
| Repo-empty (kotlin Function) | 0.052 ms, gate on the 164 index | 0.035 ms, gate on the new index |
| OR-language variant (javascript\|jsx) | 0.087 ms, `repo_path_start_idx` + Incremental Sort | 0.170 ms, same plan (cold pages; same shape, sub-ms both) |
| Unscoped matching (go Function) | 0.111 ms, 107 index ordered walk | 0.126 ms, same plan |

Nothing regresses: every arm keeps its plan at the same sub-millisecond cost,
and the small-repository page additionally loses its sort. The OR variant
cannot drive one ordered scan by construction, so it keeps the repo-first plan
— unchanged by design.

## Cost on production

Not measured. Estimate from the clone: ~163 MB at 1.88M rows.
`content_entities_repo_idx` becomes a prefix of the new key; it stays (no
`DROP` in the migration tree). Its removal is reviewed separately in
eshu-hq/eshu#7759.

Insert cost, from the `wal_arm.sql` scripts: a 60,000-row sample reinserted
into a btree-only copy of `content_entities` (the two GIN trigram indexes
dropped to bound the run; GIN cost is the same in both arms), arms alternated
twice, `CHECKPOINT` before each, `EXPLAIN (ANALYZE, WAL)`:

| arm | WAL records | WAL bytes | insert time |
| --- | --- | --- | --- |
| without the index, round 1 / 2 | 548,099 / 548,099 | 62,339,813 / 62,339,813 | 1,555.7 / 1,542.7 ms |
| with the index, round 1 / 2 | 608,937 / 608,937 | 72,962,131 / 72,962,131 | 1,944.5 / 1,933.5 ms |

The index adds about 1.01 WAL records and about 177 bytes of WAL per inserted
row (+11.1% records and +17.0% bytes on this btree-only copy; the relative
figure is smaller on production, where the GIN indexes add WAL in both arms).
Both rounds are bit-identical (`fpi=1` in all four arms), so no
full-page-image noise pollutes the comparison.

## Cold cache

Warm numbers above are measured; first-touch BEFORE was 227.578 ms on the
clone (with read I/O). Production cold arithmetic from the issue stands: at
0.1–0.17 ms per cold page, 80k buffers is 8–14 s, the same order as the slow
first calls in the #8017 packet. Not reproduced against production here.

## Production confirmation (not yet done)

After migration 174 is applied to the production environment, run read-only
`EXPLAIN (ANALYZE, BUFFERS)` of the page query for the PHP-heavy repository
on the read replica. Expected: the page is an Index Scan on
`content_entities_repo_language_type_path_idx` with a three-column Index Cond,
no Sort, single-digit buffers, and the returned rows unchanged.

## Performance Evidence

Performance Evidence: the page read for a 32,000-row php Function repository
was an Index Scan on `content_entities_language_type_path_idx` with
`Rows Removed by Filter: 78933` and 80,234 buffers (65.6 ms warm). With
migration 174 the same parameters are an Index Scan on the new index with a
full three-column Index Cond, no Sort, 13 buffers (0.14 ms warm), and
byte-identical rows.

## Observability Evidence

No-Observability-Change: the change adds one btree index and no code. Index
build progress and size are visible through `pg_stat_progress_create_index`
and `pg_stat_user_indexes`; the query path keeps its existing `postgres.query`
span (`db.operation` = `search_entities_by_language_and_type`).
