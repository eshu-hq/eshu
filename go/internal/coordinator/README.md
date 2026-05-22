# Coordinator

## Purpose

`internal/coordinator` runs the workflow coordinator loops. It reconciles
desired collector instances, plans bounded collector work for supported
families, hands off AWS freshness triggers, reaps expired claims, and advances
workflow-run progress.

## Ownership boundary

The coordinator writes workflow rows through `Store` and leaves provider work
to collector runtimes. It does not emit facts, claim collector work on behalf
of a runtime, open graph connections, or write graph truth. `internal/workflow`
owns the durable value contracts; `internal/storage/postgres` owns persistence.

## Exported surface

See `doc.go` and `go doc ./internal/coordinator` for the godoc contract.
Callers depend on `Service`, `LoadConfig`, the `Store` port, coordinator
metrics, and the supported work planners for Terraform state, OCI registries,
package registries, scheduled AWS scans, and AWS freshness triggers.

## Dependencies

- `internal/workflow` for collector instances, claims, defaults, and run
  contracts.
- `internal/scope` for collector-kind identity.
- `internal/collector/ociregistry` for OCI repository target normalization.
- `internal/collector/awscloud/freshness` for AWS freshness trigger and target
  identity.
- `internal/telemetry` for metric attributes.

## Telemetry

`NewMetrics` registers OTEL instruments under
`eshu_dp_workflow_coordinator_` for reconcile, reap, run-reconcile, durable
instance drift, and last-reaped/reconciled-run state. AWS freshness handoff also
records bounded event counters by trigger kind and action. Structured logs cover
startup mode, planner skips, duplicate target suppression, and handoff results.

## Gotchas / invariants

- Dark mode is the default. Active mode requires claims enabled and at least one
  enabled claim-capable collector instance.
- `HeartbeatInterval` must be strictly less than `ClaimLeaseTTL`.
- `Store` is required; `Service.Run` returns an error when it is nil.
- Scheduled workflow creation uses
  `(collector_kind, collector_instance_id, scope_id, acceptance_unit_id)` as
  the open-target key to avoid duplicate non-terminal work.
- AWS freshness cannot widen configured AWS access. Targets must already exist
  in durable collector instance target scopes.
- AWS scheduled planning skips invalid global/regional service pairings and
  records skipped tuples in `workflow_runs.requested_scope_set`.
- `run_reconcile_total` and `reap_total` are expected to stay zero in dark mode.
- `recordReap` and `recordRunReconciliation` use interface type assertions so
  tests can supply narrow metric stubs. Production should use `NewMetrics`.
- This package only schedules families with explicit planners.

## Focused tests

Use the smallest command that proves the changed contract:

```bash
cd go
go test ./internal/coordinator -count=1
go vet ./internal/coordinator
go doc ./internal/coordinator
go run ./cmd/eshu docs verify ../go/internal/coordinator --limit 1000 \
  --fail-on contradicted,missing_evidence
```

Changes to scheduling admission, claim reaping, or workflow completion usually
also need the matching `internal/workflow` and `internal/storage/postgres`
tests.

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/telemetry/index.md`
- `go/internal/workflow/README.md`
- `go/cmd/workflow-coordinator/README.md`
