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
two rows. Neither comes from the seeded 20,000 findings.

| Route | Committed blocks | Measured blocks (both runs) | Ratio |
| --- | --- | --- | --- |
| `MCP search_entity_content` | 311,960 | 1,200 budget from 400 measured | collapse past 20x |
| `MCP search_file_content` | 798 | 1,740 budget from 580 measured | 2.18x growth |

What is already known. The seed counts verify exactly in both runs, and the
pilots check the exact response page on every warmup and counted request, so the
corpus is intact. Commit `29753641d7` (2026-10-27, #8682, after the table) renamed
the synthetic search repositories from `seed-search-` to `seed-gate-search-` so
the entity pilot seeds sort into the first 1,000-row ordered probe window, and its
README says measurements from before and after the rename are separate fixture
cohorts. That README also says the historical variance in #8682 remains
unexplained, so the cause of the file-search growth is not proven.

What a single run would prove. The rename is one constant
(`seedSearchRepoPrefix` in `seed_content_files.go`). A throwaway worktree at the
same head with the old value, run on the same corpus and the same legacy leg,
would show whether both rows return to their committed ratios. It has not been
run; it waits for the arbiter's ruling.
