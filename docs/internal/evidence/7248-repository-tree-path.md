# #7248 repository tree path scope

The HTTP repository tree handler and MCP `list_repository_files` use the same
handler. Previously, an unfiltered path request fetched at most 50,001 files
from `content_files` in whole-repository path order and then selected the
requested directory in Go. A directory sorting beyond that cap could return
404. The new `ContentReader.ListRepoFilesByPath` applies a literal directory
prefix before `ORDER BY relative_path LIMIT 50001`. Root requests keep the
existing whole-repository read. Language-filtered requests keep their separate
language and path predicate. The indexed ref still comes from the existing
unfiltered root context read, and selected-ref validation is unchanged.
An exact file supplied as `path` now returns 404 with a language filter; the
previous language-filtered context lookup counted the exact file and returned
an empty 200. Unfiltered exact-file paths already returned 404.

## Theory and correctness proof

On 2026-09-26 at 22:34 EDT (2026-09-27 02:34 UTC), a read-only QA
PostgreSQL query over 12,403 indexed files
returned the same 52 ordered path rows and metadata for the broad and scoped
forms (bidirectional `EXCEPT ALL` 0/0; matching SHA-256
`a5d71a547d8944b84b2fead403ab5ff26427a6f5e96dcb2749373dd9e6f2d103`).
Separate sequential warm `EXPLAIN (ANALYZE, BUFFERS)` samples measured 211.912 ms
and 12,606 buffers for the broad listing versus 93.735 ms and 401 buffers for
an anchored path query using an existing QA index. These are SQL samples,
not API or MCP latency. The QA environment received no write, DDL, settings change, or
`ANALYZE`.

A disposable PostgreSQL 18 fixture used 145,000 `content_files` rows, including
12,403 for the target repository and 52 under `top_dir`, with an ICU path
collation. The old whole-repository SQL took 25.354 ms and 8,051 buffers;
`strpos` filtering took 7.486 ms and 8,044 buffers. An escaped anchored `LIKE`
with `(repo_id, relative_path text_pattern_ops)` took 0.131 ms and 57 buffers
in the initial fixture proof, with matching 52 ordered rows and `EXCEPT ALL`
0/0. A separate three-row `%_!` fixture returned one literal child on both
`strpos` and escaped `LIKE`, with no extra or missing rows. A 100,054-path
synthetic cap check showed the legacy global page omitted all 52 `late/` files;
a scoped page returned 52 with `truncated=false`, while a directory with
50,001 descendants remained `truncated=true`.

Before #7033 added the all-repository path GIN index, the new index was
8,040 kB and built in 104.850 ms on that fixture. A one-sample
1,000-row insert probe measured 6.368 ms and 946,649 WAL bytes without it,
versus 7.898 ms and 1,074,353 WAL bytes with it. The write cost is indicative,
not a production throughput prediction. Migration 137 uses `CREATE INDEX
CONCURRENTLY IF NOT EXISTS`; first and second applications succeeded on a
separate disposable instance, and `pg_index` reported `indisvalid=true` and
`indisready=true` after the second application. Rollback is `DROP INDEX
CONCURRENTLY` on a disposable instance only; no production rollback was run.

## Rebase check with the #7033 path GIN

After main gained migrations 134-135, a fresh disposable PostgreSQL 18
fixture kept the same ICU collation, 145,000 rows, 12,403 target-repository
rows, and 52 target `top_dir/` rows. To make the new unscoped GIN relevant,
66,351 files across repositories shared that directory prefix. With the
mainline path GIN and repository path indexes present, the exact shipped
`LIKE ... ESCAPE '!'` query returned the same 52 rows as the broad read and
Go-equivalent directory filter (`EXCEPT ALL` 0/0 in both directions). In seven
sequential samples, discarding the first, the warm `EXPLAIN (ANALYZE, BUFFERS)`
median was 10.760 ms with the mainline indexes (352 shared buffers; the GIN
read 66,351 path entries), versus 0.352 ms after migration 137 added the
repository-scoped pattern index (57 buffers; 52 index entries). These are SQL
fixture timings, not endpoint p95.

A rollback-only 1,000-row insert probe on the same fixture measured warm
medians of 11.420 ms and 1,410,267 WAL bytes with the mainline indexes,
versus 12.697 ms and 1,513,945 WAL bytes with the added pattern index.
That is an indicative 11.2% insert-time and 7.4% WAL increase for this batch;
it is not a production ingestion-throughput measurement. The index keeps the
repository equality key first so common path names across repositories do not
turn the directory read into an all-repository GIN scan.

## Built method replay

A temporary Go test, removed after the run, called production
`ContentReader.ListRepoFiles` and `ListRepoFilesByPath` plus the root ref lookup
on the same disposable 145,000-row fixture. Ten alternating calls returned the
same 52 ordered files, all metadata, and indexed ref. In the first warm replay,
the nine broad-read-plus-filter samples were 17.588-53.725 ms (median
28.203 ms), versus 1.400-2.578 ms (median 1.673 ms) for scoped-read-plus-ref.
A second exactness replay passed but had heavy shared-host contention; its
timings are not used for the performance comparison. A direct `EXPLAIN` of the
shipped `LIKE ... ESCAPE '!'` query on that disposable instance returned 52
rows through `content_files_repo_path_pattern_idx`, 57 buffers, and 0.723 ms
on its first sample.

Contract Evidence: the regression first returned 404 for an existing
late-sorting directory and passed after the handler used the scoped read. Go
unit tests cover the literal pattern, repository and limit arguments, root
fallback, one-level and recursive child counts, selected ref, missing path,
exact file path, and 50,001-row scoped truncation. The built-method replay
preserved ordered file and metadata equality. Repository grant resolution is
unchanged and occurs before the file read; SQL still binds `repo_id`.

Observability Evidence: the new SQL read uses the existing `postgres.query`
span with `db.system=postgresql`, `db.sql.table=content_files`, and
`db.operation=list_repo_files_by_path`. The root ref lookup retains its
`repo_file_path_context` operation. The route's truth envelope and MCP proxy
remain unchanged. Deployed API/MCP cold and warm p95 are NOT_CHECKED pending
the owner's rollout and latency sweep.
