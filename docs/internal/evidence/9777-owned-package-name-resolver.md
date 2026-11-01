# Owned-package name resolver: from OSV package name to owned dependency rows (#9777)

## What changed at runtime

`FactStore.ResolveOwnedPackageNames`
(`go/internal/storage/postgres/owned_package_resolver.go`) maps OSV package
names onto the owned dependency inventory: for each `(ecosystem, name)` it
returns every active stored `(entity_name, version)` the vulnerability
derivation would query for that name, with the owned-package reader's
`key_hash_low24` and the virtual slot (`key_hash_low24 mod 1024`). A row the
derivation skips (a non-exact version, an unexpanded variable, an sbt row with
no consumption key) is not a target. Nothing calls it yet. It is the first read of the change-driven re-query path ruled in
the #9777 ADR (D1), and no scheduled work, queue, migration or index changes
with it.

Row selection is not copied. Every SQL piece (the active-generation CTE body,
the `DISTINCT ON` identity and order, the `sha256(ecosystem|package_name|version)`
slot key, the low-24-bit expression, and the folded-name predicate of
migration 247) is sliced out of the shipped reader constants at a marker, and
`TestOwnedResolverPartsAreSlicesOfProduction` pins each slice. The reader's
`$1` package-manager bind set comes from the same `ownedPackageManagerFilterValues`
the reader uses, so Gradle and `gomod` aliases and sbt rows follow it.

The planner's rules are shared, not mirrored.
`schedule.ExactOwnedDependencyVersionForEcosystem` and
`packageidentity.SwiftOSVSourceLocation` moved out of
`coordinator/vulnerability`, and the planner and the resolver both call them.
`packageidentity.NormalizePyPIName` and `PyPINameSeparatorRun` are the one PEP
503 fold, and the SQL filter takes the same pattern constant.

An ecosystem outside `workflow.VulnerabilityOwnedDerivationEcosystems()` is an
error, because an empty answer would read as "nothing owned".

## Why the seek needs a fence

A first version joined the name list to the reader CTE body with
`CROSS JOIN LATERAL`. On the QA read replica the planner pulled the body up
into a hash join and read the whole `package_manager = 'npm'` prefix of the
folded index. The fix is `OFFSET 0` on the lateral body, so the planner keeps
it as a per-name subplan. The two plans below are the same 40 names, the same
statement text except for that one line.

## Measurement

Performance Evidence: read-only `EXPLAIN (ANALYZE, BUFFERS, COSTS OFF)` on the
QA PostgreSQL 18.6 read replica (`pg_is_in_recovery() = t`,
`PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=120000"`),
2026-11-01 about 19:15Z to 19:30Z, one window under one hour, aggregates only.
Inputs: 40 folded names, bind `{npm}` (npm), `{gradle,maven,sbt}` (Maven
family). The npm name list had 55 distinct `(ecosystem, name, version)` rows
across 610 fact rows. The Maven, binary-artifact and swift reads used the same
npm-style name list, so they return no rows; they show the access path and the
cost floor, not a hit workload.

| Statement | Without the fence | With the fence |
| --- | --- | --- |
| npm by name (40 names) | Hash Join, 241,106 index rows read, 5,788 ms, 380,689 buffers, 39,875 temp blocks written | nested loop, 40 index searches, 610 rows, 64.2 ms, 5,917 buffers, no temp |
| Maven family by name (40 names) | not measured | 120 index searches (3 managers x 40 names), 43 ms |
| sbt `%%` binary artifact | not applicable | `package_manager = 'sbt'` prefix scan, 4.5 ms, 2 sbt rows in QA |
| swift (all swift identities) | not applicable | `package_manager = 'swift'` prefix scan, 2.2 ms, no swift rows in QA |

All four rows predate the PyPI change. The PyPI read has its own section below.

The fenced npm plan (folded-index seek per name, `Index Searches: 40`):

```
                                                                                                                                                                                                                                                                                                                                                                  QUERY PLAN
 Subquery Scan on picked (actual time=64.040..64.136 rows=55.00 loops=1)
   Buffers: shared hit=5720 read=197
   I/O Timings: shared read=1.176
   ->  Unique (actual time=64.038..64.125 rows=55.00 loops=1)
         Buffers: shared hit=5720 read=197
         I/O Timings: shared read=1.176
         ->  Sort (actual time=64.037..64.060 rows=610.00 loops=1)
               Sort Key: (CASE lower(((fact.payload -> 'entity_metadata'::text) ->> 'package_manager'::text)) WHEN 'go-module'::text THEN 'go'::text W ...
               Sort Method: quicksort  Memory: 115kB
               Buffers: shared hit=5720 read=197
               I/O Timings: shared read=1.176
               ->  Nested Loop (actual time=11.301..62.464 rows=610.00 loops=1)
                     Buffers: shared hit=5709 read=197
                     I/O Timings: shared read=1.176
                     ->  HashAggregate (actual time=0.022..0.032 rows=40.00 loops=1)
                           Group Key: lookup.name
                           Batches: 1  Memory Usage: 32kB
                           ->  Function Scan on unnest lookup (actual time=0.009..0.010 rows=40.00 loops=1)
                     ->  Nested Loop (actual time=1.080..1.541 rows=15.25 loops=40)
                           Buffers: shared hit=5709 read=197
                           I/O Timings: shared read=1.176
                           InitPlan 1
                             ->  Index Only Scan using ingestion_scopes_active_generation_idx on ingestion_scopes (actual time=0.021..0.723 rows=1929.00 loops=1)
                                   Heap Fetches: 658
                                   Index Searches: 1
                                   Buffers: shared hit=573
                           ->  Nested Loop (actual time=1.076..1.374 rows=15.25 loops=40)
                                 Buffers: shared hit=3233 read=197
                                 I/O Timings: shared read=1.176
                                 ->  Index Scan using fact_records_active_package_dependency_folded_entity_idx on fact_records fact (actual time=1.049..1.307 rows=15.25 loops=40)
                                       Index Cond: ((((payload -> 'entity_metadata'::text) ->> 'package_manager'::text) = ANY ('{npm}'::text[])) AND ((lower((TRIM(BOTH FROM (payload ->> 'entity_name'::text)))::text))::text = lookup.name) AND (generation_id = ANY ((InitPlan 1).col1)))
                                       Filter: (NOT is_tombstone)
                                       Index Searches: 40
                                       Buffers: shared hit=830 read=197
                                       I/O Timings: shared read=1.176
                                 ->  Index Scan using ingestion_scopes_active_generation_idx on ingestion_scopes scope (actual time=0.002..0.002 rows=1.00 loops=610)
                                       Index Cond: (active_generation_id = fact.generation_id)
                                       Filter: (scope_id = fact.scope_id)
                                       Index Searches: 610
                                       Buffers: shared hit=1830
                           ->  Index Scan using scope_generations_active_scope_idx on scope_generations generation (actual time=0.009..0.009 rows=1.00 loops=610)
                                 Index Cond: (scope_id = scope.scope_id)
                                 Filter: (scope.active_generation_id = generation_id)
                                 Index Searches: 610
                                 Buffers: shared hit=2476
 Planning:
   Buffers: shared hit=3812
 Planning Time: 15.601 ms
 Execution Time: 64.239 ms
(49 rows)
```

The Maven-family by-name plan keeps the same shape with `Index Searches: 120`:

```
                                                                                                                                                                                                                                                                                                                                                                  QUERY PLAN
 Subquery Scan on picked (actual time=43.167..43.169 rows=0.00 loops=1)
   Buffers: shared hit=1070 read=2
   I/O Timings: shared read=0.029
   ->  Unique (actual time=43.167..43.168 rows=0.00 loops=1)
         Buffers: shared hit=1070 read=2
         I/O Timings: shared read=0.029
         ->  Sort (actual time=43.166..43.167 rows=0.00 loops=1)
               Sort Key: (CASE lower(((fact.payload -> 'entity_metadata'::text) ->> 'package_manager'::text)) WHEN 'go-module'::text THEN 'go'::text W ...
               Sort Method: quicksort  Memory: 25kB
               Buffers: shared hit=1070 read=2
               I/O Timings: shared read=0.029
               ->  Nested Loop (actual time=43.124..43.125 rows=0.00 loops=1)
                     Buffers: shared hit=1059 read=2
                     I/O Timings: shared read=0.029
                     ->  HashAggregate (actual time=0.021..0.033 rows=40.00 loops=1)
                           Group Key: lookup.name
                           Batches: 1  Memory Usage: 32kB
                           ->  Function Scan on unnest lookup (actual time=0.010..0.012 rows=40.00 loops=1)
                     ->  Nested Loop (actual time=1.077..1.077 rows=0.00 loops=40)
                           Buffers: shared hit=1059 read=2
                           I/O Timings: shared read=0.029
                           InitPlan 1
                             ->  Index Only Scan using ingestion_scopes_active_generation_idx on ingestion_scopes (actual time=0.141..0.871 rows=1929.00 loops=1)
                                   Heap Fetches: 658
                                   Index Searches: 1
                                   Buffers: shared hit=573
                           ->  Nested Loop (actual time=1.077..1.077 rows=0.00 loops=40)
                                 Buffers: shared hit=1059 read=2
                                 I/O Timings: shared read=0.029
                                 ->  Index Scan using fact_records_active_package_dependency_folded_entity_idx on fact_records fact (actual time=1.041..1.041 rows=0.00 loops=40)
                                       Index Cond: ((((payload -> 'entity_metadata'::text) ->> 'package_manager'::text) = ANY ('{gradle,maven,sbt}'::text[])) AND ((lower((TRIM(BOTH FROM (payload ->> 'entity_name'::text)))::text))::text = lookup.name) AND (generation_id = ANY ((InitPlan 1).col1)))
                                       Filter: (NOT is_tombstone)
                                       Index Searches: 120
                                       Buffers: shared hit=486 read=2
                                       I/O Timings: shared read=0.029
                                 ->  Index Scan using ingestion_scopes_active_generation_idx on ingestion_scopes scope (never executed)
                                       Index Cond: (active_generation_id = fact.generation_id)
                                       Filter: (scope_id = fact.scope_id)
                                       Index Searches: 0
                           ->  Index Scan using scope_generations_active_scope_idx on scope_generations generation (never executed)
                                 Index Cond: (scope_id = scope.scope_id)
                                 Filter: (scope.active_generation_id = generation_id)
                                 Index Searches: 0
 Planning:
   Buffers: shared hit=3812
 Planning Time: 18.122 ms
 Execution Time: 43.274 ms
(47 rows)
```

The sbt binary-artifact read has no folded index to use. It reads the
`package_manager = 'sbt'` prefix (`fact_records_active_package_dependency_entity_idx`);
its cost is linear in the sbt row count, which was 2 in this window. It runs per
name chunk (50 names).

## The PyPI read departs from the per-name seek

This is a departure from ruling D1 condition 2 (LATERAL seek per name), for PyPI
only, so the arbiter can read it here. Condition 3 asks for `[-_.]+`
normalization on both sides, and no seek can give that: the folded index stores
the name as the manifest wrote it. The first version enumerated single-separator
spellings, which missed every stored separator run (`foo__bar` from `foo-bar`,
no error, nothing in `Partial`) and capped at five separators.

The PyPI read now takes the whole `pypi` prefix of the folded index once per
call, filters on `regexp_replace(<folded name>, '[-_.]+', '-', 'g') = ANY($2)`,
and normalizes the lookup with the same pattern constant. Only matched rows
reach Go. The run gap and the cap are gone, so PyPI never reports `Partial`.

D1 condition 2 also chunked the name list. That rule is dropped for this read
too: the call makes one statement for the whole PyPI hit list, so `$2` is bounded
by the memory list below, not by 50. A chunked prefix read would walk the prefix
once per chunk and pay the full cost N times.

Status: accepted with conditions A to D by the arbiter ruling
`9777-pypi-prefix-read-ruling-20261101`. Its quote-ready sentence, verbatim:

> The arbiter accepted the PyPI-only prefix read as the D1 condition 2
> departure (ruling `9777-pypi-prefix-read-ruling-20261101.md`): condition 3's
> `[-_.]+` normalization cannot be served by the folded index without a
> migration this slice excludes, the per-name seek silently misses separator
> runs, and the read is 52 to 55 ms for a 50-name call over 5,184 PyPI rows on
> the QA replica (146 ms when all 612 names match); the normalized expression
> index becomes mandatory when the PyPI statement reaches 500 ms warm or the
> `pypi` prefix passes 60,000 active rows.

The conditions are: A, the index trigger in the Scale section; B, the caller
contract in the Scale section; C, the plan text below; D, an obligation on slice
2: the first QA readback of slice 2 reports the measured PyPI statement duration
and `rows_read`, so the 5x and 10x lines stop being projections at the first real
data point.

Read-only `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF)` on the same QA replica,
2026-11-01 about 21:20Z, three runs each, repeated reads. The statement is the
filter above over the reader's body. Each manager's rows go through the same
regular expression filter, so the larger managers are real measured points for
what PyPI would cost at that size.

| Prefix read | Prefix rows | Names | Matched fact rows | Execution |
| --- | --- | --- | --- | --- |
| maven | 796 | 50 | 116 | 11.7 to 12.1 ms |
| composer | 2,095 | 50 | 270 | 37.2 to 37.8 ms |
| pypi (today) | 5,184 | 50 | 436 | 52.4 to 54.8 ms |
| pypi (today) | 5,184 | 612 (all, folded) | 5,184 | 145.2 to 147.0 ms |
| npm | 240,609 | 50 | 999 | 3,858 to 3,867 ms |

The statement the code ships, dumped from `ownedResolverPyPIQuery` and run on the
same replica with a fresh 50 names, took 47.7 to 49.4 ms (175 matched rows,
5,009 filtered). The shipped swift statement took 1.4 ms over no rows.

The prefix plan holds at every size: an index scan on
`fact_records_active_package_dependency_folded_entity_idx`, the filter, and an
in-memory quicksort (977 kB at the 5,184-row match, no temp). The reviewer
independently saw 60.8 ms and 148.3 ms for the two PyPI lines.

### Plan of the shipped PyPI statement

This is the plan the arbiter asked for in condition C. It is
`EXPLAIN (ANALYZE, BUFFERS)` of the shipped `ownedResolverPyPIQuery` on the QA
read replica. The run used `PREPARE` and `EXECUTE` with the arrays bound, under
`plan_cache_mode = force_custom_plan`. That is the custom plan that pgx
`QueryExecModeCacheDescribe` gets. The session had `default_transaction_read_only
= on` and a statement timeout. The run sent aggregates and `EXPLAIN` only.

The statement ran five times. The block shows the fifth run, so it is warm:
47.2 ms execution. The filter kept 227 rows and removed 4,957, out of 5,184
active PyPI rows. The plan has one index scan of the `pypi` prefix of
`fact_records_active_package_dependency_folded_entity_idx`, with the
active-generation condition in the index. Buffers: 11,368 in total, 9,471 of them
on that index. The 50 names are redacted in the Filter line, and the Sort Key is
shortened.

```
Subquery Scan on picked  (cost=224.61..224.65 rows=1 width=196) (actual time=47.078..47.132 rows=125.00 loops=1)
  Buffers: shared hit=11368
  ->  Unique  (cost=224.61..224.62 rows=1 width=340) (actual time=47.076..47.111 rows=125.00 loops=1)
        Buffers: shared hit=11368
        InitPlan 1
          ->  Index Only Scan using ingestion_scopes_active_generation_idx on ingestion_scopes  (cost=0.28..216.68 rows=1966 width=80) (actual time=0.004..0.455 rows=1968.00 loops=1)
                Heap Fetches: 1305
                Index Searches: 1
                Buffers: shared hit=1202
        ->  Sort  (cost=7.92..7.93 rows=1 width=340) (actual time=47.076..47.085 rows=227.00 loops=1)
              Sort Key: (CASE lower(((fact.payload -> 'entity_metadata'::text) ->> 'package_manager'::text)) WHEN ... (ecosystem, package_name, version, ...)
              Sort Method: quicksort  Memory: 59kB
              Buffers: shared hit=11368
              ->  Nested Loop  (cost=1.36..7.91 rows=1 width=340) (actual time=1.904..46.790 rows=227.00 loops=1)
                    Join Filter: ((scope.scope_id = fact.scope_id) AND (scope.active_generation_id = fact.generation_id))
                    Buffers: shared hit=11368
                    ->  Nested Loop  (cost=1.08..5.58 rows=1 width=1160) (actual time=1.886..45.185 rows=227.00 loops=1)
                          Buffers: shared hit=10421
                          ->  Index Scan using fact_records_active_package_dependency_folded_entity_idx on fact_records fact  (cost=0.68..2.95 rows=1 width=968) (actual time=1.872..43.299 rows=227.00 loops=1)
                                Index Cond: ((((payload -> 'entity_metadata'::text) ->> 'package_manager'::text) = ANY ('{pypi}'::text[])) AND (generation_id = ANY ((InitPlan 1).col1)))
                                Filter: ((NOT is_tombstone) AND (regexp_replace((lower((TRIM(BOTH FROM (payload ->> 'entity_name'::text)))::text))::text, '[-_.]+'::text, '-'::text, 'g'::text) = ANY ('{50 names, redacted}'::text[])))
                                Rows Removed by Filter: 4957
                                Index Searches: 1
                                Buffers: shared hit=9471
                          ->  Index Scan using scope_generations_active_scope_idx on scope_generations generation  (cost=0.40..2.62 rows=1 width=192) (actual time=0.008..0.008 rows=1.00 loops=227)
                                Index Cond: (scope_id = fact.scope_id)
                                Filter: (fact.generation_id = generation_id)
                                Index Searches: 227
                                Buffers: shared hit=950
                    ->  Index Scan using ingestion_scopes_active_generation_idx on ingestion_scopes scope  (cost=0.28..2.21 rows=1 width=299) (actual time=0.001..0.001 rows=1.00 loops=227)
                          Index Cond: (active_generation_id = generation.generation_id)
                          Filter: (scope_id = generation.scope_id)
                          Index Searches: 227
                          Buffers: shared hit=687
Planning:
  Buffers: shared hit=122
Planning Time: 6.753 ms
Execution Time: 47.178 ms
```

## Scale

1x is today's QA environment: 5,184 active PyPI fact rows holding 621 raw names,
612 folded names (lower-cased) and 601 PEP 503 normalized names, in 2,044 identities.

| PyPI size | Rows | 50-name call | Every name matches |
| --- | --- | --- | --- |
| 1x (measured) | 5,184 | 53 ms | 146 ms |
| 5x (projected) | about 26,000 | 260 to 420 ms | about 730 ms |
| 10x (projected) | about 52,000 | 520 to 830 ms | about 1.5 s |

The per-row cost of a filtered row is 10 µs at PyPI's size and 16 µs at npm's;
the 5x and 10x ranges use those two. A row that matches costs about 20 µs more
for the joins, the sort and the hash ((146 - 53) ms / 4,748 rows). The 5x and 10x
rows are projections from those costs, not runs at those PyPI sizes. The npm line
is a measured point 46 times past 1x, so the straight line is not a guess.

The seek costs about 1.1 ms per index search, because of the active-generation
index condition (the plans above show 1.04 ms per loop; the reviewer measured
1.1 ms). Enumerating spellings of the 13,675 PyPI feed names takes 3.1 forms per
name on average and a median of 1, so a 50-name call is about 155 searches, or
about 182 ms (the reviewer measured 181.4 to 182.2 ms for 155 real forms on the
same replica), and it is still wrong on runs.

The seek would be faster above about 11,300 to 18,100 rows (2x to 3x) for a
50-name call, and much sooner for a one-name call (5 ms against 53 ms today). That
is the cost of the right answer and it is small: this is a background replan
call, not a request path. It stops being worth it at about 60,000 PyPI rows (12x),
where a 50-name call reaches 1 s. The fix at that point is a partial expression
index on the normalized name. `regexp_replace` is immutable, so Postgres can index
it, and the read becomes a seek again. That needs its own migration and proof;
nothing here adds an index.

Trigger (arbiter condition A): when the PyPI resolver statement (the
`postgres.query` child of `owned_package_targets.resolve`) reaches 500 ms warm at
p95 over a day on QA, or the `pypi` prefix passes 60,000 active rows, a partial
expression index lands as its own migration, with `EXPLAIN` proof, and the PyPI
branch returns to a seek. The index is on
`regexp_replace(<folded name>, '[-_.]+', '-', 'g')`. It is partial: it carries
the migration 247 predicate (`fact_kind = 'content_entity'`, `source_system =
'git'`, a `Variable` entity with `config_kind = 'dependency'`) plus
`package_manager = 'pypi'`. 500 ms is half the 1 s limit, so the
index exists before the call is slow. The first trigger is a duration and not a
row count because history costs time that active rows do not show: the prefix
walks 46,239 index entries for 5,184 active rows (8.9x), and superseded
generations raise the cost with no growth in active rows.

Caller contract (arbiter condition B): slice 2 calls
`ResolveOwnedPackageNames` once per pass with the whole PyPI hit list, never per
name. A one-name call still pays the full prefix walk (about 53 ms), so a per-name
loop turns one walk into N.

Two limits on those figures. The prefix holds more index entries than the 5,184
active rows it names: 46,239 entries across all generations under the same
predicate, about 8.9x, and the per-row cost includes walking them, so history
growth raises the cost with no growth in active rows. The figures are also warm:
first-touch reads of other prefixes ran 1.2x to 4x their repeats (composer 52 vs
26 ms, rubygems 37 vs 9 ms, gradle 11 vs 3.3 ms). Neither changes the decision for
a background call. A cold read at 5x or 10x is not measured.

The swift read has no name filter, because the key is normalized in Go, and QA has
no swift rows to time. The same prefix read with no name filter over rows that do
exist costs 27 µs per row (pypi, 140 ms for 5,184) to 48 µs per row (composer,
100 ms for 2,095), a proxy for swift. 1,000 swift rows would cost about 30 to
50 ms, and 10,000 about 300 to 500 ms. It runs once per call and only when a swift
name changed.

Memory:

- A chunked seek holds at most 50 names per statement.
- The PyPI read has no chunk. `$2` holds the normalized names of the call, and Go
  receives only matched distinct identities, so the worst case is every PyPI
  identity (2,044 today, about 20,000 at 10x).
- The swift read returns every swift identity to Go, so its bound is the swift
  identity count.
- The sort in Postgres used 977 kB for 5,184 rows, about 190 bytes per row. Go
  holds one small struct per row and the result slice, so 20,000 rows stays in
  the low megabytes.

Concurrency: the resolver is a read-only `SELECT` on active-generation facts.
It takes no lock, writes nothing, and is idempotent. It runs with the same
pgx exec mode (`QueryExecModeCacheDescribe`) as the manifest-dependency read
so a cached generic plan cannot replace the per-name seek (#9586).

Under the pool default the PyPI read turns generic at its sixth execution. It
then runs 2.5x to 21x slower. Round-3 reads on the QA replica, shipped PyPI
statement:

| PyPI read | Custom plan | Generic plan | Ratio |
| --- | --- | --- | --- |
| 50 misses | 42.0 to 42.4 ms | 327 ms | 7.8x |
| All names (5,184 rows matched) | 144.1 to 145.4 ms | 354 to 392 ms | 2.5x |
| 14,000 misses | 44.5 to 44.9 ms (plus 25 ms planning) | 960 ms | 21x |

The generic plan also leaves the folded index for
`fact_records_content_entity_dependency_variable_repo_idx`. The answer stays the
same. The cost does not. `TestResolveOwnedPackageNamesPinsCacheDescribeOnEveryStatement`
holds the mode.

## Accuracy proof

`TestResolveOwnedPackageNamesDifferentialLive` (PostgreSQL 18.6) seeds npm
scopes, PyPI separator, run and case variants, Maven and Gradle coordinates, sbt
`%` and `%%` rows, Go case variants under `go` and `gomod`, NuGet, swift rows
written by the production `Package.resolved` parser for each location form the
planner accepts, an unexpanded `${var}` name,
non-exact versions, a tombstone, a superseded generation and a scope with no
active generation. It runs the production reader, then the production planner
(`IntelligenceWorkPlanner`) with slot scopes on, and for every query the planner
pinned to a virtual slot it resolves that query's name both as planned and as the
feed spells it. It requires a target with the planner's virtual slot and the
reader's `key_hash_low24`, requires that no returned identity is missing from the
reader, and requires one batch call to equal the union of the single calls,
including `Partial`. The union is deduplicated, because the single calls repeat a
lookup per version, but the batch is not: a repeated target fails.

The swift rows are not hand-written. `resolverParsedSwiftRows` writes pins into a
`Package.resolved` file and seeds what `parser/json.Parse` returns, and a pin the
parser drops fails the test. Two earlier versions got this wrong. The first
compared the planner's URL with the feed name and silently missed all 30 host/path
names. The second seeded `swift-crypto` as an entity name, which the parser never
writes (it writes `github.com/apple/swift-crypto`), so bare names passed a test
and matched nothing in production.

Swift feed spellings are literals from the OSV SwiftURL export. The 2026-11-01
export has 64 records and 33 distinct SwiftURL names: 30 host/path and 3 bare.
`TestResolveOwnedPackageNamesFindsEverySwiftURLNameLive` looks up all 33 against
parser-written rows.

`TestResolveOwnedPackageNamesMatchesTheFeedLive` covers PyPI runs and long mixed
names from either side, the swift forms, and bare names.
`TestResolveOwnedPackageNamesFollowsThePlannerRulesLive` looks up every row the
derivation skips and asserts no targets, and asserts an error for `crates.io`,
`Packagist`, `SwiftURL`, `sbt` and an unknown name.
`TestResolveOwnedPackageNamesChunksLongBatchesLive` runs a batch of over 200
forms with a by-name hit in the first chunk and an sbt `%%` hit in the last.
`TestSwiftRepositoryKeyJoinsFeedAndStoredSpellings` is the parity table for the
shared key.

Twenty-nine mutants each fail these tests, run on PostgreSQL 18.6 in a private
worktree. They cover the reader slices and slot hash, the shared swift rule (a
`git@` query kept, `ssh` dropped) and key (`.git`, port, fold), the shared version
rule, the PyPI filter on either side, the owned-set check, `Partial`, the sbt and
`${}` gates, and the chunk loop (first chunk only, binary read skipped after a
hit). A further 19 ran after the swift
fixture moved to parser-written rows, and each fails too: the duplicate and
`.partial` gaps the last review left alive, the bare-name match, the `.GIT` fold,
a package name in either error, and the swift rule and key again. One mapping has no such proof: treating an sbt row as `maven` for the
version rule changes nothing today, because the rule only branches on `go` and
`pypi`.

Known limit: three SwiftURL feed names are bare (`CocoaMQTT`, `swift-crypto`,
`swift-nio-http2`), so no repository key exists for them. Each appears in one
advisory record, 3 of the export's 78 SwiftURL affected entries. The resolver does
both things the review allowed. It matches the final path segment of each row's
repository key, so the likely owner is found, and it reports the lookup in
`Partial`, because a package can live in a repository of another name. The match
over-matches repositories that share a final path segment (a fork of
`swift-crypto` matches too, and the live test pins that), which costs a replan.
The caller must treat a partial lookup as "possibly more". Nothing else reports
partial today. The QA environment has no active swift rows, so no stored row has
exercised this outside the parser-written fixtures.

Observability Evidence: each call adds one `owned_package_targets.resolve`
span (`contract.SpanOwnedPackageResolve`, registered in `SpanNames`) with
`owned_package_resolver.lookups`, `.ecosystems`, `.folded_names`, `.statements`,
`.rows_read`, `.targets` and `.partial`, plus one `postgres.query` child span
per statement. It carries counts only and never a package name, including on the
error it records for an ecosystem outside the owned set.
`TestResolveOwnedPackageNamesSpanLive` pins the attributes, a non-zero `.partial`
and the error event. An operator who
sees a slow replan pass reads `statements` and `rows_read` to tell a wide batch
from a bad plan, and `partial` to see bare swift names. A PyPI or swift call
shows one statement and a `rows_read` of matched rows or swift identities.
