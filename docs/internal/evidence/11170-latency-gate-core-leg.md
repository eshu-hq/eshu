# #11170: latency gate core leg, RED baseline and findings

This note records the failing baseline for `scripts/verify-read-api-latency-gate.sh`
before the core leg, what the rewired gate hit on its first full-corpus run,
and the product gap the run found. It holds raw observations. The latency and work
numbers of the green runs are in the calibration and confirm sections.

## Commit references

This branch is merged with a merge commit and is not rebased, so the SHAs cited here
that are on the branch stay reachable from `main`, among them `cc750777cd`,
`52df6e8c43`, `25c35c5c7d` (the commit the table headers cite) and `ff9bcf780b`.
Four SHAs are not on the branch and not on GitHub: `ca232f7a89`, `4903719505`,
`46db679819` and `707674e184`. They belong to the pre-rebase history of this
branch, and the runs made at them are earlier calibration or confirm runs. Each is
labelled where it is cited.

## Host

One shared Linux box (16 CPUs, 61 GB RAM) running other lanes. Every run below
shared it. IO pressure (`/proc/pressure/io`, `some avg60`) and the 1-minute
load average are recorded beside each run, and they are high. Treat any
latency figure from this box as a noisy upper bound, not a budget proof.

## RED baseline

Command, run from a throwaway detached worktree at `origin/main` (`f2ec26f68b`):

```text
ESHU_DEPLOY_CHECKOUT=<eshu-deploy at the CI-pinned 0a645a87> ESHU_GRAPH_BACKEND=neo4j \
GATE_COMPOSE_PROJECT=eshu-11170-red GATE_TOTAL_SCOPES=40 GATE_NODES_PER_LABEL=500 \
GATE_IAC_FACT_COUNT=500 GATE_SHARED_INTENT_COUNT=5000 GATE_ITERATIONS=3 \
bash scripts/verify-read-api-latency-gate.sh
```

The deploy copy had one local change: the Neo4j healthcheck got
`retries: 60` and `start_period: 180s`, because under this host's IO the stock
10 retries flapped (`container ... is unhealthy` on `compose up --wait`). That
file is outside the repository.

| Run | Result | Exit |
| --- | --- | --- |
| Plain `origin/main` | Dies in the seed step: `seeded Postgres tables do not hold the expected row counts: ingestion_scopes: got 42, want 41; scope_generations: got 58, want 57` | 1 |
| `origin/main` plus only the seed-verify fix (`git cherry-pick --no-commit` of the migration-295 count commit) | Seed completes, then `{"level":"ERROR","msg":"eshu-api exited","error":"eshu-api: --core-base-url is required"}` and `verify-read-api-latency-gate: eshu-api /readyz never returned on port 18197` | 1 |

IO pressure at the second run: avg60 53.5 at start, 48.5 at end. Load 33.

The first row is the migration 295 `synthetic:advisory-groups` standing scope and
generation that `VerifyRelationalCounts` did not count. It is a separate bug,
fixed on `origin/main` in `95e4bb6d0c` (this branch carried the same fix before the rebase and dropped it). The second row is the failure the issue
describes: the frozen satellite `eshu-api` (pin `c041d7ec`) refuses to start
without `--core-base-url`, so the gate cannot pass on any branch.

Two earlier attempts at the second run failed for host reasons, not gate
reasons, and are listed so they are not mistaken for a result: `db-migrate`
exceeded the script's 120 s wait while Neo4j applied schema statements at about
2 s each under load 80, and Neo4j failed its stock healthcheck once.

## Findings from the rewired gate

The branch starts the in-tree `eshu-core-api` first, then the satellite with
`--core-base-url`, then the MCP server, then sweeps a legacy leg and a
winners-read leg.

1. **Satellite listen address.** The satellite honors only `-addr` and
   defaults to `:8080`. It ignores `ESHU_API_ADDR`. The first full-corpus run
   logged `eshu-api serving ... "addr":":8080"` and then
   `eshu-api /readyz never returned on port 18197`. Fixed by passing
   `-addr=":${GATE_API_PORT}"`, the same flag the golden corpus gate already
   passes.
2. **Infra read model backfill has no runner.** After the address fix the
   seed, core, satellite, and MCP all came up and the legacy sweep started,
   then stopped with
   `infra read model not ready after 10m0s: infra read model installed but the backfill marker is not recorded yet`.
   See the next section.

## The infra read model backfill had no caller

This section records the state at the time of the finding (base `194d4abbd9`).
It was fixed in #11210 (the reducer now runs `inventory.RunBackground`, in
`go/cmd/reducer/infra_inventory_backfill_wiring.go`).

The gate waits for `infra_resource_entity_backfill_markers` to hold a row. The
only writer is `inventory.Backfiller.Run`, started through
`inventory.RunBackground`.

Callers at `origin/main`, checked with
`git grep -n -E 'RunStartupBackfills|StartInfraInventoryBackfill|inventory\.RunBackground' origin/main -- go`:

| Symbol | Production callers | Test callers |
| --- | --- | --- |
| `query.RunStartupBackfills` | none | none |
| `query.StartInfraInventoryBackfill` | `query.RunStartupBackfills` only | none |
| `inventory.RunBackground` | `query.StartInfraInventoryBackfill` only | `background_live_test.go` |
| `inventory.Backfiller.Run` | `inventory.RunBackground` only | `backfill_live_test.go`, `fence_backfill_live_test.go` |

History, from `git log -S`:

| Commit | Date | Effect |
| --- | --- | --- |
| `f7a21e6fc7` | 2026-09-19 | #6793 adds `StartInfraInventoryBackfill`, run by the API and MCP server |
| `cf357fcf47` | 2026-10-31 | Removes the in-tree `eshu-mcp-server`; its `query.RunStartupBackfills` call goes with it |
| `a012da4557` | 2026-11-17 | Removes the in-tree API (`go/cmd/api`); the last `query.RunStartupBackfills` call, in `wiring_graph.go`, goes with it |

Nothing replaced either call. `go/cmd/core-api`, the reducer, the ingester,
`bootstrap-data-plane`, and the satellite `eshu-mcp` repository do not call
the backfill. The satellite `eshu-api` at the pin mentions it only in OpenAPI
description text (`git grep` at `c041d7ec`), never in code. A design note (`7126-ingest-time-config-artifacts-evidence.md`) still
says the backfill "runs in the API process today".

### What the product does without the marker

- `inventory.ReadModelReady` returns false. The unscoped infra aggregate
  (`GraphInfraResourceAggregateStore.readModelServes`) and the IaC inventory
  (`PostgresIaCInventoryStore.readModelServes`) then serve from the graph or
  the active-generation CTE path. No error, no 5xx.
- Each aggregate read is counted in `eshu_dp_infra_inventory_reads_total`
  with `source=graph`. The IaC inventory path has no matching counter.
- The status report carries the state: `InfraInventorySnapshot` reports
  `backfilling` with `marker_present: false`. With no runner, that state never
  changes on its own.
- The backfill's own failure log (`infra_inventory.backfill.failed`) cannot
  fire, because nothing starts it.

So a current deployment stays on the slower graph path for these reads and
shows `backfilling` indefinitely. That is a product gap, separate from #11170.

## Decision and what the gate does now

The arbiter ruled that the gate runs `inventory.Backfiller.Run` itself in the
seed phase, before any core starts, labelled `infra_backfill: gate_stand_in` in
both legs' latency reports. Product wiring was #11196, merged as #11210 while this
work was in review: the reducer now owns the backfill. The infra route numbers
therefore describe an install that holds the marker, and a regression in the
reducer's wiring would not show in the gate. On the full corpus the backfill took 15.8 s for 201
repositories and 1,050,000 rows.

## RED chain, in order

1. Plain `origin/main`: `seeded Postgres tables do not hold the expected row counts: ingestion_scopes: got 42, want 41` (seed verify).
2. The seed fix alone: `eshu-api: --core-base-url is required`.
3. The rewired gate without the backfill: `infra read model not ready after 10m0s: infra read model installed but the backfill marker is not recorded yet`.

## Calibration runs

Three separately seeded runs of the rebased head (runs 1 and 2 at `ca232f7a89`, run 3
at `4903719505`, on `origin/main` `194d4abbd9`; both SHAs are pre-rebase commits, not
on GitHub), full default corpus (800 scopes, 150,000 nodes per label, 150,000
IaC facts, 2,500,000 shared intents, 20,000 finding facts), Neo4j, io pressure
avg60 42 to 73 and load 25 to 30 during the runs (shared, noisy host). Every run
exited 1 on the work breaches being recalibrated only: zero `HardFailed`, zero
latency breaches, the coverage floor met, every named route exercised. Both legs
exercised 77 of 128 routes and operations (four provider-unconfigured auth
routes excluded). Findings pages were byte-identical across the legs.

| Route | Legacy blocks | Winners blocks | Legacy p95 | Winners p95 |
| --- | --- | --- | --- | --- |
| `GET /api/v0/supply-chain/impact/findings/count` | 102,546 | 801 | 127 and 130 ms | 10 and 14 ms |
| `GET /api/v0/supply-chain/impact/inventory` | 102,546 | 801 | 119 and 122 ms | 9 and 11 ms |
| `GET /api/v0/cloud/ssm-host-inventory` | 2,455 | 2,455 | 18 ms | 5 and 6 ms |
| `GET /api/v0/documentation/facts` | 36 | 36 | 2 and 5 ms | 2 and 3 ms |

The two p95 figures per cell are run 1 and run 2. Latency numbers come from a
loaded host and are not budget evidence; the work counters do not depend on
host CPU, and they matched across the two runs.

## cloud/ssm-host-inventory: 2,455 blocks for zero rows

`EXPLAIN (ANALYZE, BUFFERS)` of `buildSSMHostActiveFactsSQL` on the seeded
stack (800 scopes, no SSM facts): a nested loop over the 802 active scopes with
one index probe each on `fact_records_scope_generation_idx` (`Index Searches:
802`), 2,406 of the 2,454 execution buffers, zero rows, 2.355 ms. That is about
three buffers per active scope, so the cost of an empty answer grows with the
scope count. Product issue: #11208.

## documentation/facts: 36 blocks against the 33-block default

`pg_stat_statements` over 22 requests, repeated in two batches with identical
counts, with a 5 s idle control window that held no statements:

| Statement | Blocks per request |
| --- | --- |
| The route's facts SELECT (keyset index scan, 25 rows removed by the `fact_kind` filter, none returned) | 32 |
| Scope status lookup | 3 |
| Auth token lookup | 1 |

All 36 blocks are the route's own statements. No core background statement
landed in the window, so `-background-idle` is not the cause, and the plan shape
is sound. The corpus grew: the 20,000 seeded finding facts put about 25 facts in
every scope. The route stays unnamed and moves the default row through the
formula, from 33 to 108 blocks.

## The stand-in is temporary

#11196 (reducer-owned infra backfill) is merged on `origin/main` as #11210. The
gate starts no reducer (it runs the core, the satellite and the MCP server, all
built from the worktree), so it cannot exercise the reducer-owned path. The
stand-in stays and stays labelled `gate_stand_in` until the gate starts the
reducer, asserts that the reducer records the marker, and drops the stand-in:
that is #11226.

## Rows the ratchet refused against the committed legacy table

The committed legacy table was last rendered on 2026-10-19 and 2026-10-20, before
the gate stopped being runnable. Rendered against it, the refresh script refused
two rows. Neither comes from the seeded 20,000 findings. The arbiter ruled on both.

### MCP search_entity_content: 311,960 to 2,652 blocks (accepted, #8682)

The seed counts verify exactly and the pilot checks the exact response page on
every request, so the corpus is intact. Commit `29753641d7` (2026-10-27, #8682,
after the table) renamed the synthetic search repositories from `seed-search-` to
`seed-gate-search-` so the entity pilot seeds sort into the first 1,000-row
ordered probe, and its README says measurements from before and after are
separate fixture cohorts. This is a fixture change, not a faster query. The row now
guards the dense first-probe path only. The gate meter read 396 and 400 blocks per
request in the two calibration runs, and 884, 889 and 642 in the later runs
(8 calls and 16 rows every time; the table budgets 15 calls and 32 rows). A manual probe on
the kept stack of the third run (one request after warmup, `pg_stat_statements`
reset before it) read 8 statements that sum to about 884 blocks: the walk 1 call,
12 rows, 759 blocks, the substring readiness probe 62 plus nested 60 and 2, auth 1,
one read-only transaction. The two readings are not reconciled here. The gate's
entity pilot uses its own statement snapshot meter, and the manual probe counts
nested statements as well as their callers. #8682 recorded 11 calls, 18 rows, 186
blocks for its own cohort; the readings are not comparable with either.

The row is not stable across separately seeded runs: 396 and 400 blocks in the two
calibration runs, 884 in the third run, 889 in the first confirm run and 642 in
the final confirm run, with the same 8 calls and 16 rows each time. No other metered route differs by more than
10% across the four runs. The first render used the two calibration reports and
set the budget to 1,200, which the first confirm run's 889 still passed. The tables
were then re-rendered from all three calibration-quality reports (the third run
met the conditions too), so the budget is 3 x 884 = 2,652, and the confirm run was
repeated on the new tables. The cause of the two modes is not known.

### MCP search_file_content: 798 to 1,740 blocks (accepted, #10607)

The #8682 attribution was rejected: the rename does not change the route's order.
The per-statement proof ran on the kept stack of the third seeded run (legacy
core, the MCP pilot body once after a warmup, `pg_stat_statements_reset()` before
each request, two runs with identical output):

| Statement | Calls | Rows | Blocks |
| --- | --- | --- | --- |
| Keyed snippet re-read (`match_snippet`, added by #10607, `f6f8806260`) | 1 | 10 | 297 |
| Walk (`UNION` over `content_files`) | 1 | 12 | 128 |
| `eshu_require_content_substring_indexes_ready()` | 1 | 1 | 62 |
| nested `EXISTS` index probe | 1 | 1 | 60 |
| `eshu_require_content_lower_trgm_index_ready()` | 1 | 1 | 15 |
| nested lower-trigram `NOT EXISTS` | 1 | 1 | 15 |
| `SELECT state FROM content_substring_index_state` | 1 | 1 | 2 |
| auth token lookup | 1 | 0 | 1 |
| `set_config` | 2 | 2 | 0 |
| `begin` and `rollback` (one read-only transaction each, two in all) | 4 | 0 | 0 |
| Sum | 14 | 29 | 580 |

The sum equals the gate's measured row exactly. The committed row implies 13 calls,
20 rows and 266 blocks (22 = ceil(13 x 1.25) + 5; 40 = 2 x 20; 798 = 3 x 266).
Growth is +314 blocks; the re-read is 297 of them (94.6%). The walk did not grow.
The remaining 17 blocks (the nested lower-trigram `NOT EXISTS`, 15, and the state
read, 2) are not shown to come from the 2026-10-20 window: the lower-trigram probe
exists since 2026-10-12, and the old composition cannot be reproduced. Calls and rows
do not close either: #10607 predicts +4 calls and +11 rows, the committed row
implies +1 and +9. Every other row's calls also fell by about that amount, which
fits background statements of the removed in-tree API in the old windows, but that
is a theory.

## Other rows that moved more than 25%

For each, the new value is shown; the old value is not reproducible and is not
attributed. Per request, on the kept stack of the third run:

- `infra/resources/count`: one statement reading 24,910 blocks (39 rows), the
  marker check 2, auth 1. `iac/resources`: one statement, 24,972 blocks, marker 2,
  auth 1. `infra_resource_entities` is 24,894 heap blocks, 1,050,000 rows, 0 dead
  tuples; 3 x 24,914 = 74,742. The committed 149,436 is about twice that.
- `repositories/{repo_id}/story`: calls 33 (722 blocks), 38 (262 blocks), 60
  (180 blocks) of keyed `content_files` reads, plus 3 (a 19,296-block correlation
  statement and two 1,650-block statements). The seed holds 33 story repositories.
  The committed 389 calls is not attributed.
- Default-row floor: see above. The default calls fell from 20 to 14 because the
  largest unnamed route now reads 7 calls (supply-chain security-alerts
  reconciliations count) where it read 12.

## Confirm runs

Both confirm runs seeded the full default corpus afresh and ran the script end to
end (`bash scripts/verify-read-api-latency-gate.sh`, no flags).

| Run | HEAD | Tables | Exit | Notes |
| --- | --- | --- | --- | --- |
| First | `46db679819` (pre-rebase, not on GitHub) | first render (two reports per leg) | 0 | earlier confirm run, superseded when the tables were re-rendered |
| Earlier final | `707674e184` (pre-rebase, not on GitHub) | render from three pre-vacuum reports | 0 | both legs "all 77 exercised routes within budget", findings pages byte-identical; superseded by the seed vacuum |

Final run, host io pressure avg60 33.5 at the start and 54.9 at the end (shared,
noisy host). The tables it enforced, by sha256 as the gate logged them:
`read-api-route-work-budgets.txt` `a958861c9660c02596ee4406f8229e1ed76157e80e51fc02cb8c2e8cb91c8f4c`,
`read-api-route-work-budgets-winners.txt` `1d8a7ea8f460566bf14217abbac8bb0e04446937743c7bb8321b6b92840eb8c8`,
`read-api-route-budgets.txt` `35b576671f5a08a2b10f0a34d63495461347b29303a9c7248b423299092c3cca`.
Its reports: `latency.json` `c2fdf11f944ec7f973ac7cae737728b8f991ea5cbd4f6ab86785afbf002755dc`,
`latency-winners.json` `c9f5c02c6157d0358d6929d698c3141d6ad73e235568ae22817b29c547cf2d74`,
`work.json` `b8e8558039ff31f76a74ecedb0891d3ae3ae7b5421915f1804d51adae771ff98`,
`work-winners.json` `8130bbfbbc15c5f521ab1a6f2bbeee7b5cbf1c8c325239467c2a75d1444fbdda`.

| Route | Legacy blocks | Winners blocks | Legacy p95 | Winners p95 |
| --- | --- | --- | --- | --- |
| `supply-chain/impact/findings/count` | 102,546 | 801 | 141 ms | 19 ms |
| `supply-chain/impact/inventory` | 102,546 | 801 | 134 ms | 20 ms |
| `cloud/ssm-host-inventory` | 2,455 | 2,455 | 5.5 ms | 5.3 ms |
| `documentation/facts` | 36 | 36 | 2.1 ms | 4.2 ms |
| `MCP search_file_content` | 580 | 580 | 14 ms | 4.8 ms |
| `MCP search_entity_content` | 642 | 642 | 8.4 ms | 2.0 ms |

Seeded RED (the #10880 AC4 proof): the first confirm run's legacy work report,
evaluated against the winners table, breaches both impact aggregate routes (102,546
blocks against a 2,403-block row each) and nothing else.

## #11224: the entity search row follows the visibility map

Observed on one full-corpus seed kept after the seed phase (project
`eshu-11170-bim`, 1,050,367 `content_entities` rows, the gate's seed with its own
`ANALYZE`, autovacuum still working), by `EXPLAIN (ANALYZE, BUFFERS)` of the
first-window entity statement (`entityStepFirstSQL`, parameters `gateentitytoken`,
1000, 11, `max_parallel_workers_per_gather = 0`), then again after
`VACUUM (ANALYZE) content_entities` on the same database:

| | As seeded | After `VACUUM (ANALYZE)` |
| --- | --- | --- |
| `relallvisible` / `relpages` | 13,475 / 32,977 | 32,933 / 32,977 |
| `n_dead_tup`, `n_mod_since_analyze` | 0, 0 | 0, 0 |
| Second arm (Index Only Scan on `content_entities_repo_path_start_idx`) | Heap Fetches 754, 507 buffers | Heap Fetches 0 |
| Whole statement (Append) | 512 buffers | 32 buffers |
| First arm (the 11-row probe) | 5 buffers | 5 buffers |
| Execution time | 0.340 ms | 0.232 ms |

The plan nodes are the same in both states, so planner statistics are ruled out.
The first 1,000 rows in key order sit on 709 distinct heap blocks (block range
15,023 to 32,932) in both states, so heap layout is ruled out too. As seeded,
`last_autovacuum` was 3 minutes before the first probe and 41% of pages were
all-visible. The gate's own readings for the walk statement were 759 blocks in the
high mode and about 270 in the low mode (the gate's 884 and 396 minus about 125
blocks of readiness probes and the auth lookup). The visibility-map state of the
five earlier runs was not recorded, so the mapping from reading to state is
inferred from this arithmetic. Only one database was seeded. The raw `EXPLAIN`
output was not kept in the repository; the table above holds the figures taken from
it.

The first fix vacuumed the seven counted tables. Review found two read-model
tables the seed fills before that step and budgeted routes read,
`infra_resource_entities` and `supply_chain_impact_canonical_winners`; both are now in
the list. The first calibration run on the seven-table version was stopped
mid-seed, so it produced no reports and there is no before and after for those two
tables; the later calibration reports are all from the nine-table version.

## Calibration with the seed vacuum

Three separately seeded full-corpus runs at `25c35c5c7d` (the seed ends with
`VACUUM (ANALYZE)` over nine tables), each exit 0 on both legs: zero `HardFailed`,
zero latency breaches, 77 of 128 routes exercised (floor 77), every named route
exercised, byte-identical findings. No metered counter moved more than 10% across
the three runs, on either leg. io pressure avg60 ranged from 14 to 42 across the runs
(shared, noisy host). The winners rebuild logged `inserted=4000`, the count the seed now
asserts.

| Row | Before the vacuum (five runs) | With the vacuum (three runs) |
| --- | --- | --- |
| `MCP search_entity_content` blocks | 396, 400, 642, 884, 889 | 157, 158, 162 |
| `MCP search_file_content` blocks | 580 in each | 580 in each |
| `infra/resources/count` blocks | 24,912 to 24,914 | 24,913 |
| `iac/resources` blocks | 24,974 to 24,976 | 24,975 |
| `supply-chain/impact/*` legacy / winners blocks | 102,546 / 801 | 102,546 / 801 |
| `freshness/generations` blocks | 30,953 in each | 19,899 in each |

The entity walk now reads 157 blocks: the 32 of the vacuumed Index Only Scan plus
the 125 of the readiness probes and the auth lookup, which is what the #11224
probe predicted. The seed vacuums the two read-model tables the infra, iac and
impact routes read as well; their blocks did not change, as expected for routes
that scan the table, so the table list costs nothing there and removes a dependence
on autovacuum timing. `freshness/generations` fell by a third; that is consistent
with the visibility map but the plan was not checked.

Rendered against the committed tables (each baselined on its own copy), neither
table needed an accept flag: the entity row falls 2,652 to 486 (5.5x, under the 20x
drop factor), and only that row and `freshness/generations` (92,858 to 59,696)
moved more than 25% on either leg. The header notes keep the history of the two
earlier acceptances (#8682, #10607) and of the bimodal row.

## Final confirm run

`bash scripts/verify-read-api-latency-gate.sh`, no flags, from a throwaway worktree at
`ff9bcf780b` (the tables and code of the final render), fresh full default corpus:
exit 0, started 08:56:27Z and ended 09:22:44Z (26 min 17 s); io pressure avg60 32
at the start and 59 at the end (shared, noisy host). Both legs: "exercised 77/128
routes (floor 77)", "all 77 exercised routes within budget", zero `HardFailed`,
findings pages byte-identical, "PASS: read-api-latency-gate green (legacy + winners
legs)". The seed logged `winners rebuild inserted=4000 updated=0 deleted=0
unchanged=0`, `infra read model backfilled ... duration=1m20.2s` (the earlier runs
took 16 to 24 s; this host was loaded), and `VACUUM (ANALYZE) finished in 40.254s`.

Tables the gate enforced, by sha256 as it logged them:
`read-api-route-work-budgets.txt` `0a493e2e0888d88695b69eac1d71dbc171779a10a7a886c47ddb7613ba615224`,
`read-api-route-work-budgets-winners.txt` `e0316bfb0a7a661ef8bc1b7aea2acaccdd4128a3ab7cb0a0412c4f015cdc99ea`,
`read-api-route-budgets.txt` `35b576671f5a08a2b10f0a34d63495461347b29303a9c7248b423299092c3cca`.
Reports: `latency.json` `39f0a93f5ddf00b0b799b9ca18df91e24dde863296c314e904894c33eab93bbf`,
`latency-winners.json` `8c6e0e3e5380916fb0dcf60884d3cee786b86bd0af132dbb560f147546140125`,
`work.json` `aae85a0918177935a4fee0c7d06ad2e7b2d790218d995a74a18de69d00d124d6`,
`work-winners.json` `fd639860a00e5179922adb8c29be1986c40973700b57c72abb32b6a4c0c2268e`.

| Route | Legacy blocks | Winners blocks | Legacy p95 | Winners p95 |
| --- | --- | --- | --- | --- |
| `supply-chain/impact/findings/count` | 102,546 | 801 | 139 ms | 16 ms |
| `supply-chain/impact/inventory` | 102,546 | 801 | 129 ms | 17 ms |
| `cloud/ssm-host-inventory` | 2,455 | 2,455 | 5.7 ms | 7.7 ms |
| `documentation/facts` | 36 | 36 | 2.6 ms | 2.6 ms |
| `freshness/generations` | 19,899 | 19,899 | 28.5 ms | 26.7 ms |
| `infra/resources/count` | 24,912 | 24,912 | 265.5 ms | 290.1 ms |
| `MCP search_file_content` | 580 | 580 | 4.2 ms | 5.5 ms |
| `MCP search_entity_content` | 164 | 164 | 2.0 ms | 2.8 ms |

The entity row read 164 blocks, the fourth reading with the vacuum (157, 158, 162,
164), against its 486-block budget. Latency numbers come from a loaded host and are
not budget evidence.

## Performance and observability evidence

Performance Evidence: this change touches the gate tool only (its budget report type,
seed and sweep order), not a production hot path. The measured subject is the gate's
own work budget. Baseline: the committed legacy table before this branch read 2,652
blocks for the entity-search row (bimodal readings 396 to 889 blocks across runs).
After: three fresh runs with the nine-table seed `VACUUM (ANALYZE)` read 157, 158
and 162 blocks (a fourth reading 164), budget 486, with no other counter moving more
than 10% across runs. Backend: Neo4j and the Postgres from the CI-pinned
`eshu-deploy` compose (`0a645a87`). Input shape: 800 scopes, 150,000 nodes per label,
150,000 IaC facts, 2,500,000 shared intents, 20,000 finding facts; the winners seed
inserts 4,000 rows. Terminal counts: 77 of 128 routes exercised on both legs, zero
`HardFailed`. Why safe: the change adds a typed not-exercised class and a seed
maintenance step; no query, queue or lease path changes.

No-Regression Evidence: the final confirm run exited 0 on both legs with every
exercised route within its table budget and the findings pages byte-identical.

No-Observability-Change: no metric, span, log key or status field is added to a
runtime service. The seed logs `VACUUM (ANALYZE) finished in <duration>` as gate
output for the operator running the gate.
