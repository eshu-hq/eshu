# AGENTS.md — cmd/projector guidance for LLM assistants

## Read first

1. `go/cmd/projector/README.md` — binary purpose, lifecycle, configuration,
   and gotchas
2. `go/cmd/projector/runtime_wiring.go` — `buildProjectorService` and
   `buildProjectorRuntime`; how every projector port is wired
3. `go/internal/projector/README.md` — the projection engine this binary drives
4. `go/internal/runtime/README.md` — `OpenPostgres`, `OpenNeo4jDriver`,
   `LoadRetryPolicyConfig`; shared config helpers used here
5. `go/internal/storage/cypher/README.md` — canonical node writer and
   `InstrumentedExecutor`; what this binary passes as `CanonicalWriter`

## Invariants this package enforces

- **Single wiring point** — `buildProjectorRuntime` in `runtime_wiring.go` is
  the only place that constructs `projector.Runtime`. Any new port or dependency
  must be wired here, not scattered through `main.go` or `config.go`.
- **Queue lease match** — both `projectorQueue` and `reducerQueue` use the same
  one-minute lease duration. Changing one without the other risks re-claim
  races. Enforced at `runtime_wiring.go:31-32`.
- **Instrumented executor wraps raw executor** — the Neo4j executor returned by
  `openProjectorCanonicalWriter` is always wrapped with
  `sourcecypher.InstrumentedExecutor` before being passed to the canonical node
  writer. Raw executor must not be exposed directly. Enforced at
  `runtime_wiring.go:96-102`.
- **SIGINT/SIGTERM triggers clean shutdown** — `signal.NotifyContext` is the
  only mechanism for stopping `service.Run`; do not add `os.Exit` calls or
  explicit goroutine kills outside this path.

## Common changes and how to scope them

- **Add a new env var for projector tuning** → add constant in `config.go`,
  thread through `buildProjectorService` or `buildProjectorRuntime`, add to
  `README.md` configuration section, run `runtime_wiring_test.go`. Why:
  untested env vars have led to silent zero-value defaults in past PRs.

- **Change the canonical writer backend** → touch `openProjectorCanonicalWriter`
  in `runtime_wiring.go`; the `projector.CanonicalWriter` interface is the
  boundary — new backends must implement it. Why: swapping the writer here
  replaces the graph-write path for the entire binary without touching
  `internal/projector` code.

- **Add a new port to `projector.Runtime`** → add the interface to
  `internal/projector/runtime.go`, wire the Postgres implementation in
  `buildProjectorRuntime`, add a test in `runtime_wiring_test.go`. Why:
  `Runtime` fields are all optional interfaces; missing wiring silently leaves
  the port nil and the feature inactive.

## Failure modes and how to debug

- Symptom: binary exits immediately with `telemetry bootstrap failed` → cause:
  OTEL provider init failed; check the OTEL endpoint env var is valid or empty
  (not malformed).

- Symptom: binary exits with `open neo4j driver` error → cause: Neo4j URI,
  username, or password env vars missing or wrong; verify those env vars match
  the running graph backend.

- Symptom: projector claims work but never writes → cause: `CanonicalWriter`
  or `ContentWriter` is nil because wiring was skipped; check
  `buildProjectorRuntime` returned no error and all fields are non-nil before
  `service.Run`.

- Symptom: work items re-claimed repeatedly → cause: canonical write exceeding
  the one-minute lease without heartbeat; wire `projector.Service.Heartbeater`
  (use `postgres.ProjectorQueue`) and set the heartbeat interval to less than
  the lease duration.

- Symptom: `/admin/status` shows no queue progress → cause: Postgres connection
  failed or projector queue is empty; curl `/healthz` first, then check
  `eshu_dp_queue_depth{queue="projector"}`.

## Anti-patterns specific to this package

- **Logic in `main.go` beyond bootstrap** — `main.go` should only bootstrap
  telemetry and delegate to `run`. Domain wiring belongs in `runtime_wiring.go`;
  config loading belongs in `config.go`.

- **Calling `projector.Runtime.Project` directly from `main.go`** — `Service.Run`
  owns claim/ack lifecycle. Bypassing it means work items are never acked,
  causing re-queue loops.

- **Hard-coding `ESHU_PROJECTOR_RETRY_ONCE_SCOPE_GENERATION` for production
  tuning** — this env var is a fault-injection knob for testing retry paths.
  Use the `PROJECTOR`-prefixed max-attempts and retry-delay env vars loaded via
  `runtimecfg.LoadRetryPolicyConfig` for production retry policy.

## What NOT to change without an ADR

- The `projector` queue lease duration (currently one minute) — changing it
  affects claim contention across all projector workers; coordinate with
  `eshu-ingester` and `eshu-reducer` owners.
- The admin surface endpoints (`/healthz`, `/readyz`, `/metrics`,
  `/admin/status`) mounted by `app.NewHostedWithStatusServer` — these are part
  of the shared operator contract across all Eshu runtimes; see
  `docs/docs/deployment/service-runtimes.md`.
