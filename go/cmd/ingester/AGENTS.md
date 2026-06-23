# AGENTS.md — cmd/ingester guidance for LLM assistants

## Read first

1. `go/cmd/ingester/README.md` — pipeline position, lifecycle, env vars,
   and operational notes
2. `go/cmd/ingester/main.go` — `run` function; understand bootstrap order
   before touching wiring
3. `go/cmd/ingester/wiring.go` — `buildIngesterService`, `compositeRunner`,
   `buildIngesterCollectorService`, `buildIngesterProjectorService`; the two
   services run concurrently under a shared cancel context
4. `go/cmd/ingester/wiring_deferred_relationship.go` — shard-drain barrier,
   deferred relationship backfill, and deployment-mapping reopen ordering after
   collector batch drain
5. `go/internal/collector/README.md` and `go/internal/projector/README.md` —
   understand both services before modifying their wiring
6. `go/cmd/ingester/wiring_nornicdb_env.go` and `wiring_nornicdb_config.go` —
   NornicDB knobs; read before adding or changing any ESHU_NORNICDB_* variable

## Invariants this package enforces

- **Single workspace owner** — the ingester is the only runtime that should
  hold the workspace PVC. Do not add PVC mounts to other workloads.
- **AfterBatchDrained ordering** — sharded ingesters must record the local
  batch drain before global maintenance runs. Only the shard that completes the
  fleet barrier may run `BackfillAllRelationshipEvidence`, and it must run
  before `ReopenDeploymentMappingWorkItems`. Both must succeed; a failure exits
  the ingester. This implements CLAUDE.md Phase 1 / Phase 3 bootstrap ordering.
  Enforced in `wiring_deferred_relationship.go` and
  `internal/storage/postgres/deferred_maintenance_barrier.go`.
- **SkipRelationshipBackfill = true** on `IngestionStore` — per-commit backfill
  is suppressed deliberately. Do not remove this flag without adding equivalent
  per-commit backfill, which would slow the hot commit path.
- **Empty sharded batch participation** — `AfterEmptyBatchDrained` is enabled
  only when `ESHU_REPO_SHARD_COUNT > 1` so empty shards can enter the
  fleet-wide barrier. Do not enable it for unsharded ingesters.
- **compositeRunner retry-aware bounded drain** — superseding the former
  "first-error cancel" invariant (see
  `docs/internal/design/3501-ingester-composite-runner-failure-isolation.md`).
  A *transient* per-unit fault is owned by each service's own Run loop (durable
  dead-letter replay) and must not escape to the composite runner: a retryable
  collector commit failure is quarantined and the loop continues; the projector
  routes transient failures through `WorkSink.Fail`. Only a *fatal* error
  escapes a service's Run. When one does, the composite runner cancels the
  shared context, waits a **bounded** `drainGrace` for the sibling to finish its
  in-flight unit, aggregates **every** terminal error with `errors.Join` (no
  sibling error is masked or dropped), and joins `errCompositeDrainTimeout` if a
  sibling ignores cancellation. Do not reintroduce returning only the
  first-arriving result, and do not drop sibling errors.
- **Signal-driven shutdown** — `signal.NotifyContext(SIGINT, SIGTERM)` is the
  only supported shutdown path. Do not add alternate shutdown mechanisms.

## Common changes and how to scope them

- **Add a new graph backend** → add `wiring_<backend>_executor.go` and
  `wiring_<backend>_env.go` following the NornicDB pattern; handle the new
  `ESHU_GRAPH_BACKEND` value in `openIngesterCanonicalWriter`; update
  `docs/public/reference/nornicdb-tuning.md` if new tuning knobs are added.
  Do not branch on backend inside `buildIngesterService` or `buildIngesterProjectorService`.

- **Add a new NornicDB tuning knob** → add the env var constant in `wiring.go`
  alongside the existing `nornicDBCanonicalGroupedWritesEnv` constants, add the
  reader in `wiring_nornicdb_env.go`, pass the value through
  `openIngesterCanonicalWriter`, and update `docs/public/reference/nornicdb-tuning.md`
  and the active NornicDB ADR in the same PR. See CLAUDE.md NornicDB
  Compatibility Workflow.

- **Change projector worker defaults** → edit `projectorWorkerCount` in
  `wiring.go`; add a test in `wiring_nornicdb_phase_group_test.go` or a new
  file; read the projector README concurrency guidance first.

- **Add a new admin route** → wire through `app.NewHostedWithStatusServer`
  options in `main.go`; do not add bespoke HTTP bootstrap code outside that
  call.

## Failure modes and how to debug

- Symptom: ingester exits immediately after start →
  likely cause: `openIngesterCanonicalWriter` or `OpenPostgres` failed →
  check structured logs for `telemetry bootstrap`, `open postgres`, or
  `build ingester` errors; verify Bolt URI and Postgres DSN are set.

- Symptom: `eshu_dp_repos_snapshotted_total{status="failed"}` rising →
  likely cause: git clone failure, discovery error, or parse error →
  check `collector snapshot stage completed` logs for `stage=discovery` or
  `stage=parse` error fields; check workspace disk pressure and git credentials.

- Symptom: projector queue age growing after ingester restart →
  likely cause: projector workers cannot drain as fast as collection fills →
  check `eshu_dp_projector_stage_duration_seconds{stage="canonical_write"}`;
  raise `ESHU_PROJECTOR_WORKERS` only after confirming graph backend is not the
  bottleneck.

- Symptom: ingester exits with "deferred relationship maintenance failed" →
  likely cause: shard-drain barrier, `BackfillAllRelationshipEvidence`, or
  `ReopenDeploymentMappingWorkItems` Postgres error → check Postgres connection
  health, barrier table state, and fact-store table constraints; the exit is
  intentional to prevent partial maintenance state.

- Symptom: NornicDB write timeout in logs →
  likely cause: `ESHU_CANONICAL_WRITE_TIMEOUT` too short for current entity
  density or NornicDB is under memory pressure →
  check `nornicdb-tuning.md` for per-label batch size guidance before
  increasing the timeout blindly.

## Anti-patterns specific to this package

- **Branching on `ESHU_GRAPH_BACKEND` inside `buildIngesterService`** — backend
  selection belongs only in `openIngesterCanonicalWriter` and the
  `wiring_<backend>_*.go` files. The collector and projector services are
  backend-agnostic.

- **Attaching the workspace PVC to another runtime** — the ingester is the
  single owner. Sharing the PVC causes write conflicts under concurrent git
  operations.

- **Running `AfterBatchDrained` logic inline in the per-commit path** —
  backfill must be deferred to after the full batch drain, not per-commit.
  Per-commit backfill is the design that `SkipRelationshipBackfill = true`
  intentionally avoids.

- **Setting ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true in production before
  conformance** — this flag is gated on the fixed rollback binary and a full
  conformance pass. Using it prematurely can produce partial writes.

- **Raising ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY without measuring NornicDB
  commit headroom** — the streaming dispatcher in
  `wiring_nornicdb_phase_group_streaming.go` keeps one Bolt session per
  worker open for the lifetime of an entity-phase call, so peak Bolt session
  demand is `ESHU_PROJECTOR_WORKERS * ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY`
  and the cap of 16 still applies. The legacy per-flush path lives in
  `wiring_nornicdb_phase_group.go` (`executeEntityPhaseGroup`) and runs only
  when concurrency is at most one. Raise the knob only after a focused run
  names the canonical entities phase as the wall-clock bottleneck and
  NornicDB structured logs show no contention on parallel commits.

## What NOT to change without an ADR

- `AfterBatchDrained` barrier and call order (fleet drain barrier before
  `BackfillAllRelationshipEvidence`, then `ReopenDeploymentMappingWorkItems`) —
  changing this order breaks the bootstrap phase contract in `CLAUDE.md`.
- `compositeRunner` error propagation and the retry-vs-fatal boundary —
  silencing either service error, returning only the first-arriving result, or
  promoting a retryable per-unit fault into a fatal teardown all break the
  failure-isolation contract in
  `docs/internal/design/3501-ingester-composite-runner-failure-isolation.md`.
  The drain wait must stay bounded; an unbounded wait can hang ingester
  teardown.

## Concurrency evidence (issue #3501)

No-Regression Evidence: `go build ./...` (exit 0);
`go test ./cmd/ingester/... ./internal/runtime/... -count=1 -race` (369 passed);
`go test ./internal/collector/ -count=1 -race` (381 passed). The composite
runner change removes a serialized fail-fast teardown rather than adding one and
touches no Cypher, graph write shape, worker count, batch size, lease, or queue
ordering, so there is no throughput regression to measure; concurrency is
preserved because the projector keeps running through a transient collector
fault. Conflict domain: the shared cancel context and the result channel
(buffered to `len(runners)` so sends never block and no goroutine leaks);
coordination is by context cancellation and a single-reader channel, with no
locks. `TestCompositeRunnerNoGoroutineLeak` and
`TestCompositeRunnerBoundsDrainOnWedgedSibling` (both run under `-race`) prove
no leak and bounded teardown.

Observability Evidence: a retryable collector commit logs `WARN` with
`failure_class=commit_retryable` and `retryable=true`; a composite fatal logs
`ERROR` with `failure_class=composite_runner_fatal`, `runner_index`, and
`drain_grace`; a bounded drain timeout logs `ERROR` with
`failure_class=composite_runner_drain_timeout` and returns
`errCompositeDrainTimeout`. These let an operator separate a retried generation,
a fatal teardown, and a forced bounded drain without reading code.
- The workspace PVC ownership model — moving workspace ownership to another
  runtime requires a coordinated deployment change and an ADR documenting
  the new ownership boundary.
