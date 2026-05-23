# Coordinator

## Purpose

`internal/coordinator` runs workflow-coordinator reconciliation. It reconciles
collector instances, plans bounded collector work, hands off AWS freshness
triggers, reaps expired claims, and advances workflow-run progress.

## Ownership Boundary

The coordinator writes workflow/control rows through the `Store` port. It does
not claim provider work for collectors, call source APIs, emit facts, open graph
connections, or decide graph truth. `internal/workflow` owns value contracts;
`internal/storage/postgres` owns persistence.

## Exported Surface

See `doc.go` and `go doc ./internal/coordinator` for the contract. Callers use
`Service`, `LoadConfig`, `Store`, `NewMetrics`, and the focused work planners;
planner lists belong in godoc, not duplicated here.

## Telemetry

`NewMetrics` registers `eshu_dp_workflow_coordinator_*` instruments for
reconcile, reap, run reconciliation, durable instance drift, and freshness
handoff. Logs cover startup mode, planner skips, duplicate target suppression,
drift warnings, and handoff results.

## Gotchas / Invariants

- Dark mode is the default. Active mode requires claims enabled and at least one
  enabled claim-capable collector instance.
- `HeartbeatInterval` must be lower than `ClaimLeaseTTL`.
- Scheduled work uses collector kind, collector instance, scope, and acceptance
  unit as the open-work key.
- AWS freshness cannot widen configured access; durable target scopes must
  already authorize the claim.
- AWS scheduled planning records invalid global/regional skips in the requested
  scope set.

## Focused Tests

```bash
cd go
go test ./internal/coordinator -count=1
go vet ./internal/coordinator
go doc ./internal/coordinator
go run ./cmd/eshu docs verify ../go/internal/coordinator --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/telemetry/index.md`
- `go/internal/workflow/README.md`
