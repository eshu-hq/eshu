# AGENTS.md — cmd/collector-git guidance for LLM assistants

## Read first

1. `go/cmd/collector-git/README.md` — binary purpose, ownership boundary,
   and gotchas
2. `go/cmd/collector-git/service.go` — `buildCollectorService`; how every
   `collector.Service` field is wired from env config
3. `go/cmd/collector-git/main.go` — `run`; telemetry bootstrap, Postgres
   open, service hosting, and signal handling
4. `go/internal/collector/README.md` — `Service`, `GitSource`,
   `NativeRepositorySelector`, `NativeRepositorySnapshotter`, and their config
   loaders
5. `go/internal/runtime/README.md` — `OpenPostgres`, `WithPrometheusHandler`;
   shared config helpers used in `run`

## Invariants this package enforces

- **Collection through `collector.Service` only** — facts must not be written
  directly from this binary. All collection goes through the
  `collector.Service` poll loop backed by `NativeRepositorySelector` and
  `NativeRepositorySnapshotter`. Enforced by doc.go.
- **Signal-driven shutdown** — `run` sets up a signal context via
  `signal.NotifyContext`, passing `WithPrometheusHandler` to the hosted
  service, and the runtime drains in-flight cycles before exiting. Enforced
  at `main.go:83`.
- **Admin surface always present** — `NewHostedWithStatusServer` mounts
  `/healthz`, `/readyz`, `/metrics`, and `/admin/status`. Removing or
  bypassing this wiring drops the shared operator contract. Enforced at
  `main.go:73`.
- **Poll interval is 1 second** — `defaultCollectorPollInterval` is a
  package-level constant in `service.go`. Changing it affects how quickly
  the `Instruments`-instrumented service reacts to new repo-sync events
  without queue-depth visibility. Enforced at `service.go:13`.

## Common changes and how to scope them

- **Add a new `collector.GitSource` field** → add the field wiring to
  `buildCollectorService` in `service.go`; add a corresponding test in
  `service_test.go`. Why: `buildCollectorService` is the single wiring
  point; missing fields silently default to zero values.

- **Add a new env-driven config option** → add a loader call in
  `buildCollectorService` (after `collector.LoadRepoSyncConfig` or
  `collector.LoadDiscoveryOptionsFromEnv`); thread the value into the
  `collector.Service` or `collector.NativeRepositorySnapshotter` fields;
  update `README.md`. Why: untested env wiring has caused silent zero-value
  defaults in past PRs.

- **Change the poll interval** → update `defaultCollectorPollInterval` in
  `service.go`; add a comment explaining the reason. Why: there is no runtime
  knob for this value; it must be changed in source and verified with a
  focused test.

## Failure modes and how to debug

- Symptom: binary exits with `telemetry bootstrap failed` → cause: OTEL
  provider init error; check that the OTEL endpoint env var is empty (not
  malformed) or points to a reachable collector.

- Symptom: binary exits with a Postgres open error → cause: ESHU_POSTGRES_DSN
  is missing or wrong; verify the value matches the running Postgres service.

- Symptom: `buildCollectorService` returns an error at startup → cause:
  `collector.LoadRepoSyncConfig` or `collector.LoadDiscoveryOptionsFromEnv`
  found an invalid env value; check the structured log `error` field printed
  before `os.Exit(1)`.

- Symptom: collection cycles run but no facts appear in Postgres → cause: the
  `postgres.NewIngestionStore` committer is not receiving snapshots; check
  `collector.Service.Committer` is wired and that `collector.GitSource.Source`
  returns snapshots; inspect the collector span in traces.

## Anti-patterns specific to this package

- **Writing facts outside `collector.Service`** — bypassing the service
  discards all telemetry instrumentation, retry handling, and the shared
  admin/metrics surface.

- **Removing `app.NewHostedWithStatusServer`** — the Prometheus handler and
  status endpoints are part of the shared operator contract across all Eshu
  local runtimes. Operator tooling expects them.

- **Running this binary in Kubernetes as the deployed collector** — the
  deployed long-running collector is `ingester`, which mounts the workspace
  PVC. `collector-git` is the local verification lane only.

## What NOT to change without an ADR

- The `/healthz`, `/readyz`, `/metrics`, and `/admin/status` endpoints mounted
  by `app.NewHostedWithStatusServer` — these are part of the shared operator
  contract across all Eshu runtimes; see
  `docs/docs/deployment/service-runtimes.md`.
