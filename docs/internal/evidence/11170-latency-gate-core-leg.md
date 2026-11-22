# #11170: latency gate core leg, RED baseline and findings

This note records the failing baseline for `scripts/verify-read-api-latency-gate.sh`
before the core leg, what the rewired gate hit on its first full-corpus run,
and one product gap the run found. It holds raw observations only. Latency
numbers land in a later section once a full green run exists.

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
fixed test-first in this branch. The second row is the failure the issue
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

## The infra read model backfill has no caller

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
both legs' latency reports. Product wiring is #11196, worked separately. The
infra route numbers therefore describe an install that holds the marker. A
fresh install serves those routes from the graph, and this gate does not
measure that path. On the full corpus the backfill took 15.8 s for 201
repositories and 1,050,000 rows.

## RED chain, in order

1. Plain `origin/main`: `seeded Postgres tables do not hold the expected row counts: ingestion_scopes: got 42, want 41` (seed verify).
2. The seed fix alone: `eshu-api: --core-base-url is required`.
3. The rewired gate without the backfill: `infra read model not ready after 10m0s: infra read model installed but the backfill marker is not recorded yet`.

## Calibration runs

Two separately seeded runs of the rebased head (`ca232f7a89` on `origin/main`
`194d4abbd9`), full default corpus (800 scopes, 150,000 nodes per label, 150,000
IaC facts, 2,500,000 shared intents, 20,000 finding facts), Neo4j, io pressure
avg60 42 to 73 and load 25 to 30 during the runs (shared, noisy host). Both runs
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

#11196 (reducer-owned infra backfill) is merged on `origin/main`. The gate's
reducer image only runs the product backfill once the deploy image pinned in CI
carries it, so the stand-in stays and stays labelled `gate_stand_in`. The
follow-up is to drop the stand-in and assert that the reducer itself records the
marker, once the pinned image carries #11196.

## Rows the ratchet refused against the committed legacy table

The committed legacy table was last rendered on 2026-10-19 and 2026-10-20, before
the gate stopped being runnable. Rendered against it, the refresh script refused
two rows. Neither comes from the seeded 20,000 findings. The arbiter ruled on both.

### MCP search_entity_content: 311,960 to 1,200 blocks (accepted, #8682)

The seed counts verify exactly and the pilot checks the exact response page on
every request, so the corpus is intact. Commit `29753641d7` (2026-10-27, #8682,
after the table) renamed the synthetic search repositories from `seed-search-` to
`seed-gate-search-` so the entity pilot seeds sort into the first 1,000-row
ordered probe, and its README says measurements from before and after are
separate fixture cohorts. This is a fixture change, not a faster query. The row now
guards the dense first-probe path only. The gate meter read 400 blocks per request
(15 calls, 32 rows budgeted) in each of the three seeded runs. A manual probe on
the kept stack of the third run (one request after warmup, `pg_stat_statements`
reset before it) read 8 statements that sum to about 884 blocks: the walk 1 call,
12 rows, 759 blocks, the substring readiness probe 62 plus nested 60 and 2, auth 1,
one read-only transaction. The two readings are not reconciled here. The gate's
entity pilot uses its own statement snapshot meter, and the manual probe counts
nested statements as well as their callers. #8682 recorded 11 calls, 18 rows, 186
blocks for its own cohort; the readings are not comparable with either.

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
