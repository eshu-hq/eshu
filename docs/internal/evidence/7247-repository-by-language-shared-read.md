# #7247 repository by-language shared read proof

Scope: `GET /api/v0/repositories/by-language` with a nonzero page limit. The
route previously called the aggregate and page content-index reads in
sequence. The response and grant contract stay the same. This is a partial
#7247 change; the issue's other code-inventory and language-query routes
remain open, and deployed cold/warm p95 is **NOT_CHECKED**.

## Theory before implementation

The aggregate query counted distinct repositories and files for a normalized
language family. The page query independently grouped the same matching
`content_files` rows by repository and language, then sorted a bounded page.
A single statement can derive the aggregate from the grouped rows and return
both values on one PostgreSQL statement snapshot. It needs a left-joined page
sentinel so a zero-match or high-offset page still returns the aggregate.
The repository/scope grant must filter `content_files` before either
aggregation or pagination.

Read-only ops-qa PostgreSQL 18 `EXPLAIN (ANALYZE, BUFFERS)` used the recorded
PHP family, unscoped, page probe limit 101, offset 0. Two interleaved warm
baseline reads took 275.160 ms for the aggregate plus 185.059/187.064 ms for
the page, about 460–462 ms together. The single combined read took
211.034/178.206 ms, saving about 249–284 ms of SQL execution in this warm
window. Its plan had one `content_files` Bitmap Heap Scan over 45,419 matching
rows and 18,366 exact heap blocks. Materialized grouped rows stayed in memory;
there was no temporary-file spill. The first baseline aggregate was cold and
took 1,561.750 ms, so it is not included in the warm comparison. No index,
writer, WAL, schema, data, or QA settings changed.

A same-snapshot SQL differential compared aggregates and joined page rows in
both directions. PHP returned 69 repositories and 45,419 files on each side,
identical latest timestamps, 69/69 page rows, and zero `EXCEPT ALL` differences.
The same differential returned 0/0 for an absent language; 1 repository,
9,261 files and one page row for a one-repository scope; and unchanged 69/45,419
aggregate with zero page rows at offset 1000. These cases all had zero
bidirectional page differences.

The TypeScript/TSX alias covered 17,762 files across 169 repositories. The
combined plan scanned `content_files` once, held its small grouped CTEs and
hash tables in memory (no temporary spill), and executed in 42.974 ms. This
run followed the control reads, so it is a plan check, not an independent
speedup comparison. The same-snapshot differential returned 169/169
repositories, 17,762/17,762 files, the same latest timestamp, and 128/128
per-language rows for the first 101 repositories, with zero differences.

## Implementation and local proof

The handler now asks `ContentStore.ReadRepositoriesByLanguage` for one
statement-backed aggregate and page when `limit > 0`; `limit=0` keeps the
existing count-only read. The SQL filters grants inside `language_rows`, then
materializes per-repository language rows and totals. An aggregate-left-joined
page row retains counts when the offset is past the last repository. The
catalog projection, count-ranked order, `limit+1` truncation, normalized
language family, response fields, and truth envelope remain the same. The
combined read emits the existing `postgres.query` span with
`db.operation=read_repositories_by_language` and records query, scan, and
iteration errors.

Tests were written before implementation. The new handler regression failed
with zero combined calls and two legacy calls, then passed with one combined
call. A direct-port `limit=0` regression first observed a zero aggregate and
passed after the method delegated to the existing count query. The other
cases exercise scoped grants, the admin path, alias language rows, an empty
match, an offset past the page, count-only routing, and unchanged response
data. After rebasing on current `origin/main`, the affected suite passed:

```sh
cd go
GOCACHE=<7247-worktree>/.gocache go test \
  ./internal/query/repository ./internal/query ./internal/query/querycontract \
  ./internal/query/testutil/content ./internal/queryplan ./internal/mcp \
  ./cmd/api ./cmd/mcp-server -count=1
```

A temporary Go test in `internal/query` then called the built
`ContentReader` methods through `pgx` against ops-qa PostgreSQL via a local
port forward. Its session was forced read-only and verified
`default_transaction_read_only=on` before querying. The one-off command
used the pod credential in `PGPASSWORD` and the isolated worktree Go cache:

```sh
PGOPTIONS='-c default_transaction_read_only=on' go test ./internal/query \
  -run '^Test7247LiveReadOnlyCombinedAgainstLegacy$' -count=1 -v
```

The test compared the returned aggregate and full page with
`reflect.DeepEqual` against the old
count and list methods on three PHP passes. All passed with 69 repositories,
45,419 files, and 69 page rows. The first old count was cold, so it is not a
speedup comparison. Two warm old count-plus-page calls took 166.213 and
164.919 ms; the corresponding combined calls took 71.106 and 77.766 ms.
A second exact-code pass matched empty language (0/0, no page), offset 1000
(69/45,419, no page), and a real one-repository grant (1/9,261, one row).
The temporary test and port forward were removed after the run; neither is
part of the PR. This is a read-only backend proof of the shipped SQL and Go
row decoder, not a deployed API latency measurement.

The code changes add no schema, index, or writer change. The combined read
adds one bounded `db.operation` label value to the existing query span.
The route's deployed cold/warm latency, query plan under a future corpus,
and the other #7247 endpoints remain **NOT_CHECKED**.

## Limits

This is a SQL read-path improvement, not proof that the whole deployed route
meets the one-second cold/warm p95 target. #7247 remains open; the dominant
`function_count_by_file` read and language-query tails need separate proof.
