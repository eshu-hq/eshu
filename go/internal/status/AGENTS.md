# Status Agent Rules

These rules are mandatory for changes under `go/internal/status`.

## Read First

1. `go/internal/status/README.md`
2. `go/internal/status/status.go`
3. `go/internal/status/status_health.go`
4. `go/internal/status/http.go`
5. `go/internal/status/json.go`
6. `go/internal/status/coordinator.go`
7. AWS status files when changing AWS collector or freshness output.
8. `docs/public/reference/http-api.md`
9. `docs/public/reference/cli-reference.md`

## Invariants

- Published JSON field names are frozen. Add fields only additively.
- `QueueFailureSnapshot` message and details are high-cardinality status data;
  never use them as metric labels.
- `BuildReport` MUST remain pure: no I/O, no caching, no storage dependency.
- Health priority MUST remain stalled, degraded, progressing, healthy.
- Shared projection backlog MUST participate in readiness after fact queues
  drain; otherwise graph-backed reads can look ready too early.
- Domain backlog output MUST stay capped by `Options.DomainLimit`.
- `CoordinatorSnapshot` is optional. Nil-check before clone, text render, and
  JSON render.
- AWS cloud scan status MUST keep scanner state separate from fact commit state.
- AWS freshness status MUST stay aggregate. Do not expose event IDs, ARNs,
  resource IDs, or payload details.

## Change Rules

- New health reason: add it in the correct priority slot and test the state
  machine.
- New `Report` or `RawSnapshot` field: update `BuildReport`, text render,
  JSON render, HTTP/CLI docs, and tests.
- Registry status field: keep it aggregate-only and scrub host, repository,
  package, tag, digest, account, credential, and metadata URL details.
- Coordinator field: update snapshot, JSON, text rendering, clone logic, and
  nil behavior.
- Retry-policy status change: wire runtime summaries from entrypoints unless
  the value is universal to every deployment.
- HTTP format negotiation change: test `Accept` and `?format=` paths.

## Failure Checks

- Unexpected `stalled`: inspect overdue claims first, then queue claim metrics
  and worker logs.
- `latest_failure=graph_write_timeout`: inspect graph write and query duration
  metrics before changing status logic.
- Missing domain backlog: inspect the domain limit and sort order.
- Missing coordinator section: confirm runtime wiring populates
  `RawSnapshot.Coordinator`.
- JSON/text mismatch: update both render paths and run status tests.

## Forbidden Without Architecture-Owner Approval

- Health state names.
- Published JSON field names.
- Stalled-before-degraded priority.
- Making `BuildReport` stateful.
- Adding high-cardinality status fields to metrics.
- Importing telemetry to emit metrics from this package.
