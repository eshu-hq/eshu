# Read API Latency Gate

Issue #6797. Backend-backed p95 latency benchmark for every no-arg GET read
route, gated in CI so a regression like #6793 (infra resource aggregate
full-graph scan) or #6794 (status/readiness routes paying an expensive
per-scope `activeFactWorkItemsCTE` query) cannot merge without this gate
going red.

It exists because that class of regression is invisible to hermetic tests:
the cost only appears at repo/scope scale against a real Postgres+NornicDB
backend, which unit tests and the credential-free `make pre-push`/`make pre-pr`
lanes never exercise.

## What it does

`go/cmd/read-api-latency-gate` (driven by `scripts/verify-read-api-latency-gate.sh`):

1. Brings up the same Docker Compose Postgres+NornicDB pair
   [Golden Corpus Gate](golden-corpus-gate.md) uses.
2. Builds a deterministic seed plan (`BuildSeedPlan`) that distributes a
   configurable total scope count across **every** collector kind
   `scope.AllCollectorKinds` reports — not a hardcoded list, so a new
   collector kind is seeded automatically. Each scope gets one or more
   `scope_generations` rows (a minority get several, modeling re-ingestion
   churn) and a set of `fact_work_items` rows across a realistic status mix
   (`pending`, `retrying`, `claimed`, `running`, `dead_letter`, `succeeded`)
   drawn from `go/internal/queue.WorkItemStatus` — the deprecated,
   legacy-replay-only `failed` status is never seeded.
3. Bulk-writes that plan into Postgres via `pgx.CopyFrom` (`SeedPostgres`),
   seeds `fact_records` IaC `content_entity` facts with a realistic small
   (~1KB) / large (~15KB) jsonb payload mix (`BuildIaCFacts`/`SeedIaCFacts`
   — the #6793 `currentInventoryCTE` jsonb-detoast cost), and seeds
   infra-labeled graph nodes (`TerraformResource`, `K8sResource`,
   `CloudResource`, ...) into the graph backend via one `UNWIND CREATE` per
   label (`SeedGraph`) — not the real ingestion pipeline, which would blow
   the CI job's time budget at this scale. A second graph write
   (`SeedIaCGraphNodes`) adds `uid`-bearing nodes whose `uid`/`id`/`name`/
   `generation_id` match the seeded IaC facts' Postgres `entity_id`/
   `entity_name`/`generation_id`, so `/api/v0/iac/resources` — which selects
   candidates from Postgres and hydrates them from the graph by `uid` — finds
   a matching row for every candidate instead of failing its own
   consistency check ("current inventory and graph projection disagree").
   The bulk `SeedGraph` nodes carry no `uid` on purpose: they exist only to
   give the per-label scan cost realistic volume. Runs `ANALYZE` on the
   seeded Postgres tables afterward so the sweep plans against fresh
   statistics.
   After the graph seeds it reads the per-label node counts back
   (`VerifyGraphNodeCounts`) and fails the run if any label is short: a bulk
   `UNWIND range(0, $count - 1)` once seeded a single node per label on
   NornicDB without an error (see
   [NornicDB Write-Shape Pitfalls](../nornicdb-write-shape-pitfalls.md)).
4. Starts `eshu-api` against the seeded backends, with `pg_stat_statements`
   loaded into Postgres by `docker-compose.read-api-latency-gate.yaml`. The gate
   creates the extension, proves it collects, and refuses a stack that runs more
   than 2 statements per second while idle (a new poller in the API has to be
   understood, not absorbed into the budgets).
5. Derives every no-arg GET route from the generated surface inventory
   (`capabilitycatalog.LoadSurfaceInventory`, filtered to
   `category=api_route`, `readiness=implemented`, method `GET`, no path
   parameter) — so a newly added route is swept automatically — and measures
   each route's p95 latency (`SweepRoutes`).
6. Compares each route's p95 against `testdata/benchmarks/read-api-route-budgets.txt`
   (`ParseRouteBudgets`/`EvaluateBudgets`) and each route's Postgres work per
   request against `testdata/benchmarks/read-api-route-work-budgets.txt`
   (`ParseRouteWorkBudgets`/`EvaluateWorkBudgets`), and fails on any breach, an
   exercised route the meter never read, a coverage-floor shortfall, or an
   explicitly-budgeted route dropping out of coverage.

**Sampling**: for each route, `SweepRoutes` issues 2 discarded warmup
requests (a cold connection and cold Postgres/NornicDB caches make the
first request unrepresentatively slow) and then `-iterations` counted
requests, and reports the true nearest-rank p95 over the counted set — not
the max.

**Per-request outcome**: a 4xx on the warmup probe means this route needed a
selector or auth scope this gate could not supply; it is reported
`Exercised=false` (`NOT_EXERCISED(status)`) and excluded from budget
checks, without spending the counted-iteration budget. `RouteQueryArgs`
supplies a representative seeded selector for routes this gate's own corpus
can back, so those run for real instead of 400ing. A **5xx always fails**
the route regardless of how fast it answered (`HardFailed`) — a query error
or an early 500 must not pass just because it was quick. The breach summary
prints the first counted-sample 5xx response body (capped at 500 bytes) so
the error envelope is visible without re-running. The run script exports
`ESHU_COMPONENT_HOME` (an empty temp directory, a supported zero-components
registry state) because `/api/v0/component-extensions` 503s unconditionally
without it. A client-side
timeout is measured as a (large) latency sample rather than aborting the
sweep, so a regression that manifests as a hung connection — rather than an
error status — still produces a budget breach instead of a silent pass. Only
a genuine connection failure (eshu-api never came up) aborts the run.

**Coverage guards**: `ExercisedCoverageFloor` fails the run if the total
exercised-route count drops below its pinned value (a ratchet — raise it
when coverage genuinely improves, never lower it). Separately,
`RequireNamedRoutesExercised` fails the run if any route this gate
*explicitly* budgets (not merely covered by the `default` row) comes back
not-exercised — a named route silently losing its selector or auth scope
must not quietly stop being checked while the gate stays green.

## Running it

```bash
bash scripts/verify-read-api-latency-gate.sh
```

Flags mirror the golden corpus gate's: `--keep` leaves the stack up for
debugging a breach, `--no-compose` assumes Postgres/NornicDB are already
running. It shares the golden corpus gate's cross-run mutex
(`scripts/lib/live-gate-lock.sh`), so the two Docker-heavy gates serialize
against each other instead of contending for CPU and Docker I/O.

Tunables (env, matching the script's own defaults): `GATE_POSTGRES_PORT`
(15537), `GATE_NEO4J_BOLT_PORT` (7797), `GATE_NEO4J_HTTP_PORT` (7585),
`GATE_API_PORT` (18097), `GATE_TOTAL_SCOPES` (800), `GATE_NODES_PER_LABEL`
(150000), `GATE_IAC_FACT_COUNT` (150000 seeded IaC facts), `GATE_ITERATIONS`
(counted requests per route, 20), `GATE_BUDGETS`, `GATE_WORK_BUDGETS`,
`GATE_WORK_REPORT` (write the per-route measured work as JSON). `GATE_API_BIN=<path>`
swaps in a pre-built `eshu-api` binary (built from a different commit, e.g.
main with a candidate fix) instead of building one from this worktree, for a
RED/GREEN comparison without rebasing.

## Budgets

Two tables, with different jobs.

**Latency ceilings** (`testdata/benchmarks/read-api-route-budgets.txt`) are the
SLO contract. The file requires a `default` row that applies to any route the
table does not name, so a newly added route is always budgeted. Both `default`
and any named route may appear only once; a duplicate is a hard parse error.
For a route `RouteCapability` (`budget.go`) maps to a capability with a
declared production p95, the effective budget is
`min(catalog_p95 * catalogCIMultiplier, this table's value)`. These ceilings
catch catastrophic regressions (timeouts, hung graph reads). They do not fail a
plan-shape regression at this seed scale: locally, the #6794 fix moves the
status routes' p95 by only 1.3x to 7.6x and most of them sat under their 1s
ceiling before it.

**Work budgets** (`testdata/benchmarks/read-api-route-work-budgets.txt`) are the
regression-sensitive check. For every exercised route the gate resets
`pg_stat_statements` after the warmup requests, sends the counted requests, and
reads statements (`calls`), `rows`, and buffer blocks (`blks`: shared, local and
temp, read and hit) per request. A route breaches when any of the three exceeds
its budget, and the report prints all three next to the route's p95. Buffers
separate the pre- and post-fix status routes by 10.7x to 12.7x while the
statement count moves only 29.1 to 26.0, and they do not depend on runner CPU
speed (evidence:
`docs/internal/evidence/6797-read-api-work-metric-shim.md`). The table is
generated: run the gate on the runner class with `GATE_WORK_REPORT`, then
`scripts/refresh-read-api-work-budgets.sh REPORT.json...` renders it with
`calls = ceil(max*1.25)+5`, `blks = ceil(max*3.0)`, `rows = ceil(max*2.0)` over
the GREEN maximum. The five routes the #6794 fix did not change are guards for
future regressions; the fix pair cannot prove them RED.

Not covered: NornicDB exposes no work counter, so the graph side of the #6793
infra aggregate is guarded by its latency ceiling only, and
`shared_projection_intents` is not seeded, so `domainBacklogQuery` is invisible
to this gate.

**Producing a RED run.** The workflow's `workflow_dispatch` takes an `api_ref`
input: that ref's `eshu-api` is built and swept by this branch's gate code, so a
commit from before a fix shows the gate failing on the real runner class. To keep
a stack for debugging locally, run the script with `--keep`; it retains the
live-gate lock and the stack until you `docker compose down -v` and remove
`eshu-live-gate.lock` and `eshu-live-gate.lock.keep` from the git common dir.

## Focused (credential-free) proof

```bash
cd go && go test ./cmd/read-api-latency-gate -count=1
```

This exercises the seed-distribution logic, route selection, the sweep's
p95/warmup/timeout/4xx/5xx handling, and budget parsing/evaluation without a
database — see `go/cmd/read-api-latency-gate/AGENTS.md` for the invariants
those tests pin. `gate_seeded_violation_test.go` is the seeded-violation
RED/GREEN proof: an injected slow handler against a tight budget reports a
breach, and the same handler with the injected delay removed does not.
