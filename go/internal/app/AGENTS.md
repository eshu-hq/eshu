# internal/app Agent Rules

This package is only the hosted-command shell: runtime config, lifecycle
composition, runner execution, rollback on partial startup, and optional status
server wiring. Service behavior belongs in the caller.

## Read First

MUST read these before editing:

1. `README.md` and `doc.go`.
2. `app.go`, `lifecycle.go`, and `status_server.go`.
3. `../runtime/README.md` before changing hosted runtime wiring.
4. A current caller such as `cmd/reducer/main.go` or `cmd/ingester/main.go`
   before changing constructor behavior.

## Local Invariants

- `Application.Run` MUST require both `Lifecycle` and `Runner` before starting.
- `ComposeLifecycles` MUST roll back already-started lifecycles in reverse
  order when a later `Start` fails.
- `Application.Run` MUST call `Lifecycle.Stop` through defer after a successful
  start, even when `Runner.Run` returns an error.
- `MountStatusServer` MUST create a separate metrics server only when
  `MetricsAddr` is non-empty and differs from `ListenAddr`.
- This package MUST NOT add retry, queue, collector, reducer, storage, graph, or
  package-specific logic.
- This package emits no telemetry directly; runtime/status and hosted services
  own observability.

## Change Rules

- New hosted binaries MUST prefer `NewHostedWithStatusServer` unless their
  status-server wiring genuinely differs; avoid changing this package for one
  service's wiring.
- Interface changes to `Lifecycle`, `Runner`, or `Application` fields require
  coordinated updates across `internal/runtime` and every `cmd/` caller.
- New convenience constructors MUST reuse `MountStatusServer` instead of
  forking admin/metrics listener logic.

## Proof

Run the focused gate for any edit:

```bash
cd go
go test ./internal/app -count=1
go vet ./internal/app
go doc ./internal/app
```

Lifecycle interface changes also require a focused `cmd/...` gate. Docs-only
edits also need the package-doc verifier for this directory and
`git diff --check`.
