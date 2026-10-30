# #9586 Manifest Dependency Read: A Custom Plan On Every Page

`FactStore.ListActivePackageManifestDependencyFacts` pages the active Git
manifest dependency rows for one package-registry reducer intent, 500 rows
per keyset page. On a pooled reducer connection, every page from the sixth
execution of each query text on, including the whole of every later read,
ran on PostgreSQL's generic plan. That cost 1 to 3 s a page, even for a read
that returned no rows.

## Cause

The reducer pool runs pgx v5.9.2 through `database/sql` with nothing setting
`default_query_exec_mode`, so each SQL text runs as a named prepared statement
cached per connection (`QueryExecModeCacheStatement`). PostgreSQL's
`plan_cache_mode = auto` uses five custom plans, then switches to the generic
plan when its estimated cost is not much higher. Here the estimates are almost
equal, so it always switches.

The generic plan has the same shape and the same buffers as the custom one.
What changes is the Filter: `entity_name = ANY($2)` compares a Param array
linearly against every scanned row. A custom plan sees a Const array, and
PostgreSQL hashes a ScalarArrayOp only for a non-null Const array of at least
nine elements (`convert_saop_to_hashed_saop_walker`,
`src/backend/optimizer/util/clauses.c`, `MIN_ARRAY_SIZE_FOR_HASHED_SAOP`). The
sbt branch's `entity_name <> ALL($2)` has the same trap. The shim
(`/private/tmp/eshu-9586-shim.md`, PREPARE/EXECUTE with `plan_cache_mode`
forced each way) showed equal buffers and a generic plan 3 to 18 times slower
on the same page.

## Change

Both query texts now run every page with `pgx.QueryExecModeCacheDescribe`,
passed as the first query argument (`manifestDependencyQueryExecMode` in
`go/internal/storage/postgres/manifest_dependency_read_plan.go`).
pgx sends Parse, Bind and Execute on the unnamed statement, which PostgreSQL
plans with the bound values every time, so the array is a Const and hashed.
Parameter and result formats match the pool default, so decoding is unchanged. The cost
is one replan per page. Nothing else on the pool changes: the mode is a
per-call argument, not a connection setting.

Rejected: `plan_cache_mode = force_custom_plan` on the connection replans
every prepared statement on the writer pool (claims, leases, intent writes),
unmeasured. `IN (SELECT unnest($2))` flips the plan and was slower on the
common class in the shim. Inlining names into the SQL text is an injection
surface and churns the statement cache.

The rule for other readers: a large `= ANY($n)` or `<> ALL($n)` array that is
a residual Filter over many rows, run through the pooled pgx statement cache,
needs `QueryExecModeCacheDescribe` (or `QueryExecModeExec`) on that call.
Prove the trap with EXPLAIN under forced custom and generic plans first.

## Proof

Private PostgreSQL 18.6 (`initdb --no-locale`, `shared_buffers=1GB`,
`fsync=off`), the 273 origin/main migrations, then the #9568 `seed.sql` and the
reviewer's `r68-seed-extra.sql`: 965,101 `fact_records`, 794 MB, the shim's
fixture. The harness calls the production
`FactStore.ListActivePackageManifestDependencyFacts` over `SQLDB` and pgx
stdlib with one pooled connection. Mode `base` strips the leading exec-mode
argument, which is the origin/main call; mode `fix` passes the production
argument through. Names: S1 is nuget+pypi with 400 names, S2 npm with 400, S5
nuget with 400 names that match nothing, S6 the Maven family with 600.

Performance Evidence: whole paged read wall time in ms, one round per line,
host load1 and a CPU-bound canary recorded before and after each round.

| Round (order, load1 before, canary ms before/after) | Read | Pages | base ms | fix ms |
|---|---|---|---|---|
| 1 (base first, 20.47, 404.7/383.9) | S1 | 3 | 366.4 | 376.1 |
| 1 | S2 | 15 | 16,777.8 | 2,109.3 |
| 1 | S5 (0 rows) | 1 | 4,823.1 | 113.7 |
| 1 | S6 | 20 | 13,930.7 | 2,248.7 |
| Long read (fix first, 34.42, 388.2/361.3) | S3 | 277 | 324,350.5 | 142,939.7 |

S3 is the popular-name class: npm with 50 hot names and 350 others, 138,237
rows, on a fresh connection per mode. Under `base` its statement ran 5 custom
and 272 generic plans. The `fix` time already includes a fresh plan for each
of its 277 pages; EXPLAIN on this fixture puts planning at 42 to 88 ms a page
under load.

S1 is fast in both modes because its three pages are the first executions on
the connection, before the switch.

This is one round on a loaded shared host, so the absolute times are noisy.
Rounds 2 and 3, with the first mover alternating, waited for load1 below 20
and gave up at 23:48:11 EDT with load1 at 42.32. The ratio rests on the
shim's equal plans and buffers and on both modes running the same reads on
one connection.

Accuracy: in every round and mode, each read returned the same sorted
fact-id set and the same ordered full envelope (every field plus the payload
JSON, in returned order), with no duplicate ids. Round 1 hashes (sha256
prefixes): S1 id set `e0ba41d2fc4e`, ordered envelope `016ea3b9ae0b`; S2
`6b947ab63994` and `2935a10a9e53`; S6 `76f563be8a58` and `483e13e3d3d3`; S3
`6002d91593cd` and `374eb22fcfd3`; S5 empty. The id sets match the shim's.
Both modes bind from the same statement description and formats, so payload jsonb,
`observed_at` and the nullable source columns decode exactly as before.

The generic-plan switch is gone. After the four reads on the one connection,
`pg_prepared_statements` held two named manifest statements under `base`, with
14 and 15 generic plans against 5 custom each. Under `fix` it held none (the
one remaining match is the probe query's own text).

Regression test: `TestListActivePackageManifestDependencyFactsPlansEveryPageCustom`
fails on origin/main (no exec mode on any page) and passes with the change, for
the plain query over two pages and the Maven-family query.

Observability Evidence: each completed read records
`eshu_dp_package_manifest_dependency_read_seconds` and
`eshu_dp_package_manifest_dependency_read_pages`, labeled `read` (`plain` or
`maven_family`), proved by
`TestListActivePackageManifestDependencyFactsRecordsPagesAndWallTime`. A wall
time that climbs while pages hold means each page got slower, the shape this
issue had.

## Not proven here

- QA scale and QA plan choice; the fixture is a synthetic stand-in.
- The next cost: even a custom plan rescans every dependency row per page.
  EXPLAIN (npm, 400 names, page 1) shows a bitmap scan of
  `fact_records_content_entity_dependency_variable_repo_idx` estimated at 24
  rows that returns 565,101, with 554,409 removed by the Filter (77,234
  buffers). With bitmap scans off it is the same index as a plain index scan
  (619,154 buffers). With that index dropped inside a rolled-back
  transaction, the planner takes a scope-driven nested loop (793,401
  buffers) and still never uses
  `fact_records_active_package_dependency_entity_idx`. Why it skips the
  per-name index is open. That is its own shim.
