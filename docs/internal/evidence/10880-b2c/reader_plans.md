# #10880 PR-B2c reader plans: security-alert reconciliation and service-catalog vulnerability loaders

Refs #11198, #11199, #11200, #11201, #10880. Decided by the arbiter rulings of the
epic's reader PR split (decision 5: the reducer readers move off finding facts) and of
B2c itself (the non-counting deferral, the package and repository btrees, the fenced
page contract and the per-repository drain). The ruling files are working papers and
are not tracked, so the decisions are quoted where they bind.

Two reducer loaders read supply-chain impact finding facts as input. They now
read `supply_chain_impact_finding_supports` (migration 338), whose payload is the
fact payload verbatim. This note records the read shape, what it costs at 600,000
and 8,000,000 supports, and what the shape needs from the schema.

## The read shape

Both reads page the supports table with the statement of the hot-key ruling
(`supplyChainImpactSupportsPageQuery`, one builder, two texts: first page and next
page). Each filter arm is a per-key ordered `LIMIT 500` index scan:
`unnest($n) AS k(v) CROSS JOIN LATERAL (SELECT s.fact_id FROM supports s WHERE
s.col = k.v [AND s.fact_id > $cursor] ORDER BY s.fact_id LIMIT 500)`. Nothing about
the scope tables sits inside the `LIMIT`, so it fences the planner: the page
stops after 500 index entries per key and cannot flip to one probe per active
scope. The arms are `UNION ALL`, cut to the first 2,000 ids, made distinct and cut
to 500: the raw page. The outer query joins the raw ids to the supports by primary
key, `LEFT JOIN`s the two scope tables on their keys, and returns every raw row
with `is_active` and the payload only when active.

The reader appends the active rows, advances its cursor on the last raw id and
stops only when the raw page holds fewer than 500 rows. Each page is its own
autocommit statement. The security-alert loader drains its alert and consumption
facts on their own cursor and then the supports, so order equality is per kind.
The package, CVE and advisory arms repeat the literal `<> ''` of their partial
btrees; the package one comes from the same constant as migration 345's predicate.

The statement it replaced, one `OR` over the four arms in a `MATERIALIZED` CTE,
and the earlier measurements below are kept as the record of why: an `OR` is only
indexable when every arm is, and on a hot key the CTE materializes every match.

## Measurements at 8,000,000 supports (the superseded OR statement)

Disposable PostgreSQL 18.6 (`postgres:18.6`), shared_buffers 2 GB, work_mem 64 MB,
two parallel workers per gather, host load average 14 to 34 from other lanes.
Corpus (synthetic): 8,000,000 supports, heap 5,207 MB, 9,000 active scopes with
one active generation each, 400,000 repositories, 60,000 packages, 40,000 CVEs,
50,000 advisories, the migration-338 primary key and `(scope_id, generation_id)`,
`repository_id`, `finding_id` and `generation_id` btrees, plus partial btrees
`(cve_id, fact_id) WHERE cve_id <> ''` (337 MB) and
`(advisory_id, fact_id) WHERE advisory_id <> ''` (304 MB). The candidate index is
`(package_id, fact_id) WHERE package_id <> ''`: 451 MB, built `CONCURRENTLY` in
115 s. `LIMIT 500`; each shape is the supports arm of the loader statement with
its other arms bound to empty arrays.

Raw `EXPLAIN (ANALYZE, BUFFERS)` output: the rerun is
[reader_plans_8m_raw_rerun.txt](reader_plans_8m_raw_rerun.txt) (every row below;
block headers carry load and memory), and the first run, on a container that was
later lost, is [reader_plans_8m_raw.txt](reader_plans_8m_raw.txt). "First run" is
the first execution of the shape after a container restart: `shared_buffers` is
empty, the OS page cache was not dropped. "Second run" repeats it at once.

| Shape | With the package index, first / second | Without it, first / second |
| --- | --- | --- |
| all four arms (3 repos, 2 packages, 3 CVEs, 2 advisories; 1,022 rows) | 583.6 ms / 5.1 ms | 52,186 ms / 828.6 ms |
| repository only (60 rows) | 1.47 ms / 1.22 ms | 769 ms / 729 ms |
| package only (268 rows) | 2.44 ms / 1.94 ms | 749 ms / 693 ms |
| CVE only (534 rows) | 3.50 ms / 2.88 ms | 753 ms / 705 ms |
| advisory only (160 rows) | 1.95 ms / 1.55 ms | 719 ms / 703 ms |
| prepared, `force_generic_plan`, bound parameters, all four | 5.61 ms | 968 ms |
| prepared, generic: repository / package / CVE / advisory only | 1.44 / 2.42 / 3.91 / 2.26 ms | not run |

Plans: with the index every shape is a `BitmapOr` over the four index scans
(`s_repo`, `s_pkg`, `s_cve`, `s_adv`); unused arms are bound to empty arrays and
cost 0 ms (`Index Searches: 0`), also under the generic plan. The all-arms read
touches 3,869 buffers on its first run and 3,866 on the second. Without the index
every shape is a parallel sequential scan of the 5.2 GB heap, including the
repository-only and CVE-only shapes that have their own btrees.

The earlier run on the lost container, same corpus shape, was colder: the
sequential scan took 93 to 111 s for the repository, package and CVE shapes and
4.8 s for the advisory shape, the join form 159 s with 7.25 million buffer hits,
and the package index brought the package-only read to 86 ms and the all-arms
read to 172 ms.

What the numbers say:

- The filter is an `OR`. An `OR` is only indexable when every arm is. Without a
  `package_id` index the planner scans the whole table for every shape.
- The sequential scan is fast (0.7 to 0.97 s) only while the 5.2 GB heap sits in
  the OS cache, and 52 to 111 s when it does not. A reducer worker does not keep
  5 GB hot.
- With the partial `package_id` btree the read is 1.4 to 5.6 ms warm and 584 ms on
  the first run after a restart (858 buffers read), bounded by the index.

The same shapes at 600,000 supports (6,000 scopes, 419 MB with indexes): join form
313 to 389 ms; raw-first 0.1 to 1.1 ms for the repository arm, 3.9 ms for all
four arms with three new btrees, 51 ms for all four arms with none.

**The reconciliation read at QA scale depends on the partial
`(package_id, fact_id) WHERE package_id <> ''` btree on the supports table.**
Not measured here yet: the hot-key page cost and the write cost (WAL) of the
extra index.

## Hot key: a package with 1,600,000 supports

Raw output: [hot_key_raw_noindex.txt](hot_key_raw_noindex.txt),
[hot_key_raw_index.txt](hot_key_raw_index.txt) and
[hot_key_raw_variants.txt](hot_key_raw_variants.txt) for the superseded OR
statement and the variants tried, and
[hot_key_ruled_statement_raw.txt](hot_key_ruled_statement_raw.txt),
[hot_key_bounds_raw.txt](hot_key_bounds_raw.txt),
[plan_guard_hot_corpus_raw.txt](plan_guard_hot_corpus_raw.txt) and
[hot_key_drain_binary_raw.txt](hot_key_drain_binary_raw.txt) for the ruled
statement. Corpus: 3,005,000 facts, of which the hot package has 1,600,000 impact
findings, 150,000 consumption correlations and 5,000 alerts; 2,600,000 supports,
1,600,000 for the hot package; 9,000 active scopes; the full bootstrap schema
(fact_records 6.4 GB, supports 3.0 GB). Bound parameters, `LIMIT 500`.

The OR statement (merged with the fact arm) against the legacy fact read of
`bccdb88982`, with `s_pkg`, `s_cve` and `s_adv` present, after a restart:

| Page | Legacy fact read, first / second | Merged statement, first / second | Buffers, legacy / merged | Temp blocks written |
| --- | --- | --- | --- | --- |
| page 1 | 62,186 / 20,696 ms | 20,142 / 6,349 ms | 3.03M / 5.52M | 0 / 120,463 |
| midpoint | 11,489 / 2,584 ms | 3,526 / 3,523 ms | 3.03M / 3.13M | 0 / 60,238 |

Legacy wall time swung 10x between two passes on identical buffer counts. With a
package that covers 61% of the table the planner scanned the supports
sequentially and materialized the whole match set in the CTE, which spilled to
temp. `NOT MATERIALIZED` and per-arm lateral variants with the active check inside
avoided the temp but drove from the 9,000 active scopes (2.4M to 2.6M buffers). An
ordered scan of `s_pkg` with `LIMIT 500` and no active-generation predicate took
2.9 ms and 510 buffers; any formulation that states the active check makes the
planner leave it. That is why the ruled statement keeps the active check outside
the fence.

The ruled statement, single hot key (`pkg:hot`, 1,600,000 supports), warm second
run, both plan cache modes (`force_generic_plan`, `force_custom_plan`), temp 0,
no sequential scan:

| Page | Buffers | Time |
| --- | --- | --- |
| page 1 | 5,958 | 3.8 to 3.9 ms |
| midpoint (cursor at the 800,000th id) | 5,944 | 3.9 to 4.2 ms |
| last page (250 rows left) | 2,959 | 1.9 to 2.2 ms |
| 200-key filter, three hot keys (1.6M, 188k, 188k), page 1 | 9,130 | 7.3 to 7.7 ms |
| 200-key filter, three hot keys, next page | 9,128 | 7.9 to 8.1 ms |

The first execution after a container start is cold: 763 ms for page 1, 407 ms for
the midpoint. The bounds of the ruling (at most 6,000 buffers, temp 0, at most 50
ms warm, midpoint at most 1.5x page 1) hold for the single hot key, with page 1 at
5,958 of 6,000. The plan guard (`TestSupplyChainImpactSupportsPagePlanGuardLive`)
ran on this corpus at its first form ([plan_guard_hot_corpus_raw.txt](plan_guard_hot_corpus_raw.txt)):
green for both modes and both page texts, and the package-index and cursor-as-OR
violations fail it. On this corpus the planner keeps the ordered scans even with the
active join inside the lateral, so that violation fails the statement-text test, not
the plan guard; on the 400,000-row seed with 300 scopes it fails the plan guard too.

The guard now covers the repository arm and the service-catalog statement, reads the
Index Cond of the node that scans each arm's own index and requires the arm's key (and,
on a next page, the cursor) in it. Its current run on the fresh seed
([plan_guard_fresh_seed_raw.txt](plan_guard_fresh_seed_raw.txt)): 8 greens (the
reconciliation and the service-catalog statement, both plan cache modes, first and next
page) and six seeded violations that each fail it (package or repository index dropped,
package or repository guard literal removed, active join inside the lateral, cursor
written as an `OR`). On the rebased tree, with PR-B2b's migrations 339 and 340 present,
the guard proves the CVE and advisory arms on those migrations' own indexes
(`supply_chain_impact_finding_supports_cve_idx` and `..._advisory_idx`, named in the
run's `-v` output) together with the package and repository btrees of migrations 345
and 346: all 8 greens and all six violations hold, 31.3 s and 32.2 s in two runs
([plan_guard_post_rebase_raw.txt](plan_guard_post_rebase_raw.txt)). The test no longer
builds stand-in indexes: it fails when migration 339 or 340 stops building one. The
hot-corpus run of the current guard was not repeated; the committed hot-corpus run
predates the repository arm.

Drain of the hot key on a built binary (`ListActiveSecurityAlertReconciliationFacts`,
package filter only): 3,201 supports pages, 1,600,000 raw rows, all active.
Cold first run: supports 468.5 s, whole loader 554.9 s, peak RSS 3.8 GB. Second run
in a fresh process: supports 186.3 s, loader 269.1 s. With the real
`SecurityAlertReconciliationHandler` on top (a discarding writer, scope-2, 5,009
alerts evaluated): handler 723.5 s, supports drain 416.4 s inside it, peak RSS
5.26 GB. A page is 3.9 ms in the database; the remaining 58 to 146 ms per page is
the client reading 500 payloads of about 540 bytes and decoding them, plus cold
buffers. The legacy drain of the same key was estimated, not run, at 2.5 hours or
more (1.06e10 buffer touches).

The same statement on the 8,000,000-support corpus, five selective shapes
([selective_8m_ruled_raw.txt](selective_8m_ruled_raw.txt)), first page, both plan
cache modes, `s_pkg` built in 75 s:

| Shape | Buffers | Warm | First execution after a restart |
| --- | --- | --- | --- |
| repository only (3 keys) | 494 | 2.5 to 2.6 ms | 260 ms |
| package only (2 keys) | 1,631 | 4.0 to 4.1 ms | 835 ms |
| CVE only (3 keys) | 2,870 | 5.5 to 5.6 ms | 669 ms |
| advisory only (2 keys) | 1,011 | 3.1 to 3.3 ms | 387 ms |
| all four arms | 3,191 | 6.3 to 6.4 ms | 6.3 ms (cached by the other shapes) |

All shapes are at or under 3,191 buffers and 6.4 ms warm, against the ruling's
bounds of 7,738 buffers and 15 ms. The superseded OR statement read 3,869 buffers
and took 5.1 ms warm for the all-arms shape.

## The repository arm on a repository with 160,000 supports

QA holds repositories of an estimated 120,000 to 150,000 findings, above the
ruling's 20,000 line for the repository arm, so it was measured
([repository_arm_raw.txt](repository_arm_raw.txt)) on `b2c_hot` with `repo:hotrepo`
seeded to 160,000 supports, repository array only, both plan cache modes:

| Index | Page | Buffers | Time warm | First after restart |
| --- | --- | --- | --- | --- |
| `(repository_id)` only (migration 338) | page 1 | 108,862 | 103 to 115 ms | 797 ms |
| | midpoint | 108,862 | 104 to 117 ms | 108 ms |
| | last page (250 left) | 106,101 | 90 to 96 ms | 97 ms |
| `(repository_id, fact_id) WHERE repository_id <> ''` | page 1 | 5,909 | 3.7 ms | 374 ms |
| | midpoint | 5,897 | 3.7 to 3.8 ms | 153 ms |
| | last page (250 left) | 2,958 | 1.8 to 2.0 ms | 99 ms |

With only the existing index each page reads every one of the repository's 160,000
supports and keeps the first 500 by `fact_id` (a top-N sort, no temp): 108,862
buffers per page, 320 pages to drain the repository, about 35 million buffers.
With the candidate the page is an ordered Index Only Scan and costs what a hot
package costs. Index size and build time: 261 MB and 33.4 s at 2,600,000 supports
(the `(repository_id)` btree is 42 MB); 433 MB and 82.8 s at 8,000,000 (63 MB);
151 MB and 10.3 s at 1,500,000 (11 MB). The candidate is about 6 times the size of
the btree it would serve, so it is a candidate to replace `(repository_id)` rather
than sit beside it; that, and its WAL, are not measured here (SLOT 3).

## Write cost of the new indexes (WAL)

Harness, settings and every figure: [supports_index_wal_raw.txt](supports_index_wal_raw.txt)
and [supports_index_wal_points.txt](supports_index_wal_points.txt). The impact writer's
real transaction, 3,000 findings per pass, shapes S1 (no group members) and S2 (six
group members), cold (CHECKPOINT before every pass) and warm, three interleaved
reps per cell, PostgreSQL 18.6 with `wal_compression=off`, `full_page_writes=on`,
`checkpoint_timeout=5min`, `max_wal_size=6GB`, `shared_buffers=2GB`,
`wal_level=replica`. Percentage points over the B1-shipped median, S2 cold (the
shape and mode of the ruling's stop values):

| Variant | Insert | Replay |
| --- | --- | --- |
| `+cve` (B2b) | +2.2 | +3.0 |
| `+advisory` (B2b, on `+cve`) | +5.1 | +5.4 |
| `+members` (B2b) | +15.2 | +11.4 |
| `+tokens` (B2b) | +27.1 | +5.4 (min -0.4, max +18.2: noisy) |
| `+package` (migration 345, added last) | +29.4 | +21.3 |
| `pkg-only` (345 alone on B1-shipped) | +2.3 | +3.1 |
| `repofact-only` | +3.1 | +2.5 |
| `package+repofact` | +5.3 | +5.6 |
| `swap-repobtree-for-repofact` | +2.1 | +1.4 |
| ten-index total (`+package`, plus repofact) | +32.4 | +23.8 |

The two GIN indexes (`+members`, `+tokens`) in the harness were created with default
reloptions (`fastupdate` on, the default `gin_pending_list_limit`), and the pending
list was flushed with `gin_clean_pending_list` after each pass; PR-B2b ships them with
`fastupdate = off`, so their rows here are not B2b's final figures. The harness has no
arm for migrations 343 and 344.

Reading: migration 345 alone costs +2.3 points on S2 cold insert and +3.1 on replay,
above the ruling's 2-point line on both; as the last of the cumulative chain it adds
2.3 points to insert. The nine-index total (`+package` row) is +29.4 insert and +21.3
replay against the +30 and +36 stops. Adding the `(repository_id, fact_id)` btree as a
tenth index takes the insert total to +32.4, past +30; replacing the `(repository_id)`
btree with it costs +2.1 on insert and +1.4 on replay. Replay on the cumulative
variants is noisy (spreads up to 28% inside one cell, from full-page images), so the
isolated variants are the cleaner figures; S1 shows the same ordering at about half
the size. Retract is not part of the stops and is in the raw file.

The worst replay cell of the set is S2 warm: the `+package` chain cell is +27.7 over
the B1-shipped median (max +64.8) and the ten-index total +30.2 (max +71.2), with
spreads of 26 to 28% inside the cell; the cold replay cells are the readable ones.
Chain plus the repository swap was not measured. By addition (the `+package` chain
cell plus the 346 swap, S2 cold insert) it is 36,391,136 bytes, +31.5 points over
B1-shipped: a DERIVED figure, not a measurement. The binding totals come from the
W-A run owned by the B2b lane, not from this harness.

On the index migration 346 builds (`CREATE INDEX CONCURRENTLY` of the migration
file on `b2c_hot`, 261 MB, 106 s on a loaded host), the shipped statements of both
loaders on the 160,000-support repository, both plan cache modes
([repository_arm_bounds_raw.txt](repository_arm_bounds_raw.txt)): page 1 5,909,
midpoint 5,897, last page 2,958 buffers after a vacuum (midpoint 1.0 times page 1);
5,978, 5,965 and 2,989 with every twentieth row of the repository rewritten and
nothing vacuumed. Temp 0, no sequential scan and at most 50 ms warm are asserted by
the test. On a fresh single-scope database the same pages read 2,017, 2,016 and
1,009 buffers (2,296, 2,282 and 1,143 rewritten): with 9,000 scopes the per-row
scope probes no longer memoize, which is the figure to quote.

Where these two proofs run. The raw files above name the tests as
`TestSupplyChainImpactSupportsRepositoryArmBoundsLive` and
`TestSupplyChainImpactSupportsServiceCatalogShapesLive`; they are now
`TestRepositoryArmBoundsLive` and `TestServiceCatalogSupportsShapesLive`. The old
prefix is selected by the reducer contention gate's supply-chain `-run` filter. Two
runs of the first test exist and they took different paths. The 182.9 s run
([repository_arm_bounds_raw.txt](repository_arm_bounds_raw.txt)) used
`B2C_REPO_ARM_DSN` against the existing `b2c_hot` database (9,000 scopes), where the
test skips its seed; the subtests took 0.55 s and 55.9 s, and the rest is the
8,000-row rewrite on the large table (about 126 s). The 15.0 s run
([contention_tests_seconds_raw.txt](contention_tests_seconds_raw.txt)) used the
fresh-seed path on a fresh 2 GB container, which is the path CI always takes (CI sets
no `B2C_REPO_ARM_DSN`). Neither new name matches either contention
filter (checked against both `-run` patterns in
`.github/workflows/reducer-contention-gate.yml`), and both files keep their
`live-postgres-readiness` header, so the proofs run in the readiness shards. The
shard model weighs an unmeasured test at its 3.8 s default, so the first CI run of
the first test is its real weight.

`TestServiceCatalogSupportsShapesLive` is a lab-corpus proof: it skips unless
`B2C_REPO_ARM_DSN` points at a database holding `repo:hotrepo` and the other
repositories of its three call shapes, and the shards never set it. It does not run
in CI. Its figures above come from the lab run in
[service_catalog_shapes_and_drain_raw.txt](service_catalog_shapes_and_drain_raw.txt);
the many-small-repositories proof is the one that covers the loader's call shape in
CI.

## The service-catalog call shapes and the repository drains

Raw: [service_catalog_shapes_and_drain_raw.txt](service_catalog_shapes_and_drain_raw.txt).
Lab corpus: three repositories of 160,000 supports and 200 of 1,250, on the index
migration 346 builds. The loader's first design drained all requested ids as one
statement: one repository of 160,000 read 5,908, 5,899 and 2,958 buffers for page 1,
midpoint and last page; three of 160,000 read 6,719, 6,303 and 2,958; 200 repositories
plus one of 160,000 read 92,259, 66,872 and 3,758, past the 15,000 bound (200 lateral
keys of up to 500 index entries each). The loader now drains one repository id at a
time, as the ruling authorized: the largest page of any repository in the three shapes is
5,908, 5,912 and 5,954 buffers, temp 0 and at most 50 ms warm, both plan cache modes
(201 drains for the third shape). Equality and concurrency tests pass with the
per-repository drain and migration 346 present.

A call of 200 repositories with five supports each, through the loader on a fresh
database ([many_small_repositories_raw.txt](many_small_repositories_raw.txt)): 201
statements (one readiness probe and one page per repository id), 98 ms wall time (0.49 ms a
repository). A call costs at least one statement per repository id, plus a page for every
500 supports of a repository.

The 200-repository single-drain shape failed (92,259 buffers a page, against a bound
of 15,000); the per-repository-id drain is the shipped form.

The service loader returned `records=0` for `repo:hotrepo` in the built-binary run
only because the lab seed changed the `repository_id` column and not the
`repository_id` in the payload, which is the value the loader groups its records by;
the drain read all 160,000 active rows. In real data the column and the payload value
are the same. It is not a read problem.

Built binary after the I/O change of the host volume (about 15:37Z; buffer counts are
unaffected, timings are not comparable with the earlier runs): `repo:hotrepo` through
`GetSupplyChainAdvisoriesForRepos` is 321 pages, 160,000 raw and active rows, 6.97 s;
through the reconciliation loader 321 pages, 160,000 envelopes, supports drain 1.9 s,
whole loader 13.0 s (the fact stream takes the rest); peak RSS 431 MB for both runs in
one process. That is 21 ms a page for the service loader and 6 ms for the
reconciliation one, against the 58 to 146 ms a page the ruling quotes from the earlier,
slower volume.

## When the package array is non-empty

`securityAlertReconciliationFilter` (`reconciliation_handler.go`) builds the four
arrays from the intent's own envelopes: a provider repository-alert fact adds its
`repository_id`, `package_id`, CVE and GHSA ids, and a package-registry package
fact adds its `package_id` (or its scope id). So the package array is non-empty
for every item that carries a package-registry package fact and for every
provider alert that names a package. How often that is in QA data was not
measured here.

## The legacy payload array arms

The legacy reconciliation statement also matched `payload->'cve_ids' ?| ...` and
`payload->'ghsa_ids' ?| ...`. The typed finding payload
(`reducerderivedv1.SupplyChainImpactFinding`) has no such field, so those arms
never matched a finding fact, and keeping them would make the OR unindexable.
`TestSupplyChainImpactFindingPayloadCarriesNoIDArrays` fails if either field is
ever added.

## Equality

`TestSecurityAlertReconciliationAndAdvisoryLoadersEqualFactReadsLive` writes a
multi-scope seed through the production impact writer (two repositories sharing
a package, a CVE-less advisory finding, a repository-less finding, a superseded
generation, a retracted finding, interleaved alert and consumption fact ids) and
old-writer direct inserts (an operator-scope row, a numeric and a blank
repository id, a tombstone). It compares each new read with the frozen
finding-fact read for every filter shape: same fact ids, same kind, same payload,
same order. It runs once after the backfill is ready and once after an old-writer
insert and the drain. Deleting the CVE arm of the supports query fails its `cve`
and `combined` cases.

## Census of finding-fact reads left in the paths

`git grep` over `go/internal/storage/postgres` for the finding fact kind, outside
tests: the security-alert and service-catalog loaders read none (the one literal
in `facts_active_security_alert_reconciliation.go` is the kind the supports arm
reports). The remaining readers are the impact holder reads (fanout, key range,
mass, repository holders, scoped packages, out of stamp, the Go-modules fence:
#11197, PR-B2b), the supports backfill, drain and maintainer, the schema's partial
indexes, and the canonical-winner audits. `securityAlertReconciliationFactKinds`
and the three key-backfill statements no longer list the impact kind
(`TestSecurityAlertReconciliationPathsReadNoFindingFacts`). Rows the key sidecar
built for impact findings before this change stay, unread.

## Poll cost of a not-ready deferral

While the supports table is not ready a reducer intent defers without counting an
attempt. `TestSupplyChainImpactSupportsNotReadyDefersNonCountingLive` runs the real
queue, fact store and reconciliation handler on PostgreSQL 18.6 and logs one cycle
(claim, alert-fact load, readiness probe, retry update): 4 statements, 22 to 66 ms
on a loaded host, 19 to 163 KB of WAL (163 KB on the first cycle, 19 to 52 KB
after). The intent re-claims about every 60 s. This is measured in the test, not
on a built binary.

## No-Regression Evidence

No-Regression Evidence: baseline is the finding-fact loaders on `5bec799579`
(join form, 313 to 389 ms at 600,000 supports and 159 s at 8,000,000, cold);
after is the supports loaders on PostgreSQL 18.6 over the synthetic corpora
above: raw-first, 0.1 to 51 ms at 600,000 and 0.09 to 0.17 s at 8,000,000 with a
`package_id` btree, 0.9 s warm and about 100 s cold without it. Worker, lease and
batch settings are unchanged: each loader is one read statement per page. The
finding-fact write path gains no statement; the advisory-group cell admission
uses the production writer step (one more statement per admission batch, the
supports upsert, in the same transaction).

## Observability Evidence

Observability Evidence: `eshu_dp_supply_chain_impact_supports_reads_total`
(`source` = reader, `outcome` = `served`, `not_ready`, `failed`) shows each
loader's use of the supports table; a refusal is a retryable, non-counting error
with failure class `supply_chain_impact_supports_not_ready` in the durable queue.
`eshu_dp_supply_chain_impact_supports_backfill_ready` says when the refusals
stop. Both are documented in
[supply-chain-impact-supports.md](../../../public/reference/telemetry/supply-chain-impact-supports.md).

No-Observability-Change: #11200 (the key sidecar stops covering the impact kind)
changes no signal; the existing `eshu_dp_supply_chain_impact_supports_reads_total`
counter and the key backfill status keep their meaning.

## Open points

- The `package_id` btree above needs a migration and an arbiter ruling on its
  write cost before the reconciliation reader is enabled at QA scale.
- `LookupKeyBackfill` and `MarkSecurityAlertReconciliationKeysReady` have no
  production caller, so the keyed reconciliation reader is never ready unless a
  marker is set by hand (recorded on #10875).
