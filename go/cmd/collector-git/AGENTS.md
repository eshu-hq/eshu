# collector-git Agent Guidance

## Read First

1. `README.md` and `doc.go` for the command contract.
2. `service.go` for `collector.Service`, repo-sync, discovery, SCIP, and
   webhook-trigger handoff wiring.
3. `main.go` for version probes, telemetry bootstrap, Postgres open, hosted
   status, and signal handling.
4. `go/internal/collector/README.md` for selector, snapshotter, committer, and
   service-loop ownership.
5. `go/internal/runtime/README.md` for shared Postgres and admin/metrics
   hosting.

## Local Rules

- Keep collection behind `collector.Service` and
  `postgres.NewIngestionStore`. Do not write facts directly from this command.
- Keep `buildinfo.PrintVersionFlag` before runtime setup so `--version` and
  `-v` do not require Postgres credentials.
- Keep `/healthz`, `/readyz`, `/metrics`, and `/admin/status` hosted through
  `app.NewHostedWithStatusServer`.
- Preserve signal-driven shutdown and hosted-service draining.
- Treat `collector-git` as the local verification lane. The deployed
  PVC-backed collector runtime is `ingester`.
- Keep webhook-trigger handoff as repo-sync prioritization only. Trigger
  claims must still flow through the normal snapshot and fact commit path.
- Keep repository URLs, credentials, and full local checkout paths out of logs,
  facts, metric labels, and docs.

## Change Rules

- Add service wiring in `service.go` and cover zero-value/default behavior in
  `service_test.go`.
- Add env-driven options through the existing config loaders before threading
  them into `collector.Service`, `GitSource`, or `NativeRepositorySnapshotter`.
- Treat poll-interval changes as runtime-affecting: add focused tests and
  tracked performance/observability evidence if the change can affect queue or
  collection latency.
- Do not bypass shared telemetry, retry, status, or admin wiring to simplify a
  local run.
