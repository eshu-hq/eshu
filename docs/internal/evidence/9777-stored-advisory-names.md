# Stored advisory package names read (#9777)

## What changed at runtime

`FactStore.ListStoredAdvisoryPackageNames`
(`go/internal/storage/postgres/stored_advisory_names.go`) is a new read. Given
OSV advisory ids it returns the `(ecosystem, package_name, package_id)` rows Eshu
stores for each id in active generations, from `vulnerability.affected_package`
facts.
The change-driven re-query design (#9777, slice 1 item 2) unions these with the
names a changed OSV record carries now, so a record that dropped a name still
re-queries the old name (6 of 768 modified records dropped one in the replay).
Nothing calls it yet: the feed reader and the replan queue are not part of this
slice.

`package_id` is in the row because `package_name` alone is not the consumer key.
The fact stores the packageidentity normalized name, which keeps only the
artifact for Maven and only the last segment for Swift, with the group or
namespace split off. In the QA environment all 137 Maven rows have no `:` in
`package_name` and carry the group in `package_id`. The caller rebuilds
`group:artifact` from `package_id`. The stored ecosystem is the packageidentity
canonical spelling (Go is `gomod`), which the caller maps to the derivation
spelling with `workflow.CanonicalOwnedPackageEcosystem`.

- Scope: active generations only (the scope's `active_generation_id` plus an
  active `scope_generations` row), non-tombstoned `vulnerability.affected_package`
  facts, rows with a blank ecosystem or package name skipped.
- Limit: the id list runs in chunks of 500 ids per statement
  (`storedAdvisoryNameChunkSize`). The chunk size is a planning bound; the
  result is the union over chunks.
- Timeout: no statement timeout of its own. It runs under the caller's context.
- Cardinality: one row per distinct
  `(advisory_id, ecosystem, package_name, package_id)`. In the QA environment
  1,180 ids resolve to 57 ids and 63 rows (84 facts), and no row has a blank
  `package_id`.
- Scale: the anchor reads every row the index holds for an id across its six
  fact kinds (602 candidates for 85 affected-package rows here), so cost follows
  rows per id. The largest advisory in the QA environment has 44 affected-package
  rows. An advisory with thousands of rows is unmeasured.
- Locks: one SELECT per chunk, ACCESS SHARE only. No write, no new index, no
  migration, no schema change.
- Concurrency: stateless read. Chunks run sequentially on one connection. It
  shares nothing with any queue, lease or claim path.

## Why the statement has a MATERIALIZED anchor

The first draft joined the active scopes and generations to `fact_records` in
one statement. On the QA read replica the planner estimated one row for the
scope join and probed `fact_records` once per active scope instead of using
the advisory index. Cost then followed the scope count, not the id count.

Rejected plan 1, active joins in the same statement (1,180 ids, cold cache):

```text
Unique  (cost=806.22..806.23 rows=1 width=96) (actual time=1020.600..1020.623 rows=63.00 loops=1)
  Buffers: shared hit=9886 read=1889
  I/O Timings: shared read=958.418
  ->  Sort  (cost=806.22..806.23 rows=1 width=96) (actual time=1020.599..1020.607 rows=80.00 loops=1)
        Sort Key: ((fact.payload ->> 'advisory_id'::text)), (lower(btrim((fact.payload ->> 'ecosystem'::text)))), (btrim((fact.payload ->> 'package_name'::text)))
        Sort Method: quicksort  Memory: 29kB
        Buffers: shared hit=9886 read=1889
        I/O Timings: shared read=958.418
        ->  Nested Loop  (cost=499.36..806.21 rows=1 width=96) (actual time=94.152..1020.446 rows=80.00 loops=1)
              Buffers: shared hit=9883 read=1889
              I/O Timings: shared read=958.418
              ->  Hash Join  (cost=495.91..800.64 rows=1 width=489) (actual time=72.763..78.682 rows=1933.00 loops=1)
                    Hash Cond: ((scope.scope_id = generation.scope_id) AND (scope.active_generation_id = generation.generation_id))
                    Buffers: shared hit=1198 read=155
                    I/O Timings: shared read=67.691
                    ->  Seq Scan on ingestion_scopes scope  (cost=0.00..294.15 rows=2015 width=299) (actual time=0.015..1.196 rows=2023.00 loops=1)
                          Buffers: shared hit=274
                    ->  Hash  (cost=467.42..467.42 rows=1899 width=190) (actual time=72.729..72.731 rows=1933.00 loops=1)
                          Buckets: 2048  Batches: 1  Memory Usage: 1180kB
                          Buffers: shared hit=767 read=155
                          I/O Timings: shared read=67.691
                          ->  Bitmap Heap Scan on scope_generations generation  (cost=200.17..467.42 rows=1899 width=190) (actual time=68.718..71.245 rows=1933.00 loops=1)
                                Recheck Cond: ((status = ANY ('{<advisory ids elided>}'::text[])) AND (status = 'active'::text))
                                Heap Blocks: exact=765
                                Buffers: shared hit=767 read=155
                                I/O Timings: shared read=67.691
                                ->  BitmapAnd  (cost=200.17..200.17 rows=286 width=0) (actual time=68.575..68.577 rows=0.00 loops=1)
                                      Buffers: shared hit=2 read=155
                                      I/O Timings: shared read=67.691
                                      ->  Bitmap Index Scan on scope_generations_active_pending_activity_idx  (cost=0.00..81.27 rows=1899 width=0) (actual time=39.208..39.208 rows=2377.00 loops=1)
                                            Index Searches: 1
                                            Buffers: shared hit=2 read=63
                                            I/O Timings: shared read=38.762
                                      ->  Bitmap Index Scan on scope_generations_active_scope_idx  (cost=0.00..117.70 rows=1899 width=0) (actual time=29.347..29.347 rows=2067.00 loops=1)
                                            Index Searches: 1
                                            Buffers: shared read=92
                                            I/O Timings: shared read=28.929
              ->  Index Scan using fact_records_vulnerability_affected_package_active_scan_idx on fact_records fact  (cost=3.45..5.55 rows=1 width=889) (actual time=0.438..0.486 rows=0.04 loops=1933)
                    Index Cond: ((scope_id = scope.scope_id) AND (generation_id = scope.active_generation_id))
                    Filter: (((payload ->> 'advisory_id'::text) = ANY ('{<advisory ids elided>}'::text[])) AND (btrim(COALESCE((payload ->> 'ecosystem'::text), ''::text)) <> ''::text) AND (btrim(COALESCE((payload ->> 'package_name'::text), ''::text)) <> ''::text))
                    Rows Removed by Filter: 1
                    Index Searches: 1933
                    Buffers: shared hit=8541 read=1734
                    I/O Timings: shared read=890.727
Planning:
  Buffers: shared hit=3732 read=50
  I/O Timings: shared read=0.976
Planning Time: 22.880 ms
Execution Time: 1020.686 ms
```

Adding a `MATERIALIZED` candidates CTE removed that plan, but with the anchor
predicate `fact_kind = 'vulnerability.affected_package'` the planner took
`fact_records_vulnerability_package_purl_lookup_idx` and filtered 2,507 rows
for 85 hits. That predicate admits the purl and active-scan partial indexes as
well as the advisory index.

Rejected plan 2, anchor on the single kind (1,180 ids, cold cache):

```text
Unique  (cost=6039.93..6039.95 rows=1 width=96) (actual time=547.767..547.787 rows=63.00 loops=1)
  Buffers: shared hit=3720 read=998
  I/O Timings: shared read=526.030
  CTE candidates
    ->  Index Scan using fact_records_vulnerability_package_purl_lookup_idx on fact_records fact  (cost=3.20..5448.50 rows=8805 width=204) (actual time=9.385..534.804 rows=85.00 loops=1)
          Filter: ((payload ->> 'advisory_id'::text) = ANY ('{<advisory ids elided>}'::text[]))
          Rows Removed by Filter: 2422
          Index Searches: 1
          Buffers: shared hit=2760 read=977
          I/O Timings: shared read=519.567
  ->  Sort  (cost=591.43..591.44 rows=1 width=96) (actual time=547.766..547.772 rows=80.00 loops=1)
        Sort Key: candidate.advisory_id, candidate.ecosystem, candidate.package_name
        Sort Method: quicksort  Memory: 29kB
        Buffers: shared hit=3720 read=998
        I/O Timings: shared read=526.030
        ->  Nested Loop  (cost=324.78..591.42 rows=1 width=96) (actual time=13.028..547.663 rows=80.00 loops=1)
              Join Filter: ((scope.scope_id = generation.scope_id) AND (scope.active_generation_id = generation.generation_id))
              Buffers: shared hit=3717 read=998
              I/O Timings: shared read=526.030
              ->  Hash Join  (cost=324.38..590.26 rows=2 width=459) (actual time=12.985..545.505 rows=80.00 loops=1)
                    Hash Cond: ((candidate.scope_id = scope.scope_id) AND (candidate.generation_id = scope.active_generation_id))
                    Buffers: shared hit=3294 read=992
                    I/O Timings: shared read=525.974
                    ->  CTE Scan on candidates candidate  (cost=0.00..220.12 rows=8717 width=160) (actual time=9.388..534.919 rows=85.00 loops=1)
                          Filter: ((ecosystem <> ''::text) AND (package_name <> ''::text))
                          Storage: Memory  Maximum Storage: 62kB
                          Buffers: shared hit=2760 read=977
                          I/O Timings: shared read=519.567
                    ->  Hash  (cost=294.15..294.15 rows=2015 width=299) (actual time=3.582..3.583 rows=1933.00 loops=1)
                          Buckets: 2048  Batches: 1  Memory Usage: 617kB
                          Buffers: shared hit=365
                          ->  Seq Scan on ingestion_scopes scope  (cost=0.00..294.15 rows=2015 width=299) (actual time=0.020..1.401 rows=2023.00 loops=1)
                                Buffers: shared hit=274
              ->  Index Scan using scope_generations_active_scope_idx on scope_generations generation  (cost=0.40..0.56 rows=1 width=190) (actual time=0.024..0.024 rows=1.00 loops=80)
                    Index Cond: (scope_id = candidate.scope_id)
                    Filter: (generation_id = candidate.generation_id)
                    Index Searches: 80
                    Buffers: shared hit=314 read=6
                    I/O Timings: shared read=0.056
Planning:
  Buffers: shared hit=3728
Planning Time: 23.683 ms
Execution Time: 547.839 ms
```

The shipped statement anchors on the exact `fact_kind IN (...)` list of
`fact_records_vulnerability_active_advisory_lookup_v2_idx`, so that index is the
only partial index the predicate can use, and narrows to affected packages after
the index read. `TestStoredAdvisoryNamesQueryKindListMatchesTheIndexPredicate`
derives the list from the shipped index DDL, so the two cannot drift.

## Performance Evidence:

Environment: the QA environment read replica (`pg_is_in_recovery()` = true),
read-only session (`default_transaction_read_only=on`, `statement_timeout`
120 s), `EXPLAIN (ANALYZE, BUFFERS)` of the shipped statement text with the id
list inlined, aggregates only. The 1,180 ids are the changed-id set from the
#9777 replay shim. Rejected plans and the cold run: 2026-11-01 20:05Z to 20:09Z,
on the statement before `package_id` was added. Shipped statement:
2026-11-01 21:23:10Z to 21:23:23Z.

| Shape | Plan | Execution time | Buffers |
| --- | --- | --- | --- |
| Rejected plan 1, 1,180 ids, cold | scope-led nested loop, 1,933 probes | 1,020.7 ms | hit 9,886 read 1,889 |
| Rejected plan 2, 1,180 ids, cold | purl index, full filtered scan | 547.8 ms | hit 3,720 read 998 |
| Before `package_id`, 1,180 ids, first run, cold | advisory lookup index, 336 searches | 509.5 ms | hit 3,225 read 953, shared read time 490.7 ms |
| Before `package_id`, 1,180 ids, warm | advisory lookup index, 336 searches | 12.5 ms | hit 4,178 |
| Shipped, 1,180 ids, first run of the session | advisory lookup index, 336 searches | 19.0 ms | hit 3,463 read 1,129 |
| Shipped, 1,180 ids, warm, two runs | advisory lookup index, 336 searches | 12.3 and 12.5 ms | hit 4,592 |
| Shipped, 500-id chunk, warm | advisory lookup index, 238 searches | 9.9 ms | hit 3,575 |
| Shipped, one id, warm | advisory lookup index, 1 search | 0.08 ms | hit 8 |

The cold figure is bounded by reads of index and heap pages: 490.7 of the 509.5
ms is shared read time, 953 pages. The replay shim measured 445 ms for the same
ids on the same index with a two-kind predicate (378 index searches). The
shipped statement's first run is not a cold figure: its 1,129 reads came from
the operating system cache, because the replica's caches cannot be dropped from
a read-only session. Adding `package_id` reads the same rows from the same index,
so the cold read cost should not move, but that is not re-measured. It adds one
more payload extraction per candidate row: warm buffer hits go from 4,178 to
4,592, and warm time stays at 12.3 to 12.5 ms.

Three alternating warm runs of the earlier statement with this kind list and
with a two-kind variant gave 12.1 to 12.7 ms and 8.5 ms. The shipped list reads
602 candidate rows instead of 85 because it also returns the other kinds the
index holds for the same ids; the 4 ms difference is the price of an exact
predicate match.

Final plan, shipped statement, 1,180 ids, warm:

```text
Unique  (cost=573960.19..573960.20 rows=1 width=128) (actual time=12.238..12.253 rows=63.00 loops=1)
 Buffers: shared hit=4592
 CTE candidates
   ->  Index Scan using fact_records_vulnerability_active_advisory_lookup_v2_idx on fact_records fact  (cost=0.68..550960.87 rows=831233 width=258) (actual time=0.485..8.925 rows=602.00 loops=1)
         Index Cond: ((payload ->> 'advisory_id'::text) = ANY ('{<advisory ids elided>}'::text[]))
         Index Searches: 336
         Buffers: shared hit=3851
 ->  Sort  (cost=22999.31..22999.32 rows=1 width=128) (actual time=12.237..12.241 rows=80.00 loops=1)
       Sort Key: candidate.advisory_id, candidate.ecosystem, candidate.package_name, candidate.package_id
       Sort Method: quicksort  Memory: 32kB
       Buffers: shared hit=4592
       ->  Nested Loop  (cost=0.68..22999.30 rows=1 width=128) (actual time=0.556..12.158 rows=80.00 loops=1)
             Buffers: shared hit=4589
             ->  Nested Loop  (cost=0.28..22998.26 rows=1 width=491) (actual time=0.523..10.508 rows=80.00 loops=1)
                   Buffers: shared hit=4235
                   ->  CTE Scan on candidates candidate  (cost=0.00..22858.91 rows=74 width=192) (actual time=0.488..9.415 rows=85.00 loops=1)
                         Filter: ((ecosystem <> ''::text) AND (package_name <> ''::text) AND (fact_kind = 'vulnerability.affected_package'::text))
                         Rows Removed by Filter: 517
                         Storage: Memory  Maximum Storage: 458kB
                         Buffers: shared hit=3851
                   ->  Index Scan using ingestion_scopes_active_generation_idx on ingestion_scopes scope  (cost=0.28..1.87 rows=1 width=299) (actual time=0.012..0.012 rows=0.94 loops=85)
                         Index Cond: (active_generation_id = candidate.generation_id)
                         Filter: (candidate.scope_id = scope_id)
                         Index Searches: 85
                         Buffers: shared hit=384
             ->  Index Scan using scope_generations_active_scope_idx on scope_generations generation  (cost=0.40..1.03 rows=1 width=190) (actual time=0.019..0.019 rows=1.00 loops=80)
                   Index Cond: (scope_id = scope.scope_id)
                   Filter: (scope.active_generation_id = generation_id)
                   Index Searches: 80
                   Buffers: shared hit=320
Planning:
 Buffers: shared hit=3724
Planning Time: 22.800 ms
Execution Time: 12.334 ms
```

Final plan, shipped statement, one 500-id chunk, warm:

```text
Unique  (cost=528645.39..528645.40 rows=1 width=128) (actual time=9.786..9.799 rows=50.00 loops=1)
 Buffers: shared hit=3575
 CTE candidates
   ->  Index Scan using fact_records_vulnerability_active_advisory_lookup_v2_idx on fact_records fact  (cost=0.68..507463.44 rows=765494 width=258) (actual time=0.280..7.103 rows=474.00 loops=1)
         Index Cond: ((payload ->> 'advisory_id'::text) = ANY ('{<advisory ids elided>}'::text[]))
         Index Searches: 238
         Buffers: shared hit=2947
 ->  Sort  (cost=21181.95..21181.95 rows=1 width=128) (actual time=9.786..9.789 rows=65.00 loops=1)
       Sort Key: candidate.advisory_id, candidate.ecosystem, candidate.package_name, candidate.package_id
       Sort Method: quicksort  Memory: 30kB
       Buffers: shared hit=3575
       ->  Nested Loop  (cost=0.68..21181.94 rows=1 width=128) (actual time=0.354..9.725 rows=65.00 loops=1)
             Buffers: shared hit=3572
             ->  Nested Loop  (cost=0.28..21180.90 rows=1 width=491) (actual time=0.320..8.360 rows=65.00 loops=1)
                   Buffers: shared hit=3282
                   ->  CTE Scan on candidates candidate  (cost=0.00..21051.09 rows=68 width=192) (actual time=0.284..7.444 rows=70.00 loops=1)
                         Filter: ((ecosystem <> ''::text) AND (package_name <> ''::text) AND (fact_kind = 'vulnerability.affected_package'::text))
                         Rows Removed by Filter: 404
                         Storage: Memory  Maximum Storage: 365kB
                         Buffers: shared hit=2947
                   ->  Index Scan using ingestion_scopes_active_generation_idx on ingestion_scopes scope  (cost=0.28..1.90 rows=1 width=299) (actual time=0.012..0.012 rows=0.93 loops=70)
                         Index Cond: (active_generation_id = candidate.generation_id)
                         Filter: (candidate.scope_id = scope_id)
                         Index Searches: 70
                         Buffers: shared hit=335
             ->  Index Scan using scope_generations_active_scope_idx on scope_generations generation  (cost=0.40..1.03 rows=1 width=190) (actual time=0.020..0.020 rows=1.00 loops=65)
                   Index Cond: (scope_id = scope.scope_id)
                   Filter: (scope.active_generation_id = generation_id)
                   Index Searches: 65
                   Buffers: shared hit=260
Planning:
 Buffers: shared hit=3724
Planning Time: 18.758 ms
Execution Time: 9.883 ms
```

Result check on the same ids: the aggregate form of the shipped statement
returned 63 rows, 57 ids resolved and 0 blank `package_id` values, which agrees
with the replay shim's 57 ids with stored facts. The ids also give 63 distinct
`(ecosystem, package_name)` pairs, so no two packages in this set share a name
and differ only in `package_id`. The live fixture covers that case.

No-Regression Evidence: nothing in the runtime calls this method yet, so no
existing path changes. The new read is bounded by the id count (336 index
searches for 1,180 ids), not by the number of active scopes, and a burst of
changed ids runs as short statements of at most 500 ids.

## Observability Evidence:

Each call adds one `stored_advisory_names.read` span (registered in
`go/internal/telemetry`, listed in
[traces](../../public/reference/telemetry/traces.md)) with a `postgres.query`
child span per chunk for statement latency. Span attributes are counts only:
`stored_advisory_names.ids_requested`, `.chunks`, `.ids_resolved` and `.packages`.
An operator who sees a feed pass that re-queried too few or too many names can
compare `ids_requested` with `ids_resolved` on this span. A failed chunk marks
the span as an error and the call returns no partial result, because a partial
name set would hide a dropped name. No new metric: statement latency stays on the
existing Postgres query histogram.

## Tests

Every fixture row that models collected data goes through the real
`vulnerabilityintelligence.OSVRecordEnvelopes` and `FactStore.UpsertFacts`, so
its shape is the producer's. A small separate set of guard rows is written by
hand, because the collector never writes them; each one says which guard it
pins. The expected values are literals, so a producer change fails the test.

- `TestListStoredAdvisoryPackageNamesReturnsDroppedNameLive` (PostgreSQL 18.6):
  a record that dropped a name keeps the old name in the active generation, and
  the read returns it. Two Maven packages that share an artifact stay two rows.
  A Go package reads as `gomod` and maps to `go`. Maven, Swift and Hex rows carry
  the group or namespace in `PackageID`. A superseded generation, a tombstone,
  active status without the scope pointer, the scope pointer on a pending
  generation, another fact kind, a blank name, a missing ecosystem and an
  unrequested id return nothing. One package in two scopes is one row.
- `TestListStoredAdvisoryPackageNamesChunksTheIDListLive`: 1,009 ids across
  three chunks return what one statement would.
- Hermetic: statement sizes pinned to the literals 500, 500 and 4, blank and
  repeated ids dropped before the database, ties sorted by `PackageID`, no
  statement for an empty request, error returns no partial result, span counts,
  query shape, and the index-predicate drift guard.

Mutation checks, each killed. Statement mutants ran against the live tests only,
so a test that matches the query text cannot be what kills them:

| Mutant | Killed by |
| --- | --- |
| Drop the scope pointer condition | both live tests |
| Drop `generation.status = 'active'` | both live tests |
| Change the status check to `<> 'retired'` | both live tests |
| Drop `DISTINCT` | both live tests |
| Leave `PackageID` unset in the scanned row | both live tests |
| `DISTINCT ON` the name only, which collapses Maven groups | dropped-name live test |
| Drop the trim on `package_name` or `package_id` | dropped-name live test |
| Drop the ecosystem lowercasing | dropped-name live test |
| Drop the tombstone filter | dropped-name live test |
| Drop the fact-kind narrowing | dropped-name live test |
| Drop the blank-name or blank-ecosystem filter | dropped-name live test |
| Chunk size 501 | `TestListStoredAdvisoryPackageNamesChunksAndDedupes` |
| Return the partial map on error | `TestListStoredAdvisoryPackageNamesFailsClosedOnQueryError` |
| Ignore `PackageID` as the sort tie-break | `TestListStoredAdvisoryPackageNamesSortsTiesByPackageID` |
| Reverse the sort | dropped-name live test and the tie-break test |
| Drop one kind from the anchor list | `TestStoredAdvisoryNamesQueryKindListMatchesTheIndexPredicate` |
