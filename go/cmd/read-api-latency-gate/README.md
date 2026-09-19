# Read API Latency Gate

## Purpose

Backend-backed p95 latency benchmark for every no-arg GET read route,
gated in CI so a regression like #6793 (infra resource aggregate full-graph
scan) or #6794 (status/readiness routes paying an expensive per-scope
`activeFactWorkItemsCTE` query) cannot merge without the CI job going red.

Two budget tables gate it, with different jobs. The latency ceilings
(`testdata/benchmarks/read-api-route-budgets.txt`) are the SLO contract and
catch catastrophic regressions; at this seed scale they do NOT fail a
plan-shape regression (the #6794 fix moves latency only 1.3x-7.6x, mostly
under the ceiling). The regression-sensitive check is the per-request Postgres
work budget (`testdata/benchmarks/read-api-route-work-budgets.txt`): buffers per
request differ 10.7x-12.7x between the pre- and post-fix status routes, and do
not depend on runner CPU speed.

## Ownership boundary

Owns: the synthetic seed corpus (scope/generation/fact-work-item and infra
graph node distribution), the route sweep, and the budget comparison. Does
not own the routes themselves (`go/internal/query`), the surface inventory
(`go/internal/capabilitycatalog`), or the reducer/status queries it exercises
(`go/internal/storage/postgres`).

## Exported surface

- `BuildSeedPlan` — pure, deterministic scope/generation/work-item
  distribution across every `scope.AllCollectorKinds` entry
- `SeedPostgres`, `SeedGraph`, `SeedIaCFacts` — bulk-write a `SeedPlan` (plus
  `BuildIaCFacts`' IaC content_entity facts) into a live Postgres/NornicDB
  pair
- `SeedIaCGraphNodes` — bulk-writes uid-bearing graph nodes correlated with
  `BuildIaCFacts`' `entity_id`/`entity_name`/`generation_id`, so
  `/api/v0/iac/resources`' Postgres-then-graph hydration finds a matching row
  for every candidate instead of 500ing ("current inventory and graph
  projection disagree"); additional to, not a replacement for, `SeedGraph`'s
  anonymous bulk `infraLabels` nodes
- `NoArgGetRoutes` — the no-arg GET routes to sweep, derived from
  `capabilitycatalog.LoadSurfaceInventory`
- `SweepRoutes` — measures true nearest-rank p95 latency per route against a
  running eshu-api, over a warmup-discarded counted sample; flags any 5xx
  response as `HardFailed` regardless of latency and captures the first
  counted-sample 5xx body (capped) as `HardFailedBody` so an operator can see
  the error envelope without re-running. `RouteQueryArgs` supplies
  representative selectors (seeded ids) so a route that needs one runs its
  real query instead of 400ing
- `WorkMeter`, `NewPgxWorkMeter`, `EnsureWorkMeterExtension`,
  `MeasureBackgroundCallRate`, `CheckMeterQuiet` — the `pg_stat_statements`
  meter (calls, rows, buffer blocks per request), its idle-noise guard, and the
  interface the hermetic RED/GREEN tests fake
- `ParseRouteWorkBudgets`, `EvaluateWorkBudgets`, `UnmeteredExercisedRoutes`,
  `WriteWorkReport` — the work budget table, the breach check (any of calls,
  blks, rows over budget), the guard against an exercised-but-unmetered route,
  and the JSON report `scripts/refresh-read-api-work-budgets.sh` renders the
  table from
- `VerifyGraphNodeCounts` — reads back per-label node counts after the graph
  seeds and fails the run when any label is short
- `ParseRouteBudgets`, `EvaluateBudgets` — the budget table and breach check
  (a `HardFailed` route always breaches); `RouteBudgets.WithCatalog`
  tightens a mapped route's budget to the capability catalog's declared
  production p95 when that is stricter
- `ExercisedCoverage`, `CheckCoverageFloor` — the vacuity guard: a route this
  gate could not really exercise (a 4xx) must not silently count as a pass,
  and the exercised-route count must not silently shrink
- `RequireNamedRoutesExercised` — the stricter companion guard: a route this
  table *explicitly* budgets must not silently drop out of coverage even if
  the overall floor still holds

See `doc.go` for the full contract.

## Dependencies

- `internal/scope` — `AllCollectorKinds`, the seed distribution's source of
  truth for collector kinds
- `internal/capabilitycatalog` — the generated surface inventory that drives
  route selection
- `internal/query`, `internal/storage/postgres` — the routes and queries this
  gate measures (not imported directly; exercised over HTTP against a
  running `eshu-api`)

## Telemetry

None. This is a CI-only benchmark binary; it reports through stdout/stderr
and its process exit code, not through the runtime telemetry surface.

## Gotchas / invariants

- `BuildSeedPlan` is pure and must stay deterministic — the live gate's
  RED/GREEN evidence depends on being able to reproduce the same seeded
  corpus across runs.
- The budget table (`testdata/benchmarks/read-api-route-budgets.txt`) must
  keep its `default` row: `ParseRouteBudgets` refuses to load a table
  without one, so a newly added route can never silently go unchecked.
- `NoArgGetRoutes` filters on `Category == SurfaceAPIRoute`,
  `Readiness == ReadinessImplemented`, method `GET`, and no `{` in the path.
  A route with a path parameter needs its own fixture and is out of scope
  for this gate.
- A route whose first probe returns a 4xx is reported `Exercised=false` and
  is not budget-checked — a fast validation rejection is not latency
  evidence. `RouteQueryArgs` is a manually-curated, partial table of
  representative selectors for routes this gate's own seeded corpus can
  satisfy; a route needing a selector from an unseeded domain (package
  registry, secrets/IAM, ...) is left not-exercised on purpose rather than
  given a fabricated argument. `ExercisedCoverageFloor` (`coverage.go`) is a
  ratchet on the exercised count — raise it when coverage improves, never
  lower it to paper over a regression.
- `RouteCapability` (`budget.go`) maps a route to its capability id for the
  catalog-tighter-wins budget check. It is not derivable automatically — the
  capability matrix's declared "tools" name is sometimes the route itself
  and sometimes an unrelated MCP/API tool name — so each entry is verified
  by reading the handler's own `XCapability = "..."` Go constant.

## Related docs

- `docs/public/reference/local-testing.md` (Compose live-gate pattern)
- `docs/public/reference/ci-gates.md` (`read-api-latency-gate` registry entry)
