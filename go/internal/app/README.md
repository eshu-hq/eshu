# internal/app

## Purpose

`internal/app` provides the small hosted-application wrapper shared by Eshu
commands: runtime config, lifecycle composition, runner execution, rollback on
partial startup, and optional shared admin/status routes.

## Ownership boundary

This package owns process lifecycle wiring, not service behavior. Commands
provide the runner and status reader; `internal/runtime` owns the concrete
admin mux and config parsing.

## Exported surface

See `doc.go` for the contract. Exported surfaces include `Lifecycle`, `Runner`,
`Application`, `New`, `NewHosted`, `ComposeLifecycles`, `MountStatusServer`,
and `NewHostedWithStatusServer`.

## Dependencies

The package depends on `internal/runtime` for config and admin mux wiring and
`internal/status` for status readers. It should stay free of collector,
parser, reducer, storage, and graph-backend ownership.

## Telemetry

`internal/app` emits no metrics or spans directly. Hosted commands expose the
runtime info gauge and admin routes through the mounted runtime mux, and own
their service-specific logs, spans, and metrics.

## Gotchas / invariants

- Lifecycle startup must roll back already-started lifecycles when a later
  start hook fails.
- Stop hooks should run in reverse ownership order through composed lifecycles.
- `NewHostedWithStatusServer` is convenience wiring; commands still own the
  status reader and route options they pass in.
- Keep this package generic. Do not add package-specific runtime behavior here.

## Focused tests

```bash
cd go
go test ./internal/app -run 'Test.*Lifecycle|Test.*Hosted|Test.*Status' -count=1
go test ./internal/app -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/telemetry/index.md`
- `go/internal/runtime/README.md`
