# Coordinator

## Purpose

`internal/coordinator` owns the workflow coordinator loop. It reconciles desired
collector instances, plans bounded scheduled work for supported collector
families, hands off AWS freshness triggers, reaps expired claims, and
reconciles workflow-run progress.

## Ownership boundary

Coordinator code plans work rows and reconciles workflow state through a narrow
`Store` interface. It does not claim provider work on behalf of collectors,
emit facts, open graph connections, or write graph truth. Collector runtimes own
claim execution and fact emission; `internal/workflow` owns the value
contracts.

Active-mode loop:

```text
Service.Run
  -> runReconcile
  -> scheduled planners for Terraform-state / OCI / package / AWS
  -> runActiveMaintenance
  -> runReapExpiredClaims
  -> runAWSFreshnessHandoff
  -> runWorkflowReconciliation
```

Dark mode reconciles collector instances only. Its reap ticker is nil.

## Exported surface

See `doc.go` and `go doc ./internal/coordinator` for the full godoc contract.
The main package contracts are:

- `Service`, `Config`, and `LoadConfig`.
- `Store`, the durable interface implemented by
  `storage/postgres.WorkflowControlStore`.
- `Metrics`, `NewMetrics`, `ReconcileObservation`, `ReapObservation`, and
  `RunReconciliationObservation`.
- `TerraformStateWorkPlanner`, `OCIRegistryWorkPlanner`,
  `PackageRegistryWorkPlanner`, `AWSScheduledWorkPlanner`, and
  `AWSFreshnessWorkPlanner`.
- `AWSFreshnessTriggerStore` and planner request/response types for supported
  families.

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
`eshu_dp_workflow_coordinator_`: `reconcile_total`,
`reconcile_duration_seconds`, `reap_total`, `reap_duration_seconds`,
`run_reconcile_total`, `run_reconcile_duration_seconds`,
`desired_collector_instances`, `durable_collector_instances`,
`collector_instance_drift`, `last_reaped_claims`, and
`last_reconciled_runs`.

Structured logs include startup mode, collector-instance drift, planner
admission/skips, duplicate target suppression, and AWS freshness handoff
outcomes.

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

## Verification

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
