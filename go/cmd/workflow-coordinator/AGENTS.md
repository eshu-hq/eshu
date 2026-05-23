# cmd/workflow-coordinator Agent Rules

These rules apply only inside `go/cmd/workflow-coordinator/`. Root
`AGENTS.md` still controls global proof, performance, concurrency, and skill
requirements.

## Read First

- `go/cmd/workflow-coordinator/README.md`
- `go/cmd/workflow-coordinator/doc.go`
- `go/cmd/workflow-coordinator/main.go`
- `go/internal/coordinator/service.go`
- `go/internal/coordinator/config.go`
- `go/internal/app/README.md`

## Local Invariants

- MUST keep deployment mode dark by default. Active mode is explicit opt-in.
- MUST preserve the active-mode gate: claims enabled plus at least one enabled,
  claim-capable collector instance.
- MUST keep heartbeat interval below lease TTL; invalid config must fail at
  startup.
- MUST keep this runtime off the graph backend. It reconciles Postgres workflow
  control state only.
- MUST keep trigger normalization and collector claim ownership outside this
  binary.
- MUST keep coordinator business logic in `internal/coordinator` or
  `internal/workflow`, not in `main.go`.
- MUST use SIGINT/SIGTERM context cancellation for shutdown.

## Change Gates

- New env vars MUST be parsed in `internal/coordinator/config.go`, validated in
  `Config.Validate` when needed, documented in command and coordinator READMEs,
  and covered by coordinator tests.
- Active-mode local tests MUST set deployment mode, claims enabled, and a valid
  `ESHU_COLLECTOR_INSTANCES_JSON`; do not hard-code deployment mode in
  `main.go`.
- Metrics changes MUST live in `internal/coordinator/metrics.go` and keep the
  `eshu_dp_workflow_coordinator_` prefix.
- Admin endpoint changes MUST go through shared hosted runtime surfaces, not
  direct handler registration in this command.

## Focused Verification

```bash
cd go
go test ./cmd/workflow-coordinator -count=1
go test ./internal/coordinator ./internal/storage/postgres -run 'Test.*Workflow|Test.*Claim|Test.*Reconcile' -count=1
```
