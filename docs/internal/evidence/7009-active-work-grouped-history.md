# Status active work: gated grouped history (#7009)

## Scope and cause

`StatusStore.ReadStatusSnapshotFiltered` reads stage counts, domain backlog,
queue state, conflict blockage, and the latest failure from one statement,
`activeWorkSummaryQuery` (`read="active_work_summary"`). Before #7009 the
statement joined every `fact_work_items` row (about 254,000 on the ops-qa
capture of 2026-10-05) to its scope state and generation, materialized the
joined set, and counted `fact_work_items` a second time for `total_count`. Most
rows are succeeded history, which the summary reads only as `(stage, status)`
counts and `succeeded_count`. The C3 shim attributed most of the cost to
per-row CPU, not I/O.

The first fix (A2) grouped history before the generation join on every poll.
The S4 and S5 shims showed A2 is 1.28-1.46x the baseline when most rows are
live, so the shipped statement is the S5 variant `a2v`: it groups history only
when the statistics say most rows are history. The shim reports and the
arbiter rulings are out-of-tree harness artifacts in the latency kit
(eshu-latency-kit/latency-reconcile-20261002/control-diagnostic/harness-worktree:
7009-s5-shim-report-20261005.md, 7009-s5-ruling-20261006.md,
7009-s4-ruling-20261005.md, 7009-a2-ruling-20261005.md).

## Change and semantics

- Gate: the first CTE, `fact_work_summary_mode`, sums the `pg_stats`
  most-common-value frequencies of the six detail statuses
  (`activeWorkDetailStatuses`: pending, claimed, running, retrying, failed,
  dead_letter) for the table `'fact_work_items'::regclass` resolves to, never
  `current_schema()`. The sum is COALESCEd to 0, so the gate is never NULL and
  absent statistics take the grouped branch. `grouped` is the estimate below
  `activeWorkGroupedThreshold` = 0.4 (S5 R2.4; T is re-measured, never tuned in
  place). The gate is an uncorrelated InitPlan, evaluated once per execution.
- Grouped branch (estimate < 0.4): the A2 statement.
  `active_fact_work_items` reads only the six statuses;
  `fact_work_status_groups` counts every row once by
  `(scope_id, generation_id, stage, status)`; `fact_work_history_counts`
  applies the same scope and generation join to each group of the other
  statuses. `total_count` sums the grouped pass.
- Detail branch (estimate >= 0.4): the detail input is every row, the grouped
  pass reads nothing, and `total_count` is one `COUNT(*)` of
  `fact_work_items`, as before #7009.
- The input is two one-time-filtered arms under `UNION ALL ... OFFSET 0`, so
  the arm of the branch not taken never executes. `succeeded_count` is the
  grouped history sum plus the detail rows' succeeded count; one of the two is
  empty.
- A sixth result section, `mode`, returns `{mode, estimate}`: the branch the
  gate took and the estimate read again in the same statement. The decoder
  accepts only `grouped` or `detail`. The status snapshot the API and MCP
  serve is unchanged.
- No schema, index, lock, lease, claim, or write change. No migration.

Render pins. Without the mode section the Go render is the S5 shim's
`s5_a2v_T.sql` byte for byte (SHA-256
`89d33b86f51f8ae44e7d6f980d6e4d84c0d2eca0738dd78bbf4e561294f421bf`), its
forced renders are `s5_a2v_g.sql` and `s5_a2v_d.sql`, and the three gate
mutations are the shim's mutation files
(`TestActiveWorkSummaryIsTheMeasuredA2vRender`). The full render is pinned at
`9d833808212a48859e1d5a26a9fd1c378f4bc366e6521876d7938834ccc96cae`
(`TestActiveWorkSummaryQueryIsThePinnedRender`); any edit re-pins it and
reruns the oracle differential.

## Proof

Commit binding: the local live PostgreSQL 18.3 results in this section (the
differential, the mutations, the regclass proof, and the guard; the final
local live set was 41 PASS) ran at `2f0057d8e` on base `5c4e03613`. From
`2f0057d8e` to the pre-rebase tip `9f87bcd82` the only storage change is a
comment in `status_active_generation_index_bench_test.go` (4 lines added, 1
removed). `go/internal/storage` is tree-identical between `9f87bcd82` and the
rebased branch (`a423c1b0`), and the storage and CI diff has the same
patch-id on both sides of the rebase (`6111e373`). The base delta to
`3c0de54e5` (PR-1) changes no storage file and no `go.mod` or `go.sum`; PR-1
touched only `go/internal/runtime/postgres` and docs. The reducer-contention
gate then ran every #7009 live proof on the rebased head `78d178ecf` (CI run
37454693298, Reducer Contention Gate, success): the oracle differential, both
mutation controls, the regclass proof, the terminal-text proof, and the
plan guard all PASS, with the same section-row counts as the table below.

- Oracle: `activeWorkSummaryPreHistoryGroupsOracle` is the pre-#7009 query,
  rendered from origin/main `9bcca588f` and pinned to the shim baseline
  SHA-256 `bef1078e2f5b971f6f35f576403b5c7197b59b6e37ccf13974d3240e4d432e2e`.
  `TestActiveWorkSummaryDiffersFromOracleOnlyInHistoryGroups` removes the gate,
  the mode section, and the grouped-history pieces and gets the oracle back,
  byte for byte. The oracle stays pinned; its lifecycle is unchanged (a shared
  fragment change re-derives it by those substitutions with sign-off, or
  retires it in favor of `TestActiveWorkSummaryMatchesStandaloneReads`).
- Section statuses: `TestActiveWorkSummarySectionStatusesStayWithinDetailStatuses`
  requires every section status predicate outside grouped history and
  `succeeded_count` to use `IN` or `=` with only the six statuses.
- Differential: `TestActiveWorkSummaryMatchesPreHistoryGroupsOracle` runs the
  oracle beside the shipped render and both forced renders (threshold literal
  `2`, always grouped; `-1`, never grouped) in one statement on PostgreSQL
  18.3, comparing section rows with `EXCEPT ALL` both ways (mode rows
  excluded). Every render is equal on every case. Each render's mode row must
  name its branch, the shipped mode must agree with its estimate, and on an
  analyzed state the estimate must be within 0.02 of the true live share.

| case | rows | shipped mode | estimate |
| --- | ---: | --- | ---: |
| 8 edge cases: 7 shim ports and the history fence (no statistics) | 1-12 | grouped | 0 |
| 0.1% live, analyzed | 11 | grouped | 0 |
| 20% live (busy shape), analyzed | 28 | grouped | 0.2 |
| 50% live, analyzed | 31 | detail | 0.5 |
| 80% live, analyzed | 31 | detail | 0.8 |
| 100% live, analyzed | 26 | detail | 1 |
| 80% live behind pre-update statistics, autovacuum off | 23 | grouped (wrong branch) | 0 |
| #6794 semantics fixture | 35 | detail | 0.953 |

- Seeded history faults (`TestActiveWorkSummaryOracleCatchesSeededHistoryMutations`,
  applied to the forced-grouped render so the grouped path runs on every
  case): dropping `failed` or `claimed` from the status partition, dropping
  `retrying` from the detail filter only, dropping the stale-generation
  predicate, joining history without the scope key, counting `total_count`
  from the joined rows, and counting `succeeded_count` from the unjoined
  groups. Each differs from the oracle on 6 to 11 cases.
- Gate mutations (`TestActiveWorkSummaryGateMutationsAgainstOracle`, S5 ruling
  D5.6, shipped render): (a) the forced wrong branch is equal everywhere (the
  differential above); (b) dropping the history `NOT IN (six)` differs on 10
  cases; (c) a NULL statistics subquery is equal on every case; (c2) a NULL
  `grouped` flag differs on 10 cases, so an edit that drops the COALESCE fails.
- Regclass: `TestActiveWorkSummaryGateResolvesTableByRegclass` puts
  `pg_catalog` first on the search_path. The gate still estimates the analyzed
  20%-live fixture (0.2), while a `current_schema()` control estimates 0, the
  always-grouped vacuous gate.

## Generation scan guard

`checkSummaryGenerationScans` binds the #4446 guard to the per-row rescan cost
class (A2 ruling D1). For every `Seq Scan on scope_generations` line:

- R1: the nearest shallower plan line must be `Hash`, `Parallel Hash`, a hash
  join on either side, `Sort`, `Gather`, or `Gather Merge`.
- R2: with actuals, loops on the scan and its parent are at most the nearest
  `Gather`'s `Workers Launched` plus one, or 1.
- R3: at most one such scan inside `active_fact_work_items`, at most one inside
  `fact_work_history_counts`, none elsewhere; no detail CTE fails closed.

`TestStatusActiveFactWorkItemsCTEUsesGenerationIndex` now runs the guard on
the 100,000-generation fixture in both gate states, reached by seeding the
live share (about 78% live as seeded: detail; unleased live rows rewritten to
succeeded and analyzed: grouped), then on a forced generic plan of the
prepared statement after six executions (`$1` in the plan). Each plan must
have actuals, the gate CTE node must run with `loops=1`, and R1-R3 must hold.
The seeded nested-loop plan is still rejected in both CTEs.
`TestCheckSummaryGenerationScansAcceptsTheGatedDetailPlan` pins a PostgreSQL
18.3 detail-branch plan (gate CTE, gate InitPlans, both arms) as accepted, so
the CTE extractor is not mis-scoped by the new leading CTE, and rejects the
same plan with the gate at `loops=2000`. The fixed-text cases of
`TestCheckSummaryGenerationScansRejectsDetailFullScan` are unchanged. Known
limitation: the standalone `stageCountsQuery` check still rejects any
`Seq Scan on scope_generations`.

## Performance Evidence:

LOCAL FIXTURE evidence only, from the S5 shim run 2 (2026-10-06 00:51-01:40
EDT). Not ops-qa, no endpoint p95. PostgreSQL 18.3 primary and streaming hot
standby, `--cpus 2 --memory 1536m`, ops-qa row counts, statements on the
standby with `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF)`, rotating first mover,
CPU canary before every sample, load1 3.2-8.8 on 18 CPUs (the 8.8 maximum in
cell C35r). Every cell was valid twice (5 then 7 repetitions). Equality
105/105. All variant arms ran with `jit=off`. The per-cell medians with
min-max in ms, the buffers and costs of every arm, and the full T-selection
table are in the shim report, 7009-s5-shim-report-20261005.md (sections R2.1
and R2.4), in the latency kit named above; the table below keeps the ratios
the acceptance verdict uses.

`a2v` / base (jit=off; jit=on), runs a and b, branch from the gate estimate:

| cell | est | branch | a2v run a | a2v run b | A2 / base jit=off (a; b) |
| --- | ---: | --- | --- | --- | --- |
| C0.1 | 0.0010 | grouped | 0.326; 0.297 | 0.321; 0.326 | 0.297; 0.297 |
| C10 | 0.1000 | grouped | 0.421; 0.430 | 0.422; 0.428 | 0.422; 0.404 |
| C20 | 0.2018 | grouped | 0.634; 0.645 | 0.632; 0.623 | 0.591; 0.609 |
| C30 | 0.2989 | grouped | 0.720; 0.734 | 0.734; 0.752 | 0.700; 0.702 |
| C40 | 0.4007 | detail | 1.027; 1.024 | 1.064; 1.036 | 0.844; 0.820 |
| C50 | 0.4980 | detail | 1.032; 1.001 | 1.021; 1.038 | 0.928; 0.907 |
| C80 | 0.8004 | detail | 1.031; 0.976 | 1.035; 1.049 | 1.304; 1.281 |
| C80s (stale VM) | 0.8024 | detail | 1.051; 0.751 | 1.031; 0.816 | 1.337; 1.222 |
| C100 | 1.0000 | detail | 1.071; 1.065 | 1.023; 1.013 | 1.459; 1.421 |
| C100 at 4MB | 1.0000 | detail | 1.041; 1.032 | 1.039; 1.033 | 1.416; 1.406 |

Verdict: `a2v` passes every acceptance cell in both runs against both
baselines (worst 1.071, C100 run a); `a2u` and `a2s` were not admitted (S5
ruling D3). Buffers at C0.1: base 45,933, A2 45,242, a2v 45,127; at C100: base
159,165, A2 100,622, a2v detail 161,056.

Threshold: A2 is at or below 1.00x the baseline through 50% live and crosses
over between 50% and 60%, so T = 0.4. The gate estimate tracked the true live
share within 0.45 percentage points. At C40 the estimate sits on T; both
branches pass there. The informational C45r cell passes against the jit=off
baseline in both runs (the jit=on split there is identical-plan noise, ruling
D2).

- 0.1% live: a2v is A2 + 3-10 ms (1.04-1.10x A2), -65 to -68% vs base.
- Forced-grouped inside the stats window: <= A2 + 24 ms measured, 1.55x base
  worst sample. The bound that applies (ruling D1.2): variant_g median <= A2
  median + max(5% of base median, 3 x identical-plan-noise ms) AND <= 1.60 x
  base, inside the measured stats-replay window only. The gate is not free.
- Stats window: 71.2 s to the standby estimate 0.8019, one sample, with
  autoanalyze after a 23.49 s autovacuum VACUUM. Window bound: 2 x
  `autovacuum_naptime` + autovacuum VACUUM duration + ANALYZE duration +
  replay lag.
- Generic plans: generic/custom 0.954-1.048 in every cell, arm, and attempt;
  R1-R3 hold on all 640 samples; the gate InitPlan runs once per execution.
- Mode section: not in the shim text. On the Go guard fixture its estimate
  re-read was one InitPlan of 0.130 ms with 93 catalog buffer hits (one
  EXPLAIN ANALYZE sample, host load1 about 20; not a timing claim).

JIT dependency. The a2v statement costs 114-249k on the fixture, above the
default `jit_above_cost` of 100,000. The ops-qa reader inventory (run
c5544da49fcd5c68) reads `jit=on`, `jit_above_cost=100000`, `work_mem=64MB`,
and costs the pre-#7009 `active_work_summary` at 62,530, below the JIT
threshold. Every a2v number above is at `jit=off`. PR-1 (#7639), merged as
`3c0de54e5`, issues `SET LOCAL jit = off` once at the start of the status
snapshot transaction (see 7009-status-snapshot-jit-off.md). Its measured
wrapper overhead: the semantic-only read (one statement, N=201 per arm) was
+0.259 ms (paired +0.285 ms), with a direct `SET LOCAL jit = off` median of
0.176 ms. Its caveats, quoted from that note: host load1 was 13 to 23 on 18
CPUs, so those runs "report interleaved relative deltas, plan costs, and JIT
function counts, and make no absolute-latency claim"; the full-read deltas
"change sign between runs (+55.6, -48.3 ms) and are unresolved at this load";
and the SET changed no plan's JIT decision in those runs.
`TestStatusSnapshotDisablesJITBeforeTheGatedActiveWorkSummary` (runtime
postgres package) binds the dependency in code (S5 ruling D4.4): a full
status snapshot through the real `StatusStore` must issue the SET once, as
its first statement, before the gated summary. Removing the SET, or moving
it after the status read, fails the test. That test drives the status
reader's fallback path, where the SET goes through `QueryContext`; the
guarded reader's `execControl` path is covered by PR-1's
`TestGuardedStatusSnapshotJITSettingIsControlSQL`, and both paths share the
SET-before-read ordering in `snapshotStatusReader.read`.

## Observability Evidence:

`eshu_dp_status_snapshot_read_duration_seconds{read="active_work_summary",outcome}`
still times this read. New: the span active on the read context (the
`postgres.status_snapshot` span under the snapshot reader) carries
`status.active_work.summary_mode` (`grouped` or `detail`) and
`status.active_work.summary_estimate`, so a slow sample is attributable to its
branch and to stale statistics. `TestReadStatusSnapshotRecordsActiveWorkSummaryMode`
proves both attributes on a recorded span and none without a mode row. There
is no status read log line, so no log key was added.

## Not proven

- The ops-qa plan shape and timing of `a2v`, and the ops-qa compare packet
  (S5 ruling D5.8) against the new pin.
- Endpoint or bundle p95, and the API/MCP transport time.
- A built-binary or deployed run, and the ops-qa `jit` setting after PR-1.
- The share of ops-qa or production time above T, the ops-qa statistics
  replay lag, and a bootstrap-state visibility map.
- The stats window beyond one sample.
- The mode section's cost on the shim fixture.
