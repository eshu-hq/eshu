# cmd/workflow-coordinator

## Purpose

`cmd/workflow-coordinator` builds `eshu-workflow-coordinator`, the runtime that
reconciles collector instance state and, when deployment mode is active, reaps
expired collector claims and recomputes workflow-run completeness.

## Ownership boundary

This command owns process startup, deployment-mode wiring, status hosting, and
coordinator service configuration. It does not normalize triggers, collect
source data, emit facts, or decide graph truth.

## Exported surface

The command package exports no API. Its process contract is version probing,
environment-driven mode selection, hosted admin routes, pprof opt-in, and
signal-driven shutdown. See `doc.go` for the binary summary.

## Dependencies

The binary wires `internal/coordinator`, `internal/storage/postgres`,
`internal/runtime`, `internal/app`, `internal/status`, and `internal/telemetry`.
Postgres owns workflow state and claim persistence.

## Telemetry

Startup uses `telemetry.NewBootstrap("workflow-coordinator")`, service/component
`workflow-coordinator`, default tracer/meter instruments, optional pprof through
`ESHU_PPROF_ADDR`, and the shared `/healthz`, `/readyz`, `/metrics`, and
`/admin/status` routes.

## Gotchas / invariants

- `--version` and `-v` must exit before telemetry, Postgres, pprof, or HTTP
  setup.
- Dark mode should reconcile declarative state without active claim reaping.
  Active deployment mode gates reaping and completeness loops.
- Expired claim reaping must preserve workflow store lease/fencing semantics.
- Completeness summaries are status signals, not graph truth.

## Focused tests

```bash
cd go
go test ./cmd/workflow-coordinator -count=1
go test ./internal/coordinator ./internal/storage/postgres -run 'Test.*Workflow|Test.*Claim|Test.*Reconcile' -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/local-testing.md`
- `go/internal/storage/postgres/README.md`
