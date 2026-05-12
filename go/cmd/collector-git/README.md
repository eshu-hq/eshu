# collector-git

## Purpose

`collector-git` is the local verification runtime for native Go collection.
It owns cycle orchestration, source-mode repository selection, repo sync,
durable fact commit, per-repo snapshot collection, content shaping, and the
optional SCIP collector path. It is the collection half of the runtime split
that the long-running `ingester` performs in the deployed stack.

## Ownership boundary

This binary owns collection-cycle wiring around `collector.Service`,
`collector.GitSource`, `NativeRepositorySelector`, and
`NativeRepositorySnapshotter`. It does not own parser registries
(`internal/parser/`), fact-store schema (`internal/storage/postgres/`), or
projection. The deployed long-running runtime that mounts the workspace PVC
in Kubernetes is `ingester`, not `collector-git`.

## Entry points

- `main` and `run` in `go/cmd/collector-git/main.go`
- `buildCollectorService` in `go/cmd/collector-git/service.go`
- run via `go run ./cmd/collector-git` in the local verification lane
- `eshu-collector-git --version` and `eshu-collector-git -v` print the build-time
  version through `buildinfo.PrintVersionFlag` before runtime setup begins

## Configuration

Repo-sync and discovery configuration is loaded via
`collector.LoadRepoSyncConfig("collector-git", getenv)` and
`collector.LoadDiscoveryOptionsFromEnv(getenv)`. The wiring also reads
`collector.LoadSnapshotSCIPConfig(getenv)`. Postgres is opened through
`runtime.OpenPostgres` (ESHU_POSTGRES_DSN and the rest of the standard
Postgres env contract). The poll interval defaults to 1 second
(`defaultCollectorPollInterval`).

Set the shared webhook-trigger handoff enabled env value to true to claim queued
webhook refresh triggers before scheduled polling. The handoff owner defaults
to `collector-git`, and the claim limit defaults to 100.

## Telemetry

The binary inherits its telemetry stack from the shared bootstrap
(`telemetry.NewBootstrap("collector-git")`, `telemetry.NewProviders`,
`telemetry.NewInstruments`) and from the hosted runtime in
`internal/runtime` and `internal/app`. The status surface is composed by
`app.NewHostedWithStatusServer` together with the Prometheus handler from
`telemetry.Providers.PrometheusHandler`. See `internal/runtime/README.md`
for the shared admin/metrics contract.

## Gotchas / invariants

- shutdown is signal-driven: `signal.NotifyContext` watches `os.Interrupt`
  and `SIGTERM` and the hosted runtime drains in-flight cycles before exit
- version probes are pre-startup checks; keep `buildinfo.PrintVersionFlag` at
  the top of `main` so local verification scripts can inspect the binary
  without Postgres credentials
- there is no separate workspace-PVC contract here; this runtime is intended
  for local verification, not as a Kubernetes-deployed collector
- collector cycles are observed under the shared collector span and metrics;
  do not bypass `collector.Service` to write facts directly
- webhook-triggered selection is still collection work: it prioritizes
  GitHub-backed repo sync, then the normal snapshot and fact commit path
  decides freshness

## Related docs

- [Service runtimes — Local Verification Runtimes](../../../docs/docs/deployment/service-runtimes.md#local-verification-runtimes)
- [CLI reference](../../../docs/docs/reference/cli-reference.md)
- [Docker Compose deployment](../../../docs/docs/deployment/docker-compose.md)
