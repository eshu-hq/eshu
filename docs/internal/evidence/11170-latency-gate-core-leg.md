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

## Open decision

Whether the gate should run `inventory.Backfiller` itself after seeding (as it
already runs the winners maintainer's own resweep), or wait for the product to
wire the backfill, is with the arbiter. Until it is settled the winners and
legacy sweeps have no green run, and this note carries no latency numbers.
