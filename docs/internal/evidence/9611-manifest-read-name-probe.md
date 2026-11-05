# #9611 Manifest Dependency Read: Probe Each Requested Name

`FactStore.ListActivePackageManifestDependencyFacts` read every page through a
flat `package_manager = ANY($1) AND folded name = ANY($2)` predicate. Even with
a custom plan (#9586), each page scanned the whole package-manager prefix of
the dependency index, so its cost followed the manager's total dependency rows,
not the requested names.

## Cause

PostgreSQL uses statistics on an index expression only when the index is not
partial (`selfuncs.c`, the `indpred == NIL` branch). Every payload expression
here is indexed only by partial indexes, so the planner falls back to default
selectivities. `package_manager = ANY('{npm}')` gets 0.005 and a 400-name array
gets 86.5 %. The planner treats the name array as useless and scans the
package-manager prefix, filtering by name. The lane 3 claim on the issue
carries the original EXPLAIN evidence and the ruled-out causes: expression
text, collation, partial-index implication, `is_tombstone`, and array size.
`CREATE STATISTICS` on the expressions was measured and rejected.

## Change

Both active texts (`manifest_dependency_read_sql.go`) now de-duplicate the
requested managers and names with `SELECT DISTINCT ... unnest(...)` and drive
a `CROSS JOIN LATERAL` probe per (manager, folded name) pair. The probe's
equality on both leading columns of migration 247's
`fact_records_active_package_dependency_folded_entity_idx` is its Index Cond.
The probe keeps the active-generation joins, the tombstone filter, and the
keyset cursor. It returns its own first `$5` rows in `(observed_at, fact_id)`
order, and the outer sort keeps the first `$5` of those.

Every row matches exactly one (manager, folded name) pair, so the union of the
per-pair top `$5` rows contains the flat read's top `$5`. The page, its order,
and the next cursor are the same. The inner `LIMIT` also fences the LATERAL
body: the planner cannot pull it up into a hash join, the trap the owned
resolver's `OFFSET 0` records.

The Maven-family text's first branch takes the same probe. Its second branch,
the sbt `%%` artifact match, is unchanged. `deriveDeltaOverlayBaseQuery`
swaps the delta base joins inside the probe, so the base pages take the same
shape. No statement is added. The read sends the same pages, target lookup,
base resolution, and overlay inputs as before, and both texts keep
`QueryExecModeCacheDescribe`: the sbt branch still has `= ANY($2)` and
`<> ALL($2)` residual filters. No new index and no write-cost change.

The owned resolver slices the folded-name expression out of the plain text.
Its end marker moved from `= ANY($2::text[])` to `= requested_name.name`; the
expression it takes is byte-identical.

Rejected on the same fixture (issue thread): LATERAL without the inner
`LIMIT` (slower than main), a flat join to `unnest` (slower), and
`CREATE STATISTICS`.

## Performance Evidence:

Disposable PostgreSQL 18 container (`postgres:18`, default settings:
`shared_buffers` 128 MB, `work_mem` 4 MB, `random_page_cost` 4, JIT on) on a
shared 12-core host. The load average was 22 to 50 during the runs, so
buffers are the primary metric and times are indicative. Three databases,
`lane3_9611_1x`, `_5x`, and `_10x`, each with every migration applied and
the same seed at scale `k`:

- 400 * k git scopes. Each has a superseded old full and a current full with 440
  dependency Variables (300 npm, 60 pypi, 40 nuget, 30 maven, 10 sbt) and 200
  other content entities, all carrying `repo_id`.
- Names are skewed (index `floor(V * r^2)`), so low indexes are hot.
- Every 10th scope is delta-active: its current full is superseded and a 5-row
  delta is active over it.
- Every 97th row is a tombstone. Rows share seven `observed_at` values per
  generation, so ties are common. `VACUUM (ANALYZE)` ran after the seed.
- Fact rows at 1x, 5x, and 10x: 432,200, 2,161,000, and 4,322,000 (352,200,
  1,761,000, and 3,522,000 dependency rows).

Each figure is the median of five runs, interleaved main/new. The 10x run was
stopped at the investigation deadline after `npm400_p1`, `npm400_mid`,
`npm400_last`, and `npm400_base_mid`; every other shape is NOT_CHECKED at 10x
and covered at 1x and 5x. Buffers are
shared hit plus read from the last run. "Custom" binds literals, the plan the
read runs under `QueryExecModeCacheDescribe`. "Generic" is `PREPARE` with
`plan_cache_mode = force_generic_plan`. Row sets were compared by md5 of the
positional row text in output order and matched in every shape measured, at
every scale.

Threshold, stated before the runs: for the 400-name npm read, page 1 and a
cursor page, under the custom plan, the rewrite must use at most 50 % of main's
buffers and at most 50 % of main's execution time at every scale. Its generic
plan must be no worse than 1.1x main's generic plan. Every other shape must be
no worse than 1.1x main or +2 ms. Rows must be identical.

Shapes: `npm400` is 200 hot and 200 rare npm names. `_mid` uses a cursor at
row 2,500 and `_last` the cursor 100 rows before the end. `npm3` is three hot
names, `pypi_nuget` 400 mixed-case names over two managers, and `npm_zero` 400
names with no row. `maven600` is the Maven family (gradle, maven, sbt) with 500
coordinates and 100 sbt artifacts. `_base` shapes run the derived base text
with every delta-active scope in `$6`; `base1` and `base4` bind 1 and 4 scopes.

### Active pages, custom plan

| shape | scale | rows | main ms | new ms | main buffers | new buffers | buffers new/main |
| --- | --- | --- | --- | --- | --- | --- | --- |
| npm400_p1 | 1x | 500 | 550.2 | 222.7 | 343,907 | 126,750 | 0.37 |
| npm400_p1 | 5x | 500 | 6,626.7 | 1,546.0 | 1,779,488 | 637,545 | 0.36 |
| npm400_p1 | 10x | 500 | 14,167.7 | 2,888.9 | 3,558,625 | 1,168,345 | 0.33 |
| npm400_mid | 1x | 500 | 273.8 | 123.6 | 145,415 | 59,779 | 0.41 |
| npm400_mid | 5x | 500 | 2,969.0 | 896.7 | 946,280 | 365,268 | 0.39 |
| npm400_mid | 10x | 500 | 3,497.8 | 1,799.3 | 1,259,196 | 642,568 | 0.51 |
| npm400_last | 1x | 100 | 26.1 | 7.9 | 3,502 | 1,733 | 0.49 |
| npm400_last | 5x | 100 | 108.2 | 7.4 | 18,787 | 1,825 | 0.10 |
| npm400_last | 10x | 100 | 33.7 | 5.5 | 14,138 | 2,227 | 0.16 |
| npm3_p1 | 1x | 500 | 395.2 | 30.2 | 254,887 | 13,326 | 0.05 |
| npm3_p1 | 5x | 500 | 4,564.2 | 81.0 | 1,281,542 | 40,374 | 0.03 |
| pypi_nuget_p1 | 1x | 500 | 160.5 | 60.0 | 100,554 | 27,220 | 0.27 |
| pypi_nuget_p1 | 5x | 500 | 781.8 | 198.0 | 512,853 | 122,531 | 0.24 |
| npm_zero | 1x | 0 | 354.9 | 3.0 | 242,894 | 1,206 | 0.00 |
| npm_zero | 5x | 0 | 5,670.4 | 3.1 | 1,215,335 | 1,206 | 0.00 |
| maven600_p1 | 1x | 500 | 113.5 | 89.2 | 64,643 | 42,034 | 0.65 |
| maven600_p1 | 5x | 500 | 559.1 | 367.8 | 334,244 | 195,670 | 0.59 |

### Derived base pages, custom plan

| shape | scale | rows | main ms | new ms | main buffers | new buffers | buffers new/main |
| --- | --- | --- | --- | --- | --- | --- | --- |
| npm400_base_p1 | 1x | 500 | 317.9 | 86.3 | 266,498 | 51,030 | 0.19 |
| npm400_base_p1 | 5x | 500 | 5,342.8 | 865.6 | 1,333,589 | 250,930 | 0.19 |
| npm400_base_mid | 1x | 500 | 159.0 | 45.6 | 105,477 | 20,601 | 0.20 |
| npm400_base_mid | 5x | 500 | 2,078.7 | 373.1 | 648,134 | 121,911 | 0.19 |
| npm400_base_mid | 10x | 500 | 344.2 | 572.3 | 141,901 | 241,059 | 1.70 |
| npm400_base1_p1 | 1x | 36 | 234.8 | 39.3 | 243,512 | 28,044 | 0.12 |
| npm400_base1_p1 | 5x | 39 | 4,886.5 | 553.0 | 1,215,980 | 133,321 | 0.11 |
| npm400_base4_p1 | 1x | 147 | 269.6 | 47.7 | 245,384 | 29,916 | 0.12 |
| npm400_base4_p1 | 5x | 131 | 5,151.4 | 598.4 | 1,217,681 | 135,022 | 0.11 |
| maven600_base_p1 | 1x | 318 | 51.7 | 41.6 | 45,952 | 23,343 | 0.51 |
| maven600_base_p1 | 5x | 500 | 193.1 | 107.1 | 229,592 | 94,478 | 0.41 |

### Generic plan

| shape | scale | rows | main ms | new ms | main buffers | new buffers | buffers new/main |
| --- | --- | --- | --- | --- | --- | --- | --- |
| npm400_p1 | 1x | 500 | 1,280.6 | 238.2 | 126,130 | 126,750 | 1.00 |
| npm400_p1 | 5x | 500 | 16,511.9 | 1,565.3 | 1,779,488 | 637,545 | 0.36 |
| npm400_p1 | 10x | 500 | 29,292.2 | 2,581.8 | 3,558,625 | 1,168,345 | 0.33 |
| npm400_mid | 1x | 500 | 1,062.6 | 154.8 | 126,130 | 75,772 | 0.60 |
| npm400_mid | 5x | 500 | 10,890.3 | 1,189.1 | 1,571,309 | 433,861 | 0.28 |
| npm400_mid | 10x | 500 | 22,302.7 | 1,923.8 | 3,157,906 | 777,010 | 0.25 |
| npm400_last | 1x | 100 | 135.9 | 52.7 | 126,130 | 27,926 | 0.22 |
| npm400_last | 5x | 100 | 4,930.0 | 564.8 | 1,215,935 | 133,276 | 0.11 |
| npm400_last | 10x | 100 | 8,616.5 | 965.7 | 2,431,960 | 265,326 | 0.11 |
| npm_zero | 1x | 0 | 1,243.2 | 3.1 | 126,130 | 1,206 | 0.01 |
| npm_zero | 5x | 0 | 15,260.1 | 3.2 | 1,215,335 | 1,206 | 0.00 |
| maven600_p1 | 1x | 500 | 585.7 | 218.2 | 141,407 | 42,034 | 0.30 |
| maven600_p1 | 5x | 500 | 3,254.2 | 1,009.9 | 334,244 | 195,670 | 0.59 |
| npm400_base_p1 | 1x | 500 | 136.9 | 117.6 | 14,214 | 51,030 | 3.59 |
| npm400_base_p1 | 5x | 500 | 14,095.8 | 1,696.2 | 1,333,589 | 250,930 | 0.19 |
| npm400_base1_p1 | 1x | 36 | 4.6 | 39.0 | 572 | 28,044 | 49.03 |
| npm400_base1_p1 | 5x | 39 | 4,955.3 | 569.4 | 1,215,980 | 133,321 | 0.11 |
| npm400_base4_p1 | 1x | 147 | 15.2 | 48.8 | 1,613 | 29,916 | 18.55 |
| npm400_base4_p1 | 5x | 131 | 5,159.8 | 604.4 | 1,217,681 | 135,022 | 0.11 |

No plan reads `fact_records` with a sequential scan. The new plan for
`npm400_p1` at 1x is a nested loop over 400 de-duplicated names. Each probe is
an index scan of the folded entity index with both columns in the Index Cond
(400 index searches) and `NOT is_tombstone` as its only Filter.

Planning time is about the same for both texts (16 to 49 ms under this load).
`QueryExecModeCacheDescribe` replans every page, so it counts once per page
for both.

Result against the threshold: **not met in two cells.**

- The six `npm400` threshold cells: page 1 met at 1x, 5x, and 10x (0.37, 0.36,
  and 0.33 of main's buffers; 0.40, 0.23, and 0.20 of its time). The cursor
  page met at 1x and 5x (0.41 and 0.39 buffers) and missed at 10x by 1.0 point
  on buffers (0.510) and 1.4 points on time (0.514). Main's 10x cursor plan is
  scope-driven: a hash join of the 4,000 active scopes, then
  `fact_records_collector_status_active_idx` per scope with the cursor in its
  Index Cond, so it reads active-generation rows only. The probe reads each
  name's rows in every stored generation, and this corpus keeps one superseded
  full per scope, so it reads twice the name rows.
- Other shapes: met everywhere measured, except the derived base cursor page
  at 10x (`npm400_base_mid`), where the probe read 241,059 buffers in 572.3 ms
  against main's 141,901 in 344.2 ms (1.70x the buffers). Main drives that page
  from the 400 delta-active scopes in `$6`: base generation, then
  `fact_records_scope_generation_idx`. Its cost follows the base rows of the
  delta-active scopes. The probe cannot use the `$6` restriction, so it reads
  the names' rows fleet-wide. At 1x and 5x main's custom plan for the same page
  read the manager prefix instead, and the probe was 0.20 and 0.19 of its
  buffers.
- The generic plan met its bar on `npm400` (at most 1.1x main). On the base
  pages at 1x, main's generic plan drove from the scopes and read less (see
  Known limits). The read runs custom plans.

The page cost now follows the requested names and their rows, as the issue
asks. Where main finds a scope-driven plan (a cursor page at fleet scale, or a
base page with few delta-active scopes), that plan can read less than the
probe.

## Proof

- Text guard, red first: `TestManifestDependencyQueriesProbeEachRequestedName`
  checks both active texts and both derived base texts. It failed on the flat
  texts (exit 1) and passes after. `TestManifestNameProbeGuardFiresOnTheFlatShape`
  is the seeded-violation pair: the flat name match, the flat manager match, a
  missing inner limit, and non-unique names each fire, and the shipped text is
  quiet.
- Live parity: `TestManifestDependencyProbeTextsMatchFlatTextsLive` freezes the
  pre-#9611 texts (from `0650a6a4d2`) and runs them and the shipped texts
  through the same seam, `listManifestDependencyFactsWithQuery`, on one corpus.
  That is the same pages, target lookup, base pages, and overlay. The corpus
  has 12 scopes and three delta-active scopes with changed, deleted, re-emitted,
  and tombstoned base keys. It has one folded name ("hot", written three ways)
  with more than a page of rows, duplicate names in `$2`, the same name under
  two managers, tombstones, `observed_at` ties, and the sbt artifact branch.
  Four reads run in the delta-active state and again with no delta-active
  scope: 639, 831, 1,107, and 0 rows, then 744, 936, 1,320, and 0, each
  identical, envelope by envelope. It also passed on the flat texts before the
  change, which proves the harness, not the change.
- Live plan: `TestManifestDependencyProbePlanUsesTheFoldedIndexLive` seeds 400
  scopes and requires that both texts read `fact_records` through one
  folded-index probe, with the manager and the name in its Index Cond. The flat
  text fails the same check (the seeded violation). On the 12-scope parity
  corpus the planner drives each probe from the scopes instead, which is cheap
  there, so the plan check uses the fleet-shaped corpus.
- The #9991, #10003, owned-resolver, and manifest live sets pass on the
  branch (commands below).

## Statement counts

The committed #9991 harness
(`active_package_manifest_dependency_delta_bench_test.go`, tag
`perf9991_manifest_delta`) seeds its 156,201-row corpus (2,001 scopes, 201
delta-active, 400 names) and counts every statement of the whole read through
the Go path. It ran on `0650a6a4d2` and on this branch, alternating, twice
each, on the same container. Statement totals exclude BEGIN and COMMIT, one
each per read.

| state | read | statements, base and branch | rows, base and branch |
| --- | --- | --- | --- |
| delta-active, 4 reached scopes | npm | 48 and 48 (23 active pages, 1 target lookup, 21 base pages, 1 base lookup, 1 probe, 1 inputs) | 20,000 and 20,000 |
| delta-active | maven, no rows | 3 and 3 | 0 and 0 |
| no delta-active scope | npm | 42 and 42 | 20,000 and 20,000 |
| no delta-active scope | maven, no rows | 2 and 2 | 0 and 0 |

Whole-read medians of five, second pair (base at load average 8.5, branch at
13.3, so the branch ran on the busier host): delta-active npm 4.234 s on base
and 2.768 s on the branch, no-delta npm 4.864 s and 3.221 s. The no-row Maven
reads stayed in the tens of milliseconds (delta-active 52.95 ms and 32.16 ms,
no-delta 19.94 ms and 12.78 ms). The
first pair ran at load 34.4 and 19.4 and is not comparable (base 7.238 s and
5.671 s, branch 2.258 s and 3.166 s).

## Known limits

- A probe reads each requested name's rows in every stored generation and
  rejects the superseded ones in the joins, because the folded index does not
  carry the active generation. A page therefore costs up to `$5` index rows
  per hot name, so the cost follows the requested names' total rows, which
  grow with the fleet.
- When delta-active scopes are a small share of the fleet, a scope-driven base
  page reads less than the probe: 1.70x fewer buffers on the 10x base cursor
  page above. At 1x, main's forced generic plan for the base page with one
  delta-active scope also drove from the scope, at 4.6 ms and 572 buffers
  against 39.0 ms and 28,044 for the probe. At 5x the same generic plan read
  the manager prefix again (4,955 ms). Main's custom plan, the one the read
  runs, picks between these shapes on default selectivities. The base text is
  derived from the active text, so a scope-driven base would be a second
  shape and a design decision.
- Not tried within the budget: a change that cuts the superseded-generation
  rows a probe reads (join order is the planner's, and the derivation pins
  both join fragments), and a scope-driven base page.
- The Maven-family second branch still scans the `sbt` prefix.
- Not checked: QA scale and QA plan choice, non-default `random_page_cost` or
  `shared_buffers`, a quiet host, and eight of the twelve shapes at 10x.

## Observability Evidence:

No-Observability-Change: the read records the same
`eshu_dp_package_manifest_dependency_read_seconds` and `_pages` histograms
with the same `plain` and `maven_family` labels, because the constants keep
their names, and the same delta-union counter, span event, and WARN lines.
The plan change is visible to an operator as lower `read_seconds` at the same
`read_pages`.

QA readback, once deployed, read-only with a bounded `statement_timeout`:
`read_seconds` and `read_pages` by `read` before and after. Then one
`EXPLAIN (ANALYZE, BUFFERS)` of a 400-name npm page on the QA primary under
`SET LOCAL statement_timeout = '30s'`. It should show the folded-index probe,
with the manager and the name in its Index Cond.

## Commands

The env file sets `ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN` (with its
disposable opt-in), `ESHU_POSTGRES_TEST_DSN`, and
`ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN`. The owned resolver tests also need
`ESHU_POSTGRES_DSN`.

- `go test ./internal/storage/postgres -count=1` (unit, no DSN).
- `go test ./internal/storage/postgres -count=1 -run 'TestManifestDependencyProbe|<#10003 delta-union, guard, manifest, overlay set>|OwnedResolver|OwnedPackage' -v`
  with the live DSNs.
- `go test ./internal/storage/postgres -count=1 -run 'TestResolveOwnedPackageNames|TestListOwnedPackageDependencyTargets.*Live' -v`
  with `ESHU_POSTGRES_DSN`.
- `go test -tags perf9991_manifest_delta ./internal/storage/postgres -run TestManifestDeltaReadStatementCountsLive -count=1 -v`
  on `0650a6a4d2` and on this branch.
