# read-api-latency-gate — Agent Instructions

LLM-assistant companion to `README.md`. Read this before editing any file in
`go/cmd/read-api-latency-gate/`.

## Read first

- `README.md` — exported surface, ownership boundary, gotchas.
- `doc.go` — the godoc contract.
- `scripts/verify-read-api-latency-gate.sh` — the orchestrator that brings up
  Compose, builds this binary plus `eshu-api`, runs the seed+sweep, and tears
  down. Changes here often need a matching change there.
- `testdata/benchmarks/read-api-route-budgets.txt` — the per-route latency
  budgets this binary enforces.

## Invariants

- **`BuildSeedPlan` (`seed_plan.go`) must stay pure and deterministic.** No
  clock reads, no randomness, no map-iteration-order dependence in its
  output ordering. The live gate's RED/GREEN evidence and any future
  `SeedPlan` diffing depend on this.
- **Collector kind enumeration comes from `scope.AllCollectorKinds()`, never
  a hardcoded list.** A new collector kind must be picked up automatically
  (issue #6797's explicit requirement) — do not special-case kinds here.
- **Route enumeration comes from `capabilitycatalog.LoadSurfaceInventory()`,
  never a hardcoded route list.** A new no-arg GET route must be swept, and
  budgeted (via the `default` row), automatically.
- **`ParseRouteBudgets` requires a `default` row.** Do not make it optional —
  that is what keeps a newly added route from going unchecked.
- Bulk writes (`seed_postgres.go`, `seed_graph.go`) use `pgx.CopyFrom` and
  one `UNWIND CREATE` per graph label, not per-row `INSERT`/`CREATE`. Row
  counts here run into the tens of thousands; per-row writes would blow the
  CI job's time budget.
- The row-shaping logic (`seed_rows.go`: `scopeRows`, `generationRows`,
  `workItemRows`) is deliberately split from the `pgx.CopyFrom` calls
  (`seed_postgres.go`) so it stays unit-testable without a live Postgres.
  Keep that split when extending either file.
- `scope_generations_active_scope_idx` (migration `002_scope_generations.sql`)
  enforces at most one `status = 'active'` generation per scope. If you
  change the churn logic in `seed_plan.go`, keep `generationRows`
  (`seed_rows.go`) emitting exactly one active row per scope's generation
  set — the seeder will hit a unique-constraint violation otherwise.
- **`fact_work_items_reducer_live_lease_uniq` (migration `005_fact_work_items.sql`)
  allows at most one row per scope with `stage='reducer' AND status IN
  ('claimed','running')`, across every generation of that scope, not
  distinguishing which of the two statuses it is** — this seeder never sets
  `conflict_key`, so every reducer row for a scope collides on the same
  index key. `injectOneReducerLease` (`seed_plan.go`) is what keeps this
  invariant; `TestBuildSeedPlanRespectsReducerLiveLeaseUniqueness`
  (`seed_plan_test.go`) is a regression test for a real bug this hit live.
  Do not add `"claimed"`/`"running"` back into `factWorkItemStatuses`'s
  weighted cycle.
- **`SeedIaCGraphNodes` (`seed_graph.go`) must stay correlated with
  `BuildIaCFacts` (`seed_iac.go`) via `buildIaCGraphNodeRows`
  (`seed_iac_graph_rows.go`).** `/api/v0/iac/resources`
  (`go/internal/query/iac/resources.go`) selects candidates from Postgres
  (`entity_id`/`entity_name`/`generation_id`) and hydrates them from the graph
  with `MATCH (n:<label>) WHERE n.uid IN $candidate_ids`; `searchHydrationMatches`
  then 500s ("current inventory and graph projection disagree") unless every
  candidate has a graph row whose `uid`, `id`, `name`, and `generation_id`
  all match. `SeedGraph`'s anonymous bulk `infraLabels` nodes carry no `uid`
  on purpose (they exist only to give the #6793 per-label scan cost realistic
  volume), so they can never satisfy that query on their own — this is a real
  gate-environment gap that HardFailed the route on every run until closed.
  `TestBuildIaCGraphNodeRowsCorrelateWithFacts` pins the correlation.
- **`BuildIaCFacts`'s `EntityName` is zero-padded (`%06d`) on purpose.**
  Postgres `SearchActive` orders `entity_name, entity_id` as text, so an
  unpadded `seed_%d` sorts `seed_1`, `seed_10`, `seed_2` — the "first page"
  would then be an unpredictable subset of build order.
  `TestBuildIaCFactsEntityNameIsLexicographicallySortPredictable` guards it.
- **`ESHU_COMPONENT_HOME` is exported by `verify-read-api-latency-gate.sh`.**
  `GET /api/v0/component-extensions` 503s unconditionally without it
  (`go/internal/query/component_extensions.go`), a gate-environment gap that
  HardFailed the route on every run until set. An empty directory is a
  supported registry state (zero installed components, not an error).
- **`HardFailedBody` (`sweep.go`) captures only the FIRST counted-sample 5xx
  body, capped at `hardFailedBodyCap` bytes.** The first failure is the most
  informative; overwriting it with a later repeat discards the evidence an
  operator needs to root-cause a HardFailed route.
- **A route's status code does not decide whether `sweepOne` fails** — see
  `sweep.go`'s doc comments. A 4xx means "not exercised," not "sweep error";
  only a connection-level failure aborts the run. Do not reintroduce a
  non-2xx check that returns an error.
- **`p95` (`sweep.go`) is true nearest-rank (`ceil(0.95*n)-1`), not
  `int(n*0.95 + 0.9999)`.** The latter is the max for n=20 and off-by-one for
  larger n — a real bug this gate shipped and a live review caught. `TestSweepRoutesComputesTrueNearestRankP95` pins this
  with a single-outlier-among-20 case; do not "simplify" the formula back to
  a ceiling-via-float trick.
- **`warmupRequests` (`sweep.go`) discarded probes run before the counted
  sample.** A cold connection/cache makes the first request(s)
  unrepresentative; do not fold them into the p95 sample set.
- **A 5xx always sets `HardFailed=true` and always breaches** (`sweep.go`,
  `EvaluateBudgets`), regardless of how fast it answered. Do not let a fast
  failure read as a fast pass.
- **`RequireNamedRoutesExercised` must run on every named budget row, not
  just the overall floor.** A route this table explicitly budgets losing its
  selector/auth scope must fail the gate loudly even while
  `ExercisedCoverageFloor` still holds on the aggregate count.
- **`RouteQueryArgs` (`route_args.go`) must only name a route this gate's own
  seeded corpus can actually satisfy.** Do not add an entry whose selector
  has no matching seeded data — that would trade a honest `NOT_EXERCISED`
  for a query that runs but proves nothing more than the 400 it replaces.
- **`RouteBudgets.WithCatalog`'s catalog value only ever tightens the
  effective budget (`min(catalog*catalogCIMultiplier, configured)`), never
  loosens it.** If you add a `RouteCapability` entry, verify the mapping
  against the handler's own `XCapability = "..."` constant in
  `go/internal/query` — do not guess from the capability matrix's `tools`
  field alone (see `RouteCapability`'s doc comment for why).
- **`ExercisedCoverageFloor` (`coverage.go`) is a ratchet.** Raise it only
  after a change (a new `RouteQueryArgs` entry, an auth fix) measurably
  increases the exercised count; never lower it.

## Verification

```bash
cd go && go test ./cmd/read-api-latency-gate/... -v
cd go && go vet ./cmd/read-api-latency-gate/...
gofumpt -l go/cmd/read-api-latency-gate/
```

The Postgres/NornicDB-backed paths (`SeedPostgres`, `SeedGraph`, and the full
`scripts/verify-read-api-latency-gate.sh` run) are not exercised by
`go test` alone — they need the Compose stack. See
`docs/public/reference/local-testing.md` for the live-gate pattern this
mirrors (`scripts/verify-golden-corpus-gate.sh`).
