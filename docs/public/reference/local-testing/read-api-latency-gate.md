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
   the CI job's time budget at this scale. Runs `ANALYZE` on the seeded
   Postgres tables afterward so the sweep plans against fresh statistics.
4. Starts `eshu-api` against the seeded backends.
5. Derives every no-arg GET route from the generated surface inventory
   (`capabilitycatalog.LoadSurfaceInventory`, filtered to
   `category=api_route`, `readiness=implemented`, method `GET`, no path
   parameter) — so a newly added route is swept automatically — and measures
   each route's p95 latency (`SweepRoutes`).
6. Compares each route's p95 against `testdata/benchmarks/read-api-route-budgets.txt`
   (`ParseRouteBudgets`/`EvaluateBudgets`) and fails on any breach, a
   coverage-floor shortfall, or an explicitly-budgeted route dropping out of
   coverage.

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
or an early 500 must not pass just because it was quick. A client-side
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
(counted requests per route, 20), `GATE_BUDGETS`. `GATE_API_BIN=<path>`
swaps in a pre-built `eshu-api` binary (built from a different commit, e.g.
main with a candidate fix) instead of building one from this worktree, for a
RED/GREEN comparison without rebasing.

## Budgets

`testdata/benchmarks/read-api-route-budgets.txt` requires a `default` row
that applies to any route the table does not name explicitly, so a newly
added route is always budgeted — never silently unchecked. Both `default`
and any named route may appear only once; a duplicate is a hard parse
error. The `#6793`/`#6794` route families carry post-fix target budgets, not
what the gate measures on unpatched `main`: those routes are expected to
breach until the underlying regressions are fixed. For a route
`RouteCapability` (`budget.go`) maps to a capability with a declared
production p95, the effective budget is
`min(catalog_p95 * catalogCIMultiplier, this table's value)` — the catalog
value only ever tightens the row, never loosens it. See the file's own
header comment for the full provenance, rationale, and the accepted
infra-resource-aggregate coverage gap.

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
